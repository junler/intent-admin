// Package auth implements the browser authentication boundary. Permissions and
// risk checks remain in the Capability executor for every execution source.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/service"
)

const (
	sessionContextKey = "intent-admin:session-id"
	userContextKey    = "intent-admin:current-user"
	failureTTL        = 10 * time.Minute
	permissionTTL     = 30 * time.Minute
)

// Service owns signed tokens, revocable Redis sessions and identity queries.
type Service struct {
	db       *gorm.DB
	identity *service.Identity
	cache    *cache.Cache
	cfg      *config.Config
	now      func() time.Time
}

func New(db *gorm.DB, c *cache.Cache, cfg *config.Config) *Service {
	return &Service{db: db, identity: service.NewIdentity(db), cache: c, cfg: cfg, now: time.Now}
}

type Session struct {
	UserID    int64     `json:"userId"`
	UserName  string    `json:"userName"`
	LoginTime time.Time `json:"loginTime"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"userAgent"`
}
type permissionSnapshot struct {
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

func itoa(id int64) string                     { return strconv.FormatInt(id, 10) }
func (s *Service) sessionKey(id string) string { return s.cache.Key("login_token", id) }
func (s *Service) failureKey(username string) string {
	sum := sha256.Sum256([]byte(username))
	return s.cache.Key("login_fail", hex.EncodeToString(sum[:]))
}
func (s *Service) ttl() time.Duration { return s.cfg.JWT.Expire }

func (s *Service) permissions(ctx context.Context, userID int64) (permissionSnapshot, error) {
	key := s.cache.Key("user_perms", itoa(userID))
	raw, err := s.cache.Get(ctx, key).Result()
	if err == nil {
		var snapshot permissionSnapshot
		if json.Unmarshal([]byte(raw), &snapshot) == nil && snapshot.Roles != nil && snapshot.Permissions != nil {
			return snapshot, nil
		}
	} else if !errors.Is(err, redis.Nil) {
		return permissionSnapshot{}, err
	}
	roles, perms, err := s.identity.RolesAndPermissions(ctx, userID)
	if err != nil {
		return permissionSnapshot{}, err
	}
	snapshot := permissionSnapshot{Roles: roles, Permissions: perms}
	rawBytes, err := json.Marshal(snapshot)
	if err != nil {
		return permissionSnapshot{}, err
	}
	if err = s.cache.Set(ctx, key, rawBytes, permissionTTL).Err(); err != nil {
		return permissionSnapshot{}, err
	}
	return snapshot, nil
}

// InvalidatePermissions must be called by future role/menu/grant writes. Login
// also drops this cache so a new session never inherits an old authorization.
func (s *Service) InvalidatePermissions(ctx context.Context, userID int64) error {
	return s.cache.Del(ctx, s.cache.Key("user_perms", itoa(userID))).Err()
}

func (s *Service) issueSession(ctx context.Context, user *model.User, ip, agent string) (string, error) {
	id := uuid.NewString()
	session := Session{UserID: user.UserID, UserName: user.UserName, LoginTime: s.now(), IP: ip, UserAgent: truncate(agent, 512)}
	raw, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	token, err := signToken(s.cfg.JWT.Secret, s.cfg.JWT.Issuer, id, s.now())
	if err != nil {
		return "", err
	}
	if err = s.cache.Set(ctx, s.sessionKey(id), raw, s.ttl()).Err(); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Service) loadSession(ctx context.Context, token string) (string, *Session, error) {
	id, err := parseToken(s.cfg.JWT.Secret, s.cfg.JWT.Issuer, token, s.now())
	if err != nil {
		return "", nil, errcode.Unauthorizedf("登录状态无效，请重新登录")
	}
	raw, err := s.cache.Get(ctx, s.sessionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return "", nil, errcode.Unauthorizedf("登录已过期，请重新登录")
	}
	if err != nil {
		return "", nil, err
	}
	var session Session
	if json.Unmarshal(raw, &session) != nil || session.UserID <= 0 {
		return "", nil, errcode.Unauthorizedf("登录状态无效，请重新登录")
	}
	return id, &session, nil
}

var failureScript = redis.NewScript(`
local count=redis.call('INCR',KEYS[1])
if count==1 or count==5 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end
return count
`)

func (s *Service) checkLock(ctx context.Context, username string) error {
	n, err := s.cache.Get(ctx, s.failureKey(username)).Int()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return err
	}
	if n >= 5 {
		return errcode.New(errcode.TooManyReq, "密码错误次数过多，请 10 分钟后重试")
	}
	return nil
}
func (s *Service) failed(ctx context.Context, username string) error {
	n, err := failureScript.Run(ctx, s.cache.Client, []string{s.failureKey(username)}, failureTTL.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if n >= 5 {
		return errcode.New(errcode.TooManyReq, "密码错误次数过多，请 10 分钟后重试")
	}
	return errcode.BadRequestf("用户名或密码错误")
}

// Atomic refresh cannot resurrect a session concurrently deleted by logout.
var refreshScript = redis.NewScript(`
local ttl=redis.call('PTTL',KEYS[1])
if ttl<0 then return -1 end
if ttl<tonumber(ARGV[1]) then redis.call('PEXPIRE',KEYS[1],ARGV[2]); return tonumber(ARGV[2]) end
return ttl
`)

func (s *Service) refresh(ctx context.Context, id string) (time.Duration, error) {
	n, err := refreshScript.Run(ctx, s.cache.Client, []string{s.sessionKey(id)}, (s.ttl() / 3).Milliseconds(), s.ttl().Milliseconds()).Int64()
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, errcode.Unauthorizedf("登录已过期，请重新登录")
	}
	return time.Duration(n) * time.Millisecond, nil
}
func (s *Service) settings(ctx context.Context) (captcha, register bool, err error) {
	captcha, err = s.identity.ConfigBool(ctx, "sys.account.captchaEnabled", true)
	if err != nil {
		return
	}
	register, err = s.identity.ConfigBool(ctx, "sys.account.registerUser", false)
	return
}
func (s *Service) effectiveRisk(user *model.User) capability.Risk {
	global, err := capability.ParseRisk(s.cfg.AI.MaxRisk)
	if err != nil {
		return capability.RiskLow
	}
	if user.AIMaxRisk != nil && *user.AIMaxRisk != "" {
		userRisk, err := capability.ParseRisk(*user.AIMaxRisk)
		if err != nil {
			return capability.RiskLow
		}
		if userRisk.Level() < global.Level() {
			return userRisk
		}
	}
	return global
}
func (s *Service) recordLogin(ctx context.Context, name, ip, agent, status, msg string) {
	if err := s.identity.RecordLogin(ctx, truncate(name, 50), truncate(ip, 128), truncate(agent, 64), status, truncate(msg, 255)); err != nil {
		slog.Error("写入登录日志失败", "error", err)
	}
}
func truncate(v string, max int) string {
	r := []rune(v)
	if len(r) > max {
		return string(r[:max])
	}
	return v
}

// A fixed cost-matched hash avoids skipping bcrypt for an unknown username.
var dummyHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

func verifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

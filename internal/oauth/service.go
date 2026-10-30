// Package oauth implements the public-client authorization-code flow for MCP.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type ActorResolver func(context.Context, int64, []string, capability.Risk) (*capability.Actor, error)
type Options struct {
	Issuer       string
	ResourcePath string
	ConsentPath  string
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
}
type Service struct {
	db      *gorm.DB
	cache   *cache.Cache
	opts    Options
	resolve ActorResolver
	now     func() time.Time
	Audit   func(context.Context, capability.AuditRecord)
}
type client struct {
	ClientID     string `gorm:"primaryKey"`
	ClientName   string
	RedirectURIs json.RawMessage `gorm:"type:jsonb"`
	CreateTime   time.Time
}

func (client) TableName() string { return "sys_oauth_client" }

type request struct {
	RequestID   string `gorm:"primaryKey"`
	ClientID    string
	RedirectURI string
	State       string
	Challenge   string
	Scopes      json.RawMessage `gorm:"type:jsonb"`
	Resource    string
	UserID      *int64
	Status      string
	CodeHash    *string
	GrantID     *int64
	ExpiresAt   time.Time
	CreateTime  time.Time
}

func (request) TableName() string { return "sys_oauth_request" }

type grant struct {
	GrantID     int64 `gorm:"primaryKey"`
	ClientID    string
	UserID      int64
	Scopes      json.RawMessage `gorm:"type:jsonb"`
	Permissions json.RawMessage `gorm:"type:jsonb"`
	Resource    string
	ExpiresAt   time.Time
	RevokedAt   *time.Time
	CreateTime  time.Time
}

func (grant) TableName() string { return "sys_oauth_grant" }

type token struct {
	TokenID          int64 `gorm:"primaryKey"`
	GrantID          int64
	AccessHash       string
	RefreshHash      *string
	AccessExpiresAt  time.Time
	RefreshExpiresAt *time.Time
	RefreshUsedAt    *time.Time
	RevokedAt        *time.Time
	CreateTime       time.Time
}

func (token) TableName() string { return "sys_oauth_token" }

type Scope struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

var scopes = []Scope{{"mcp:read", "读取当前账号有权访问的 MCP 功能及知识资源"}, {"mcp:write", "在当前账号权限及风险上限内预览、确认并执行写操作"}, {"offline_access", "允许客户端通过轮换的刷新令牌保持授权，最长 30 天"}}
var verifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
var challengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func New(db *gorm.DB, c *cache.Cache, opts Options, resolver ActorResolver) (*Service, error) {
	u, err := validRedirect(opts.Issuer)
	if err != nil || u.RawQuery != "" || u.ForceQuery || u.Path != "" && u.Path != "/" || resolver == nil {
		return nil, errors.New("OAuth issuer must be a canonical HTTPS origin (HTTP numeric loopback is allowed for development)")
	}
	opts.Issuer = strings.TrimSuffix(opts.Issuer, "/")
	if !strings.HasPrefix(opts.ResourcePath, "/") || strings.ContainsAny(opts.ResourcePath, "?#") {
		return nil, errors.New("OAuth resource path must be absolute")
	}
	if opts.ConsentPath == "" {
		opts.ConsentPath = "/oauth/consent"
	}
	if !strings.HasPrefix(opts.ConsentPath, "/") || strings.HasPrefix(opts.ConsentPath, "//") || strings.ContainsAny(opts.ConsentPath, "?#\\") {
		return nil, errors.New("invalid OAuth consent path")
	}
	if opts.AccessTTL <= 0 {
		opts.AccessTTL = 15 * time.Minute
	}
	if opts.RefreshTTL <= 0 {
		opts.RefreshTTL = 30 * 24 * time.Hour
	}
	return &Service{db: db, cache: c, opts: opts, resolve: resolver, now: time.Now}, nil
}
func (s *Service) Resource() string { return s.opts.Issuer + s.opts.ResourcePath }
func (s *Service) ResourceMetadataURL() string {
	return s.opts.Issuer + "/.well-known/oauth-protected-resource" + s.opts.ResourcePath
}
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func secret(prefix string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}
func encoded(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func stringList(v json.RawMessage) []string {
	out := []string{}
	_ = json.Unmarshal(v, &out)
	return out
}
func validRedirect(raw string) (*url.URL, error) {
	if len(raw) > 2048 || strings.ContainsAny(raw, "\\\r\n\t") {
		return nil, errors.New("invalid redirect URI")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || strings.Contains(raw, "#") || u.Opaque != "" {
		return nil, errors.New("invalid redirect URI")
	}
	if u.Port() != "" {
		port, e := strconv.Atoi(u.Port())
		if e != nil || port < 1 || port > 65535 {
			return nil, errors.New("invalid redirect port")
		}
	}
	if u.Scheme == "https" {
		return u, nil
	}
	ip, e := netip.ParseAddr(u.Hostname())
	if u.Scheme == "http" && e == nil && ip.IsLoopback() && ip.Zone() == "" {
		return u, nil
	}
	return nil, errors.New("redirect URI requires HTTPS or numeric loopback HTTP")
}
func redirectMatches(registered, actual string) bool {
	r, e := validRedirect(registered)
	if e != nil {
		return false
	}
	a, e := validRedirect(actual)
	if e != nil {
		return false
	}
	if r.Scheme == "http" && a.Scheme == "http" {
		return r.Hostname() == a.Hostname() && r.EscapedPath() == a.EscapedPath() && r.RawQuery == a.RawQuery
	}
	return registered == actual
}
func parseScopes(raw string) ([]string, error) {
	list := strings.Fields(raw)
	if len(list) == 0 {
		list = []string{"mcp:read"}
	}
	for _, v := range list {
		if !slices.ContainsFunc(scopes, func(s Scope) bool { return s.Name == v }) {
			return nil, protocolError("invalid_scope", "不支持的授权范围")
		}
	}
	slices.Sort(list)
	return slices.Compact(list), nil
}
func permissions(actor *capability.Actor, granted []string) []string {
	out := []string{}
	write := slices.Contains(granted, "mcp:write")
	read := slices.Contains(granted, "mcp:read")
	for _, e := range capability.Visible(capability.SourceMCP, actor.Perms, actor.MaxRisk) {
		if e.Risk == capability.RiskCritical || strings.HasPrefix(e.Name, "mcp.resource.") || e.Mutating && !write || !e.Mutating && !read || e.Perm == "" {
			continue
		}
		out = append(out, e.Perm)
	}
	slices.Sort(out)
	return slices.Compact(out)
}
func (s *Service) grantActor(ctx context.Context, g *grant) (*capability.Actor, error) {
	if g.RevokedAt != nil || !g.ExpiresAt.After(s.now()) || g.Resource != s.Resource() {
		return nil, errcode.Unauthorizedf("OAuth 授权已失效")
	}
	actor, err := s.resolve(ctx, g.UserID, stringList(g.Permissions), capability.RiskHigh)
	if err != nil {
		return nil, err
	}
	actor.ReadOnly = !slices.Contains(stringList(g.Scopes), "mcp:write")
	return actor, nil
}
func (s *Service) Authenticate(ctx context.Context, plain string) (*capability.Actor, error) {
	if !strings.HasPrefix(plain, "iao_") || len(plain) != 47 {
		return nil, errcode.Unauthorizedf("OAuth 令牌无效")
	}
	var t token
	if err := dbx.DBFrom(ctx, s.db).Where("access_hash=?", digest(plain)).First(&t).Error; err != nil {
		return nil, errcode.Unauthorizedf("OAuth 令牌无效")
	}
	if t.RevokedAt != nil || !t.AccessExpiresAt.After(s.now()) {
		return nil, errcode.Unauthorizedf("OAuth 令牌已过期或撤销")
	}
	var g grant
	if err := dbx.DBFrom(ctx, s.db).First(&g, t.GrantID).Error; err != nil {
		return nil, errcode.Unauthorizedf("OAuth 授权无效")
	}
	a, err := s.grantActor(ctx, &g)
	if err != nil {
		return nil, err
	}
	a.TokenID = -t.TokenID
	return a, nil
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope"`
}

func (s *Service) issue(tx *gorm.DB, g *grant) (*TokenResponse, error) {
	access, err := secret("iao_")
	if err != nil {
		return nil, err
	}
	now := s.now()
	expires := now.Add(s.opts.AccessTTL)
	if expires.After(g.ExpiresAt) {
		expires = g.ExpiresAt
	}
	t := token{GrantID: g.GrantID, AccessHash: digest(access), AccessExpiresAt: expires, CreateTime: now}
	out := &TokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int64(expires.Sub(now).Seconds()), Scope: strings.Join(stringList(g.Scopes), " ")}
	if slices.Contains(stringList(g.Scopes), "offline_access") {
		refresh, err := secret("ior_")
		if err != nil {
			return nil, err
		}
		hash := digest(refresh)
		t.RefreshHash = &hash
		t.RefreshExpiresAt = &g.ExpiresAt
		out.RefreshToken = refresh
	}
	if err := tx.Create(&t).Error; err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Service) exchange(ctx context.Context, values url.Values) (*TokenResponse, error) {
	var out *TokenResponse
	var rejected error
	err := dbx.DBFrom(ctx, s.db).Transaction(func(tx *gorm.DB) error {
		switch values.Get("grant_type") {
		case "authorization_code":
			var r request
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code_hash=?", digest(values.Get("code"))).First(&r).Error; err != nil {
				return protocolError("invalid_grant", "授权码无效")
			}
			if r.ClientID != values.Get("client_id") || r.RedirectURI != values.Get("redirect_uri") || values.Get("resource") != "" && values.Get("resource") != r.Resource {
				return protocolError("invalid_grant", "授权码与客户端、回调或资源不匹配")
			}
			verifier := values.Get("code_verifier")
			sum := sha256.Sum256([]byte(verifier))
			challenge := base64.RawURLEncoding.EncodeToString(sum[:])
			if !verifierPattern.MatchString(verifier) || subtle.ConstantTimeCompare([]byte(challenge), []byte(r.Challenge)) != 1 {
				return protocolError("invalid_grant", "PKCE 校验失败")
			}
			if r.Status == "consumed" {
				if r.GrantID != nil {
					if err := tx.Model(&grant{}).Where("grant_id=?", *r.GrantID).Update("revoked_at", s.now()).Error; err != nil {
						return err
					}
				}
				rejected = protocolError("invalid_grant", "授权码已使用，原授权已撤销")
				return nil
			}
			if r.Status != "approved" || !r.ExpiresAt.After(s.now()) || r.GrantID == nil {
				return protocolError("invalid_grant", "授权码已过期或无效")
			}
			var g grant
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&g, *r.GrantID).Error; err != nil {
				return err
			}
			if _, err := s.grantActor(ctx, &g); err != nil {
				return protocolError("invalid_grant", "授权用户或权限已失效")
			}
			if err := tx.Model(&r).Update("status", "consumed").Error; err != nil {
				return err
			}
			var err error
			out, err = s.issue(tx, &g)
			return err
		case "refresh_token":
			var t token
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("refresh_hash=?", digest(values.Get("refresh_token"))).First(&t).Error; err != nil {
				return protocolError("invalid_grant", "刷新令牌无效")
			}
			var g grant
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&g, t.GrantID).Error; err != nil {
				return err
			}
			if g.ClientID != values.Get("client_id") || values.Get("resource") != "" && values.Get("resource") != g.Resource {
				return protocolError("invalid_grant", "刷新令牌与客户端或资源不匹配")
			}
			if t.RefreshUsedAt != nil {
				if err := tx.Model(&g).Update("revoked_at", s.now()).Error; err != nil {
					return err
				}
				rejected = protocolError("invalid_grant", "检测到刷新令牌重复使用，原授权已撤销")
				return nil
			}
			if t.RevokedAt != nil || t.RefreshExpiresAt == nil || !t.RefreshExpiresAt.After(s.now()) {
				return protocolError("invalid_grant", "刷新令牌已过期或撤销")
			}
			actor, err := s.grantActor(ctx, &g)
			if err != nil {
				return protocolError("invalid_grant", "授权用户或权限已失效")
			}
			if requested := values.Get("scope"); requested != "" {
				list, err := parseScopes(requested)
				if err != nil {
					return err
				}
				for _, v := range list {
					if !slices.Contains(stringList(g.Scopes), v) {
						return protocolError("invalid_scope", "不能扩大授权范围")
					}
				}
				g.Scopes = encoded(list)
				g.Permissions = encoded(permissions(actor, list))
				if err := tx.Model(&g).Updates(map[string]any{"scopes": g.Scopes, "permissions": g.Permissions}).Error; err != nil {
					return err
				}
			}
			now := s.now()
			if err := tx.Model(&t).Updates(map[string]any{"refresh_used_at": now, "revoked_at": now}).Error; err != nil {
				return err
			}
			out, err = s.issue(tx, &g)
			return err
		default:
			return protocolError("unsupported_grant_type", "仅支持 authorization_code 和 refresh_token")
		}
	})
	if err != nil {
		return nil, err
	}
	if rejected != nil {
		return nil, rejected
	}
	return out, nil
}

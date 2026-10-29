// Package agent assembles Eino ADK with the capability-backed read and plan
// tools. Human confirmation is persisted in plans, never in model state.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/aiprovider"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/knowledge"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/plan"
)

type ModelProvider interface {
	ChatModel(context.Context, string) (model.ToolCallingChatModel, error)
	Models(context.Context) ([]aiprovider.ModelInfo, error)
}

type Service struct {
	db        *gorm.DB
	cache     *cache.Cache
	cfg       *config.Config
	models    ModelProvider
	plans     *plan.Service
	knowledge *knowledge.Service
	mu        sync.Mutex
	cancels   map[string]context.CancelFunc
}

func New(db *gorm.DB, c *cache.Cache, cfg *config.Config, models ModelProvider, plans *plan.Service, k *knowledge.Service) *Service {
	return &Service{db: db, cache: c, cfg: cfg, models: models, plans: plans, knowledge: k, cancels: map[string]context.CancelFunc{}}
}

type Message struct {
	ID       string                `json:"id"`
	Role     string                `json:"role"`
	Content  string                `json:"content"`
	Time     time.Time             `json:"time"`
	PlanID   string                `json:"planId,omitempty"`
	Evidence []capability.Evidence `json:"evidence,omitempty"`
}
type Session struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Model     string            `json:"model"`
	UpdatedAt time.Time         `json:"updatedAt"`
	Messages  []Message         `json:"messages"`
	History   []*schema.Message `json:"-"`
}
type storedSession struct {
	Session
	History []*schema.Message `json:"history"`
}

func (s *Service) ttl() time.Duration {
	if s.cfg.AI.SessionTTL > 0 {
		return s.cfg.AI.SessionTTL
	}
	return 7 * 24 * time.Hour
}
func (s *Service) sessionKey(user int64, id string) string {
	return s.cache.Key("ai:session", strconv.FormatInt(user, 10), id)
}
func (s *Service) indexKey(user int64) string {
	return s.cache.Key("ai:session:index", strconv.FormatInt(user, 10))
}
func (s *Service) runningKey(user int64, id string) string {
	return s.cache.Key("ai:running", strconv.FormatInt(user, 10), id)
}

func (s *Service) saveSession(ctx context.Context, user int64, session *Session) error {
	return s.persistSession(ctx, user, session, false)
}

var saveSessionScript = redis.NewScript(`
if ARGV[5]=='1' and redis.call('EXISTS',KEYS[1])==0 then return 0 end
redis.call('PSETEX',KEYS[1],ARGV[2],ARGV[1])
redis.call('ZADD',KEYS[2],ARGV[3],ARGV[4])
redis.call('PEXPIRE',KEYS[2],ARGV[2])
return 1
`)

func (s *Service) persistSession(ctx context.Context, user int64, session *Session, existingOnly bool) error {
	session.UpdatedAt = time.Now().UTC()
	if len(session.Messages) > 200 {
		session.Messages = session.Messages[len(session.Messages)-200:]
	}
	if len(session.History) > 60 {
		// Retain complete turns. Starting at a user message avoids orphaned
		// tool results when providers validate tool-call history strictly.
		start := len(session.History) - 60
		for start < len(session.History) && session.History[start].Role != schema.User {
			start++
		}
		session.History = session.History[start:]
	}
	raw, err := json.Marshal(storedSession{Session: *session, History: session.History})
	if err != nil {
		return err
	}
	only := "0"
	if existingOnly {
		only = "1"
	}
	n, err := saveSessionScript.Run(ctx, s.cache.Client, []string{s.sessionKey(user, session.ID), s.indexKey(user)}, raw, s.ttl().Milliseconds(), session.UpdatedAt.UnixMilli(), session.ID, only).Int()
	if err != nil {
		return err
	}
	if n == 0 {
		return errcode.NotFoundf("会话已删除或过期")
	}
	return nil
}
func (s *Service) loadSession(ctx context.Context, user int64, id string) (*Session, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, errcode.NotFoundf("会话不存在")
	}
	raw, err := s.cache.Get(ctx, s.sessionKey(user, id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, errcode.NotFoundf("会话不存在或已过期")
	}
	if err != nil {
		return nil, err
	}
	var saved storedSession
	if err = json.Unmarshal(raw, &saved); err != nil {
		return nil, err
	}
	saved.Session.History = saved.History
	return &saved.Session, nil
}
func (s *Service) newSession(ctx context.Context, user int64, title, modelName string) (*Session, error) {
	r := []rune(title)
	if len(r) > 60 {
		title = string(r[:60])
	}
	if title == "" {
		title = "新会话"
	}
	se := &Session{ID: uuid.NewString(), Title: title, Model: modelName, Messages: []Message{}, History: []*schema.Message{}}
	return se, s.saveSession(ctx, user, se)
}

func (s *Service) aiActor(ctx context.Context, intent string) (*capability.Actor, error) {
	current, ok := capability.ActorFrom(ctx)
	if !ok {
		return nil, errcode.Unauthorizedf("请先登录")
	}
	global, err := capability.ParseRisk(s.cfg.AI.MaxRisk)
	if err != nil {
		return nil, err
	}
	var user struct {
		AIMaxRisk *string
		Status    string
		DelFlag   string
	}
	if err = s.db.WithContext(ctx).Table("sys_user").Select("ai_max_risk,status,del_flag").Where("user_id = ?", current.UserID).Take(&user).Error; err != nil {
		return nil, err
	}
	if user.Status != "0" || user.DelFlag != "0" {
		return nil, errcode.Unauthorizedf("用户已停用")
	}
	if user.AIMaxRisk != nil && *user.AIMaxRisk != "" {
		ceiling, e := capability.ParseRisk(*user.AIMaxRisk)
		if e != nil {
			global = capability.RiskLow
		} else if ceiling.Level() < global.Level() {
			global = ceiling
		}
	}
	a := *current
	a.Type = capability.ActorAgent
	a.Source = capability.SourceAI
	a.MaxRisk = global
	a.Intent = intent
	a.ApprovalMode = "auto"
	return &a, nil
}

var requestRate = redis.NewScript(`local n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('PEXPIRE',KEYS[1],60000) end;return n`)

func (s *Service) rate(ctx context.Context, user int64) error {
	n, err := requestRate.Run(ctx, s.cache.Client, []string{s.cache.Key("ratelimit", "ai", strconv.FormatInt(user, 10))}).Int()
	if err != nil {
		return err
	}
	if n > 20 {
		return errcode.New(errcode.TooManyReq, "AI 请求过于频繁，请稍后重试")
	}
	return nil
}

var releaseLock = redis.NewScript(`if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end;return 0`)

func (s *Service) begin(ctx context.Context, user int64, id string) (context.Context, func(), error) {
	key := s.runningKey(user, id)
	runID := uuid.NewString()
	ok, err := s.cache.SetNX(ctx, key, runID, 10*time.Minute).Result()
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, errcode.Conflictf("该会话正在生成，请先中止或等待完成")
	}
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	s.mu.Lock()
	s.cancels[key] = cancel
	s.mu.Unlock()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				stop, _ := s.cache.Get(runCtx, key+":stop").Result()
				if stop == runID {
					cancel()
					return
				}
			}
		}
	}()
	cleanup := func() {
		cancel()
		s.mu.Lock()
		delete(s.cancels, key)
		s.mu.Unlock()
		cleanCtx, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		_ = releaseLock.Run(cleanCtx, s.cache.Client, []string{key}, runID).Err()
		_ = releaseLock.Run(cleanCtx, s.cache.Client, []string{key + ":stop"}, runID).Err()
	}
	return runCtx, cleanup, nil
}
func (s *Service) stop(ctx context.Context, user int64, id string) error {
	if _, err := s.loadSession(ctx, user, id); err != nil {
		return err
	}
	key := s.runningKey(user, id)
	runID, err := s.cache.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = s.cache.Set(ctx, key+":stop", runID, time.Minute).Err(); err != nil {
		return err
	}
	s.mu.Lock()
	cancel := s.cancels[key]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

type checkpointStore struct {
	cache  *cache.Cache
	prefix string
	ttl    time.Duration
}

func (c checkpointStore) Get(ctx context.Context, id string) ([]byte, bool, error) {
	raw, err := c.cache.Get(ctx, c.prefix+id).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	return raw, err == nil, err
}
func (c checkpointStore) Set(ctx context.Context, id string, raw []byte) error {
	return c.cache.Set(ctx, c.prefix+id, raw, c.ttl).Err()
}
func (c checkpointStore) Delete(ctx context.Context, id string) error {
	return c.cache.Del(ctx, c.prefix+id).Err()
}

func sessionError(err error) string {
	if errors.Is(err, context.Canceled) {
		return "生成已中止"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "生成超时，请缩小任务后重试"
	}
	e := errcode.From(err)
	if e.Code == 500 {
		return "模型调用失败，请检查模型服务配置与网络连接"
	}
	return e.Msg
}

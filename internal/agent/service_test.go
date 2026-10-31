package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"

	"github.com/junler/intent-admin/internal/aiprovider"
	"github.com/junler/intent-admin/internal/auth"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type fixedModel struct {
	mu       sync.Mutex
	messages []*schema.Message
	index    int
}

func (m *fixedModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) { return m, nil }
func (m *fixedModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.index >= len(m.messages) {
		return schema.AssistantMessage("完成", nil), nil
	}
	out := m.messages[m.index]
	m.index++
	return out, nil
}
func (m *fixedModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	out, err := m.Generate(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{out}), nil
}

type fixedProvider struct{ cm model.ToolCallingChatModel }

func (p fixedProvider) ChatModel(context.Context, string) (model.ToolCallingChatModel, error) {
	return p.cm, nil
}
func (p fixedProvider) Models(context.Context) ([]aiprovider.ModelInfo, error) {
	return []aiprovider.ModelInfo{}, nil
}
func sessionService(t *testing.T) (*Service, *miniredis.Miniredis) {
	t.Helper()
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cfg := &config.Config{AI: config.AI{SessionTTL: time.Hour, MaxToolCalls: 5}}
	return New(nil, &cache.Cache{Client: client}, cfg, nil, nil, nil), r
}

func TestSessionOwnershipAndExpiry(t *testing.T) {
	s, r := sessionService(t)
	ctx := context.Background()
	session, err := s.newSession(ctx, 1, "测试", "fake")
	if err != nil {
		t.Fatal(err)
	}
	session.History = []*schema.Message{schema.UserMessage("private")}
	if err = s.saveSession(ctx, 1, session); err != nil {
		t.Fatal(err)
	}
	if _, err = s.loadSession(ctx, 2, session.ID); errcode.From(err).Code != 404 {
		t.Fatalf("cross-owner session allowed: %v", err)
	}
	loaded, err := s.loadSession(ctx, 1, session.ID)
	if err != nil || len(loaded.History) != 1 {
		t.Fatalf("history not restored: %v", err)
	}
	raw, _ := json.Marshal(loaded)
	if string(raw) == "" || contains(raw, "private") {
		t.Fatal("private model history leaked in session JSON")
	}
	r.FastForward(time.Hour + time.Second)
	if _, err = s.loadSession(ctx, 1, session.ID); errcode.From(err).Code != 404 {
		t.Fatalf("expired session survived: %v", err)
	}
}
func contains(raw []byte, value string) bool {
	for i := 0; i+len(value) <= len(raw); i++ {
		if string(raw[i:i+len(value)]) == value {
			return true
		}
	}
	return false
}

func TestRunningLockAndStop(t *testing.T) {
	s, _ := sessionService(t)
	ctx := context.Background()
	session, err := s.newSession(ctx, 1, "test", "")
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cleanup, err := s.begin(ctx, 1, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, _, err = s.begin(ctx, 1, session.ID); errcode.From(err).Code != 409 {
		t.Fatalf("parallel run allowed: %v", err)
	}
	if err = s.stop(ctx, 1, session.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel active model call")
	}
}

func TestEinoRunnerQueriesThenAnswers(t *testing.T) {
	s, _ := sessionService(t)
	var reads atomic.Int64
	capability.Register(capability.Capability[struct{}, map[string]any]{Meta: capability.Meta{Name: "test.einoRead", Summary: "统计用户", Risk: capability.RiskLow, AIEnabled: true}, Fn: func(context.Context, struct{}) (map[string]any, error) {
		reads.Add(1)
		return map[string]any{"total": 3}, nil
	}})
	cm := &fixedModel{messages: []*schema.Message{schema.AssistantMessage("", []schema.ToolCall{{ID: "call-query", Type: "function", Function: schema.FunctionCall{Name: "query", Arguments: `{"name":"test.einoRead","args":{}}`}}}), schema.AssistantMessage("共有 3 位用户。", nil)}}
	s.models = fixedProvider{cm: cm}
	actor := &capability.Actor{UserID: 1, UserName: "test", Source: capability.SourceAI, Type: capability.ActorAgent, MaxRisk: capability.RiskHigh, Perms: []string{capability.SuperPerm}}
	ctx := capability.WithActor(context.Background(), actor)
	session, err := s.newSession(ctx, 1, "有几个用户", "")
	if err != nil {
		t.Fatal(err)
	}
	events := []string{}
	err = s.run(ctx, actor, session, ChatInput{Message: "有几个用户"}, func(kind string, _ any) { events = append(events, kind) })
	if err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 1 {
		t.Fatalf("real query calls=%d", reads.Load())
	}
	if len(session.Messages) != 2 || session.Messages[1].Content != "共有 3 位用户。" {
		t.Fatalf("messages=%+v", session.Messages)
	}
	found := false
	for _, e := range events {
		if e == "answer" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing structured answer: %v", events)
	}
}

func TestPageContextBounds(t *testing.T) {
	if err := validateChat(ChatInput{Message: "hello", PageContext: map[string]any{"selectedIds": make([]any, 501)}}); err == nil {
		t.Fatal("unbounded selection accepted")
	}
}

func TestLateCompletionCannotRestoreDeletedSession(t *testing.T) {
	s, _ := sessionService(t)
	ctx := context.Background()
	se, err := s.newSession(ctx, 1, "delete", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.cache.Del(ctx, s.sessionKey(1, se.ID)).Err(); err != nil {
		t.Fatal(err)
	}
	if err = s.finishSession(ctx, 1, se, "late answer", nil); err != nil {
		t.Fatal(err)
	}
	if n := s.cache.Exists(ctx, s.sessionKey(1, se.ID)).Val(); n != 0 {
		t.Fatal("deleted session resurrected")
	}
}

type fixedNavigation struct{}

func (fixedNavigation) Routes(context.Context) ([]auth.Route, error) {
	return []auth.Route{{Path: "/system/user", Component: "system/user/index", Meta: auth.RouteMeta{Title: "用户管理"}}}, nil
}

func TestNavigationReturnsDirectlyAndPersistsDestination(t *testing.T) {
	s, _ := sessionService(t)
	cm := &fixedModel{messages: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{ID: "open-page", Type: "function", Function: schema.FunctionCall{Name: "navigate", Arguments: `{"route":"/system/user"}`}}}),
		schema.AssistantMessage("should not require another model response", nil),
	}}
	s.models = fixedProvider{cm: cm}
	s.Navigation = fixedNavigation{}
	a := &capability.Actor{UserID: 1, Source: capability.SourceAI, Type: capability.ActorAgent, MaxRisk: capability.RiskLow}
	ctx := capability.WithActor(context.Background(), a)
	session, err := s.newSession(ctx, 1, "打开用户管理", "")
	if err != nil {
		t.Fatal(err)
	}
	navigations := 0
	err = s.run(ctx, a, session, ChatInput{Message: "打开用户管理"}, func(kind string, data any) {
		if kind == "navigate" {
			navigations++
		}
		if kind == "plan" {
			t.Fatal("page navigation created a business plan")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.loadSession(ctx, 1, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if navigations != 1 || cm.index != 1 || len(saved.Messages) != 2 {
		t.Fatalf("navigations=%d, model calls=%d, messages=%+v", navigations, cm.index, saved.Messages)
	}
	reply := saved.Messages[1]
	if !reply.Navigated || reply.Content != "已打开用户管理" || len(reply.Evidence) != 1 || reply.Evidence[0].Route != "/system/user" {
		t.Fatalf("navigation destination was not retained: %+v", reply)
	}
}

func TestSuggestedPageIsSavedWithoutAutomaticNavigation(t *testing.T) {
	s, _ := sessionService(t)
	cm := &fixedModel{messages: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{ID: "suggest-page", Type: "function", Function: schema.FunctionCall{Name: "suggest_page", Arguments: `{"route":"/system/user"}`}}}),
		schema.AssistantMessage("查询已完成。", nil),
	}}
	s.models = fixedProvider{cm: cm}
	s.Navigation = fixedNavigation{}
	a := &capability.Actor{UserID: 1, Source: capability.SourceAI, Type: capability.ActorAgent, MaxRisk: capability.RiskLow}
	ctx := capability.WithActor(context.Background(), a)
	session, err := s.newSession(ctx, 1, "帮我查询用户", "")
	if err != nil {
		t.Fatal(err)
	}
	err = s.run(ctx, a, session, ChatInput{Message: "帮我查询用户"}, func(kind string, data any) {
		if kind == "navigate" || kind == "plan" {
			t.Fatalf("suggestion triggered %s: %+v", kind, data)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.loadSession(ctx, 1, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Messages) != 2 || saved.Messages[1].Navigated || saved.Messages[1].Content != "查询已完成。" || len(saved.Messages[1].Evidence) != 1 || saved.Messages[1].Evidence[0].Route != "/system/user" {
		t.Fatalf("suggestion not saved with answer: %+v", saved.Messages)
	}
}

func TestUserFormHandoffDoesNotPersistPasswordOrCreateAPlan(t *testing.T) {
	s, redis := sessionService(t)
	const password = `test-"pass-42`
	args, _ := json.Marshal(map[string]any{"userName": "test_user", "nickName": "测试用户", "password": password})
	cm := &fixedModel{messages: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{ID: "fill-form", Type: "function", Function: schema.FunctionCall{Name: "fill_user_form", Arguments: string(args)}}}),
		schema.AssistantMessage("must return directly", nil),
	}}
	s.models = fixedProvider{cm: cm}
	s.Navigation = fixedNavigation{}
	a := &capability.Actor{UserID: 1, Source: capability.SourceAI, Type: capability.ActorAgent, MaxRisk: capability.RiskLow, Perms: []string{"system:user:add"}}
	ctx := capability.WithActor(context.Background(), a)
	request := "创建测试用户，初始密码是 " + password
	session, err := s.newSession(ctx, 1, request, "")
	if err != nil {
		t.Fatal(err)
	}
	fills := 0
	err = s.run(ctx, a, session, ChatInput{Message: request}, func(kind string, data any) {
		if kind == "plan" {
			t.Fatal("form handoff created a business plan")
		}
		if kind == "form_fill" {
			fills++
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if fills != 1 || cm.index != 1 {
		t.Fatal("expected a single direct form handoff")
	}
	saved, err := s.loadSession(ctx, 1, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Messages) != 2 || !saved.Messages[1].Navigated {
		t.Fatal("form handoff transcript was not retained")
	}
	for _, key := range redis.Keys() {
		value, err := redis.Get(key)
		if err == nil {
			escaped, _ := json.Marshal(password)
			if strings.Contains(value, password) || strings.Contains(value, string(escaped)) {
				t.Fatal("credential remained in persisted session or checkpoint")
			}
		}
	}
}

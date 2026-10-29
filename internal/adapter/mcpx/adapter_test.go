package mcpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/redis/go-redis/v9"

	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type writeIn struct {
	Amount int64 `json:"amount"`
}

func setup(t *testing.T, mode string, risk capability.Risk) (*Adapter, *atomic.Int64, *atomic.Int64, *atomic.Bool) {
	t.Helper()
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	value, writes, revoked := &atomic.Int64{}, &atomic.Int64{}, &atomic.Bool{}
	prefix := "test.mcp" + strings.ReplaceAll(t.Name(), "/", "")
	capability.Register(capability.Capability[struct{}, map[string]any]{Meta: capability.Meta{Name: prefix + "Read", Summary: "读取", Perm: "test:read", Risk: capability.RiskLow, MCPEnabled: true}, Fn: func(context.Context, struct{}) (map[string]any, error) {
		return map[string]any{"value": value.Load()}, nil
	}})
	capability.Register(capability.Capability[writeIn, map[string]any]{Meta: capability.Meta{Name: prefix + "Write", Summary: "修改", Perm: "test:write", Risk: capability.RiskMedium, MCPEnabled: true, Mutating: true}, Fn: func(_ context.Context, in writeIn) (map[string]any, error) {
		writes.Add(1)
		value.Store(in.Amount)
		return map[string]any{"value": in.Amount}, nil
	}, DryRun: func(_ context.Context, in writeIn) (*capability.Preview, error) {
		p := &capability.Preview{}
		p.AddChange(capability.Change{Entity: "test", ID: "1", Op: capability.OpUpdate, Fields: []capability.FieldDiff{{Field: "amount", Before: value.Load(), After: in.Amount}}})
		return p, nil
	}})
	cfg := &config.Config{MCP: config.MCP{Enabled: true, ToolMode: mode, MaxRisk: string(risk), ConfirmTTL: 5 * time.Minute, RatePerMin: 100}}
	auth := func(context.Context, string) (*capability.Actor, error) {
		if revoked.Load() {
			return nil, errcode.Unauthorizedf("revoked")
		}
		return &capability.Actor{Type: capability.ActorMCPClient, Source: capability.SourceMCP, UserID: 1, TokenID: 42, Perms: []string{"test:read", "test:write"}, MaxRisk: risk}, nil
	}
	a := New(&cache.Cache{Client: client}, cfg, auth, nil)
	a.SetConfirmationRunner(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	return a, value, writes, revoked
}
func connect(t *testing.T, a *Adapter) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	actor, err := a.authenticate(ctx, "pat")
	if err != nil {
		t.Fatal(err)
	}
	server := a.Server(actor, "pat")
	one, two := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, one, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, two, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}
func call(t *testing.T, c *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	out, err := c.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func payload(t *testing.T, r *mcp.CallToolResult) map[string]any {
	t.Helper()
	raw, err := json.Marshal(r.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("structured content=%s: %v", raw, err)
	}
	return data
}

func TestMCPConfirmationBinding(t *testing.T) {
	a, value, writes, revoked := setup(t, "meta", capability.RiskMedium)
	client := connect(t, a)
	ctx := context.Background()
	name := "test.mcp" + t.Name() + "Write"
	tools, err := client.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 5 {
		t.Fatalf("meta tools=%d", len(tools.Tools))
	}
	first := call(t, client, "invoke_capability", map[string]any{"name": name, "args": map[string]any{"amount": 5}, "intent": "设置为5"})
	if first.IsError {
		t.Fatal(first)
	}
	token := payload(t, first)["confirmToken"].(string)
	if token == "***" || len(token) < 20 {
		t.Fatalf("confirmation missing: %q", token)
	}
	if writes.Load() != 0 {
		t.Fatal("preview executed write")
	}
	bad := call(t, client, "confirm_plan", map[string]any{"confirmToken": token, "args": map[string]any{"amount": 6}})
	if !bad.IsError || payload(t, bad)["code"] != float64(400) {
		t.Fatalf("modified args accepted: %+v", bad)
	}
	first = call(t, client, "invoke_capability", map[string]any{"name": name, "args": map[string]any{"amount": 5}})
	token = payload(t, first)["confirmToken"].(string)
	ok := call(t, client, "confirm_plan", map[string]any{"confirmToken": token})
	if ok.IsError || value.Load() != 5 || writes.Load() != 1 {
		t.Fatalf("confirm failed: %+v value=%d writes=%d", ok, value.Load(), writes.Load())
	}
	replay := call(t, client, "confirm_plan", map[string]any{"confirmToken": token})
	if !replay.IsError || writes.Load() != 1 {
		t.Fatal("confirmation replayed")
	}
	first = call(t, client, "invoke_capability", map[string]any{"name": name, "args": map[string]any{"amount": 7}})
	token = payload(t, first)["confirmToken"].(string)
	value.Store(9)
	stale := call(t, client, "confirm_plan", map[string]any{"confirmToken": token})
	if !stale.IsError || payload(t, stale)["code"] != float64(412) || writes.Load() != 1 {
		t.Fatal("changed preview accepted")
	}
	revoked.Store(true)
	denied := call(t, client, "list_capabilities", map[string]any{})
	if !denied.IsError || payload(t, denied)["code"] != float64(401) {
		t.Fatal("existing MCP connection survived PAT revocation")
	}
}

func TestMCPReadonlyCatalogue(t *testing.T) {
	a, _, writes, _ := setup(t, "flat", capability.RiskLow)
	client := connect(t, a)
	tools, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range tools.Tools {
		if strings.HasSuffix(entry.Name, "Write") {
			t.Fatalf("write tool exposed at low risk: %s", entry.Name)
		}
	}
	name := "test.mcp" + t.Name() + "Write"
	actor, _ := a.authenticate(context.Background(), "pat")
	_, err = a.Invoke(capability.WithActor(context.Background(), actor), invocation{Name: name, Args: json.RawMessage(`{"amount":1}`)})
	if errcode.From(err).Code != 404 || writes.Load() != 0 {
		t.Fatalf("direct write not hidden: %v", err)
	}
}

func TestHTTPRequiresPAT(t *testing.T) {
	a, _, _, revoked := setup(t, "meta", capability.RiskLow)
	revoked.Store(true)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "http://127.0.0.1/mcp", strings.NewReader(`{}`)))
	if w.Code != 401 || w.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("HTTP %d headers %v", w.Code, w.Header())
	}
}

func TestCanonicalHashPreservesLargeIntegers(t *testing.T) {
	a, err := hashJSON(json.RawMessage(`{"value":9007199254740992}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := hashJSON(json.RawMessage(`{"value":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("distinct integer snapshots hash identically")
	}
}

func TestConfirmationRechecksAfterTransactionGuard(t *testing.T) {
	a, value, writes, _ := setup(t, "meta", capability.RiskMedium)
	actor, _ := a.authenticate(context.Background(), "pat")
	ctx := capability.WithActor(context.Background(), actor)
	name := "test.mcp" + t.Name() + "Write"
	first, err := a.Invoke(ctx, invocation{Name: name, Args: json.RawMessage(`{"amount":5}`)})
	if err != nil {
		t.Fatal(err)
	}
	// Another transaction commits after the original preview, while this call
	// is waiting to enter its lock. The recheck must happen after the runner.
	a.SetConfirmationRunner(func(locked context.Context, fn func(context.Context) error) error { value.Store(9); return fn(locked) })
	_, err = a.Confirm(ctx, confirmInput{ConfirmToken: first.(*writePreview).ConfirmToken})
	if err == nil || errcode.From(err).Code != errcode.StaleData || writes.Load() != 0 || value.Load() != 9 {
		t.Fatalf("stale write executed:%v writes=%d value=%d", err, writes.Load(), value.Load())
	}
	first, err = a.Invoke(ctx, invocation{Name: name, Args: json.RawMessage(`{"amount":5}`)})
	if err != nil {
		t.Fatal(err)
	}
	a.SetConfirmationRunner(nil)
	_, err = a.Confirm(ctx, confirmInput{ConfirmToken: first.(*writePreview).ConfirmToken})
	if err == nil || errcode.From(err).Code != errcode.InternalError || writes.Load() != 0 {
		t.Fatalf("unguarded write accepted:%v", err)
	}
}
func TestPreviewHashIgnoresEnumerationOrder(t *testing.T) {
	a := &capability.Preview{Affected: 1, Changes: []capability.Change{{Entity: "test", ID: "1", Op: capability.OpUpdate, Fields: []capability.FieldDiff{{Field: "b", Before: 1, After: 2}, {Field: "a", Before: 3, After: 4}}}}}
	b := &capability.Preview{Affected: 1, Changes: []capability.Change{{Entity: "test", ID: "1", Op: capability.OpUpdate, Fields: []capability.FieldDiff{{Field: "a", Before: 3, After: 4}, {Field: "b", Before: 1, After: 2}}}}}
	one, err := hashPreview(a)
	if err != nil {
		t.Fatal(err)
	}
	two, err := hashPreview(b)
	if err != nil {
		t.Fatal(err)
	}
	if one != two {
		t.Fatal("unchanged fieldset requires another confirmation")
	}
}

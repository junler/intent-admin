package aitool

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

func TestQueryCannotWrite(t *testing.T) {
	var calls atomic.Int64
	capability.Register(capability.Capability[struct{}, struct{}]{Meta: capability.Meta{Name: "test.aiWriteBoundary", Summary: "修改", Risk: capability.RiskMedium, AIEnabled: true, Mutating: true}, Fn: func(context.Context, struct{}) (struct{}, error) { calls.Add(1); return struct{}{}, nil }, DryRun: func(context.Context, struct{}) (*capability.Preview, error) { return &capability.Preview{}, nil }})
	a := &capability.Actor{Source: capability.SourceAI, Perms: []string{capability.SuperPerm}, MaxRisk: capability.RiskHigh}
	_, err := (&Tools{}).Query(capability.WithActor(context.Background(), a), "test.aiWriteBoundary", json.RawMessage(`{}`))
	if errcode.From(err).Code != 403 || calls.Load() != 0 {
		t.Fatalf("query crossed write boundary: %v, calls=%d", err, calls.Load())
	}
}
func TestQueryTruncatesAndRedacts(t *testing.T) {
	capability.Register(capability.Capability[struct{}, map[string]any]{Meta: capability.Meta{Name: "test.aiReadBoundary", Summary: "查询", Risk: capability.RiskLow, AIEnabled: true}, Fn: func(context.Context, struct{}) (map[string]any, error) {
		return map[string]any{"password": "never-show", "rows": strings.Repeat("中", 10000)}, nil
	}})
	a := &capability.Actor{Source: capability.SourceAI, Perms: []string{capability.SuperPerm}, MaxRisk: capability.RiskLow}
	out, err := (&Tools{}).Query(capability.WithActor(context.Background(), a), "test.aiReadBoundary", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "never-show") || !strings.Contains(string(raw), "truncated") {
		t.Fatalf("unsafe query result: %.100s", raw)
	}
}
func TestQueryHidesUnpermittedEntry(t *testing.T) {
	capability.Register(capability.Capability[struct{}, struct{}]{Meta: capability.Meta{Name: "test.aiHiddenBoundary", Summary: "受限查询", Perm: "test:required", Risk: capability.RiskLow, AIEnabled: true}, Fn: func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }})
	a := &capability.Actor{Source: capability.SourceAI, MaxRisk: capability.RiskHigh}
	_, err := (&Tools{}).Query(capability.WithActor(context.Background(), a), "test.aiHiddenBoundary", json.RawMessage(`{}`))
	if errcode.From(err).Code != 404 {
		t.Fatalf("hidden entry leaked: %v", err)
	}
}

// Registering a module must be enough for the AI to link to its page. This
// used to require editing a hand-maintained list of every route in the app.
func TestRouteIsDerivedFromTheRegistry(t *testing.T) {
	capability.Register(capability.Capability[struct{}, struct{}]{
		Meta: capability.Meta{Name: "shop.order.list", Summary: "查询订单", Risk: capability.RiskLow, AIEnabled: true,
			HTTP: &capability.HTTPBinding{Method: "GET", Path: "/shop/order/list"}},
		Fn: func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }})
	capability.Register(capability.Capability[struct{}, struct{}]{
		Meta: capability.Meta{Name: "shop.order.update", Summary: "修改订单", Risk: capability.RiskMedium, Mutating: true,
			HTTP: &capability.HTTPBinding{Method: "PUT", Path: "/shop/order/:id"}},
		Fn:     func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil },
		DryRun: func(context.Context, struct{}) (*capability.Preview, error) { return &capability.Preview{}, nil }})

	for _, tc := range []struct{ name, want string }{
		{"shop.order.list", "/shop/order"},
		{"shop.order.update", "/shop/order"},
	} {
		entry, ok := capability.Lookup(tc.name)
		if !ok {
			t.Fatalf("%s not registered", tc.name)
		}
		if got := Route(entry); got != tc.want {
			t.Fatalf("Route(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := Route(nil); got != "" {
		t.Fatalf("Route(nil) = %q", got)
	}

}

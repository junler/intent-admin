package changeset

import (
	"context"
	"encoding/json"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"testing"
	"time"
)

func TestSnapshotComparisonPreservesNumericAndJSONSemantics(t *testing.T) {
	exact := int64(9007199254740993)
	decoded := fields(json.RawMessage(`{"amount":9007199254740993}`))
	if !same(exact, decoded["amount"]) {
		t.Fatal("bigint snapshot lost precision")
	}
	if same(exact, json.Number("9007199254740992")) {
		t.Fatal("different bigint values matched")
	}
	if !same([]byte(`{"models":["model-a"],"limit":1024}`), `{"limit":1024,"models":["model-a"]}`) {
		t.Fatal("database JSON encodings did not match")
	}
	if !same("0.75", json.Number("0.75")) {
		t.Fatal("PostgreSQL numeric string mismatch")
	}
	now := time.Date(2026, 10, 29, 12, 0, 0, 123000000, time.UTC)
	if !same(now, now.In(time.FixedZone("UTC+8", 8*3600)).Format(time.RFC3339Nano)) {
		t.Fatal("equal timestamp instants did not match")
	}
	if same(now, now.Add(time.Second).Format(time.RFC3339Nano)) {
		t.Fatal("different timestamp instants matched")
	}
}

func TestRollbackCannotInvokeClosedOrCriticalOriginalEntry(t *testing.T) {
	called := 0
	for _, spec := range []struct {
		name    string
		risk    capability.Risk
		enabled bool
		code    int
	}{{"testing.configClosed", capability.RiskHigh, false, errcode.NotFound}, {"testing.criticalOriginal", capability.RiskCritical, true, errcode.RiskExceeded}} {
		capability.Register(capability.Capability[capability.Empty, capability.Empty]{Meta: capability.Meta{Name: spec.name, Summary: "回滚入口隔离回归测试", Risk: spec.risk, Mutating: true, Reversible: true, AIEnabled: spec.enabled, MCPEnabled: spec.enabled}, Fn: func(context.Context, capability.Empty) (capability.Empty, error) { return capability.Empty{}, nil }, DryRun: func(context.Context, capability.Empty) (*capability.Preview, error) {
			return &capability.Preview{}, nil
		}, Undo: func(context.Context, json.RawMessage) error { called++; return nil }})
		for _, source := range []capability.Source{capability.SourceAI, capability.SourceMCP} {
			ctx := capability.WithActor(context.Background(), &capability.Actor{Source: source, Perms: []string{capability.SuperPerm}, MaxRisk: capability.RiskCritical})
			s := New(nil)
			if err := s.check(ctx, Item{Capability: spec.name}, false); err == nil || errcode.From(err).Code != spec.code {
				t.Fatalf("normal rollback source%s entry%s: %v", source, spec.name, err)
			}
			payload := raw(rollbackUndo{Actions: []rollbackAction{{Capability: spec.name, Guard: spec.name, Payload: json.RawMessage(`{}`)}}})
			if err := s.Undo(ctx, payload); err == nil || errcode.From(err).Code != spec.code {
				t.Fatalf("inverse rollback source%s entry%s: %v", source, spec.name, err)
			}
		}
	}
	if called != 0 {
		t.Fatalf("closed/critical original compensation invoked%d times", called)
	}
}

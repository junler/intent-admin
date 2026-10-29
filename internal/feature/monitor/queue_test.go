package monitor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/task"
)

func TestQueueCapabilitiesEnforcePermissionsAndRisk(t *testing.T) {
	s := &Service{runtime: &task.Runtime{}}
	s.registerQueue()
	audits := []capability.AuditRecord{}
	capability.Install(capability.Hooks{WriteAudit: func(_ context.Context, record capability.AuditRecord) { audits = append(audits, record) }})
	t.Cleanup(func() { capability.Install(capability.Hooks{}) })
	actor := &capability.Actor{Source: capability.SourceHTTP, Type: capability.ActorHuman, MaxRisk: capability.RiskHigh, Perms: []string{"monitor:queue:list"}}
	ctx := capability.WithActor(context.Background(), actor)
	for _, action := range []string{"retry", "archive", "cancel", "delete", "pause", "resume"} {
		name := "monitor.queue." + action
		entry, ok := capability.Lookup(name)
		if !ok || !entry.Mutating || !entry.HasDryRun() || entry.Reversible || entry.AIEnabled || entry.MCPEnabled {
			t.Fatalf("unsafe metadata for %s", name)
		}
		args := json.RawMessage(`{"queue":"default","taskId":"id","expectedState":"pending"}`)
		if action == "pause" || action == "resume" {
			args = json.RawMessage(`{"queue":"default"}`)
		}
		if _, err := capability.InvokeJSON(ctx, name, args); errcode.From(err).Code != errcode.Forbidden {
			t.Fatalf("list permission executed %s: %v", name, err)
		}
		if _, err := capability.RunPreview(ctx, name, args); errcode.From(err).Code != errcode.Forbidden {
			t.Fatalf("unauthorized preview %s: %v", name, err)
		}
	}
	if len(audits) != 6 {
		t.Fatalf("rejected mutations not audited: %d", len(audits))
	}
	actor.Perms = []string{"monitor:queue:retry"}
	actor.MaxRisk = capability.RiskMedium
	if _, err := capability.InvokeJSON(ctx, "monitor.queue.retry", json.RawMessage(`{"queue":"default","taskId":"id","expectedState":"archived"}`)); errcode.From(err).Code != errcode.RiskExceeded {
		t.Fatalf("low risk ceiling executed retry: %v", err)
	}
	for _, src := range []capability.Source{capability.SourceAI, capability.SourceMCP} {
		actor.Source = src
		actor.Perms = []string{capability.SuperPerm}
		actor.MaxRisk = capability.RiskHigh
		if _, err := capability.InvokeJSON(ctx, "monitor.queue.task", json.RawMessage(`{"queue":"default","taskId":"id"}`)); errcode.From(err).Code != errcode.NotFound {
			t.Fatalf("internal queue exposed to %s: %v", src, err)
		}
	}
}

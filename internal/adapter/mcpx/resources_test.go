package mcpx

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/knowledge"
)

type resourceKnowledge struct{}

func (resourceKnowledge) Get(context.Context, string) (knowledge.Knowledge, error) {
	return knowledge.Knowledge{
		Entities: []knowledge.EntityDef{{Table: "sys_user", Summary: "用户", Relations: []knowledge.Relation{{Target: "sys_role"}}}, {Table: "sys_role", Summary: "私有角色"}},
		Enums:    []knowledge.EnumDef{{DictType: "shared_status", Summary: "状态", UsedBy: []string{"sys_user.status", "sys_role.status"}, Values: map[string]string{"0": "正常"}}, {DictType: "private_role", Summary: "私有角色字典", UsedBy: []string{"sys_role.status"}}},
		Rules:    []knowledge.RuleDef{{Scope: "*", Rule: "公共规则"}, {Scope: "sys_user", Rule: "用户规则"}, {Scope: "sys_role", Rule: "私有角色规则"}},
	}, nil
}
func TestResourcesArePermissionTrimmedOwnedAndAudited(t *testing.T) {
	a, _, _, revoked := setup(t, "meta", capability.RiskHigh)
	a.knowledge = resourceKnowledge{}
	capability.Register(capability.Capability[struct{}, struct{}]{Meta: capability.Meta{Name: "system.user.resourceFixtureRead", Summary: "用户元数据", Perm: "test:read", Risk: capability.RiskLow, MCPEnabled: true}, Fn: func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }})
	capability.Register(capability.Capability[struct{}, struct{}]{Meta: capability.Meta{Name: "system.role.resourceFixtureRead", Summary: "私有角色元数据", Perm: "private:role", Risk: capability.RiskLow, MCPEnabled: true}, Fn: func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }})
	var audits atomic.Int64
	capability.Install(capability.Hooks{WriteAudit: func(_ context.Context, r capability.AuditRecord) {
		if strings.HasPrefix(r.Capability, "mcp.resource.") {
			audits.Add(1)
			if r.Source != capability.SourceMCP || r.TokenID != 42 {
				t.Errorf("resource audit lost subject: %+v", r)
			}
		}
	}})
	t.Cleanup(func() { capability.Install(capability.Hooks{}) })
	base := a.authenticate
	var narrowed atomic.Bool
	a.authenticate = func(ctx context.Context, plain string) (*capability.Actor, error) {
		actor, err := base(ctx, plain)
		if err == nil && narrowed.Load() {
			actor.Perms = []string{}
		}
		return actor, err
	}
	cs := connect(t, a)
	ctx := context.Background()
	list, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range list.Resources {
		seen[r.URI] = true
		if strings.Contains(r.URI, "private_role") || strings.Contains(r.URI, "sys_role") || strings.Contains(r.URI, "system.role.") {
			t.Fatalf("resource catalogue leaked %s", r.URI)
		}
	}
	if !seen["entity://sys_user"] || !seen["dict://shared_status"] || !seen["context://me"] {
		t.Fatalf("missing authorized resources %+v", seen)
	}
	user, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "entity://sys_user"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(user.Contents[0].Text, "sys_role") {
		t.Fatal("hidden relation leaked")
	}
	for _, uri := range []string{"entity://sys_role", "dict://private_role", "context://users/2", "capability://system.role.resourceFixtureRead"} {
		if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri}); err == nil {
			t.Fatalf("read forbidden URI %s", uri)
		}
	}
	narrowed.Store(true)
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("meta protocol tools should remain available")
	}
	if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "entity://sys_user"}); err == nil {
		t.Fatal("cached session retained revoked permission")
	}
	revoked.Store(true)
	if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "context://me"}); err == nil {
		t.Fatal("revoked token read own resource")
	}
	if audits.Load() < 6 {
		t.Fatalf("resource attempts not audited: %d", audits.Load())
	}
}

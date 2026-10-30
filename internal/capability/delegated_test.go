package capability

import (
	"context"
	"testing"
)

func TestReadOnlyDelegationRejectsWritesSharingReadPermission(t *testing.T) {
	called := false
	name := "test.delegatedReadOnlyWrite"
	Register(Capability[struct{}, struct{}]{Meta: Meta{Name: name, Summary: "共享权限写入", Perm: "test:shared", Risk: RiskLow, Mutating: true, MCPEnabled: true}, Fn: func(context.Context, struct{}) (struct{}, error) { called = true; return struct{}{}, nil }, DryRun: func(context.Context, struct{}) (*Preview, error) { return &Preview{}, nil }})
	ctx := WithActor(context.Background(), &Actor{Source: SourceMCP, Type: ActorMCPClient, Perms: []string{"test:shared"}, MaxRisk: RiskHigh, ReadOnly: true})
	if _, err := InvokeJSON(ctx, name, []byte(`{}`)); err == nil || called {
		t.Fatalf("read-only delegated call executed: called=%v err=%v", called, err)
	}
	if _, err := RunPreview(ctx, name, []byte(`{}`)); err == nil {
		t.Fatal("read-only delegate obtained write preview")
	}
}

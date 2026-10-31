package plan

import (
	"context"
	"testing"

	"github.com/junler/intent-admin/internal/capability"
)

func TestPrincipalRechecksCurrentUserWithoutReviewerGrant(t *testing.T) {
	service := New(nil, Options{MaxRisk: capability.RiskHigh})
	service.ActorLoader = func(context.Context, int64) (*capability.Actor, error) {
		return &capability.Actor{UserID: 7, Perms: []string{"ai:agent:use", "demo:item:edit"}, MaxRisk: capability.RiskHigh}, nil
	}
	ctx := capability.WithActor(context.Background(), &capability.Actor{UserID: 7, Perms: []string{capability.SuperPerm}, MaxRisk: capability.RiskCritical})
	_, actor, err := service.principal(ctx)
	if err != nil || actor.MaxRisk != capability.RiskHigh || !actor.Can("demo:item:edit") || actor.Can("ai:plan:approve") {
		t.Fatalf("owner confirmation needs only current operation grants: actor=%+v err=%v", actor, err)
	}
	service.ActorLoader = func(context.Context, int64) (*capability.Actor, error) {
		return &capability.Actor{UserID: 7, MaxRisk: capability.RiskHigh}, nil
	}
	if _, _, err := service.principal(ctx); err == nil {
		t.Fatal("revoked AI permission accepted from stale session")
	}
}

func TestLegacyApprovalMetadataDoesNotBlockOwnerConfirmation(t *testing.T) {
	p, err := fromRow(row{ID: "legacy", UserID: 7, Steps: []byte(`[]`), PageContext: []byte(`{"approvalId":"retired","onError":"stop"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(toRow(p).PageContext); got != `{"context":null,"onError":"stop"}` {
		t.Fatalf("retired review binding survived plan save: %s", got)
	}
}

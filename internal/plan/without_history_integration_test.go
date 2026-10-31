package plan

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/capability/dryrun"
	"github.com/junler/intent-admin/internal/changeset"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/service"
)

func TestOwnerConfirmationWithoutHistory(t *testing.T) {
	db := integrationDB(t)
	sets := changeset.NewWithoutHistory(db)
	const remove = "historyFixture.item.remove"
	svc := service.NewDemoService(db)
	capability.Register(capability.Capability[service.DemoRemoveIn, capability.Empty]{
		Meta: capability.Meta{Name: remove, Summary: "删除示例", Perm: "demo:item:remove", Risk: capability.RiskHigh, Mutating: true, AIEnabled: true},
		Fn:   svc.Remove,
		DryRun: func(ctx context.Context, in service.DemoRemoveIn) (*capability.Preview, error) {
			ids := make([]any, len(in.ItemIDs))
			for i, id := range in.ItemIDs {
				ids[i] = id
			}
			return dryrun.ForDelete(ctx, db, &model.DemoItem{}, ids, dryrun.LabelOpt{Entity: "demo_item", IDField: "item_id", LabelField: "name"})
		},
	})
	capability.Install(capability.Hooks{Tx: dbx.NewTxRunner(db), ReadOnly: dbx.NewReadOnlyRunner(), MaxAIRisk: capability.RiskHigh})
	plans := New(db, Options{Changesets: sets, MaxRisk: capability.RiskHigh})
	actor := &capability.Actor{UserID: 1, UserName: "admin", Source: capability.SourceAI, Type: capability.ActorAgent, Perms: []string{"ai:agent:use", "demo:item:remove"}, MaxRisk: capability.RiskHigh}
	plans.ActorLoader = func(context.Context, int64) (*capability.Actor, error) { return actor, nil }
	ctx := capability.WithActor(context.Background(), actor)
	p, err := plans.Create(ctx, CreateIn{Intent: "删除示例", Steps: []Step{{Capability: remove, Args: json.RawMessage(`{"itemIds":[1]}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if p.MaxRisk != capability.RiskHigh || p.Status != "previewed" {
		t.Fatalf("high-risk action did not await owner confirmation: %+v", p)
	}
	var flag string
	if err := db.Table("demo_item").Where("item_id=1").Select("del_flag").Scan(&flag).Error; err != nil || flag != "0" {
		t.Fatalf("preview changed business data: flag=%s err=%v", flag, err)
	}
	if err := db.Exec(`UPDATE sys_ai_plan SET page_context=page_context || '{"approvalId":"retired"}'::jsonb WHERE plan_id=?`, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	p, err = plans.Apply(ctx, p.ID, ApplyIn{})
	if err != nil || p.Status != "done" || p.ChangesetID != "" {
		t.Fatalf("owner confirmation failed without a reviewer/history: plan=%+v err=%v", p, err)
	}
	human := *actor
	human.Source, human.Type, human.MaxRisk = capability.SourceHTTP, capability.ActorHuman, capability.RiskCritical
	if _, err := capability.InvokeJSON(capability.WithActor(context.Background(), &human), remove, json.RawMessage(`{"itemIds":[2]}`)); err != nil {
		t.Fatalf("direct human operation required additional confirmation: %v", err)
	}
	for _, table := range []string{"sys_changeset", "sys_changeset_item", "sys_ai_plan_approval"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("retired history was recreated in %s: count=%d err=%v", table, count, err)
		}
	}
}

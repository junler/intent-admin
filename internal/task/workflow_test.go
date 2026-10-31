package task

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/junler/intent-admin/internal/automation"
	"github.com/junler/intent-admin/internal/capability"
)

func TestWorkflowIdentityBindingAndLiveAuthorization(t *testing.T) {
	r := testRuntime(t)
	capability.Install(capability.Hooks{Tx: func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }})
	if _, ok := capability.Lookup("automation.flow.run"); !ok {
		automation.Register(nil, "Asia/Shanghai")
	}
	calls := 0
	capability.Register(capability.Capability[capability.Empty, capability.Empty]{Meta: capability.Meta{Name: "fixture.workflowWrite", Summary: "业务操作", Perm: "fixture:write", Risk: capability.RiskMedium, Mutating: true, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, _ capability.Empty) (capability.Empty, error) {
		a, _ := capability.ActorFrom(ctx)
		if a.UserID != 7 || a.IsSuper() || a.Source != capability.SourceAI {
			t.Errorf("job escalated its owner: %+v", a)
		}
		calls++
		return capability.Empty{}, nil
	}, DryRun: func(context.Context, capability.Empty) (*capability.Preview, error) {
		return &capability.Preview{Affected: 1}, nil
	}})
	a := &capability.Actor{UserID: 7, Source: capability.SourceAI, MaxRisk: capability.RiskHigh, Perms: []string{"fixture:write"}}
	ctx := capability.WithActor(context.Background(), a)
	job := func(params string) Job {
		return Job{JobName: "业务流程", InvokeTarget: WorkflowHandler, JobParams: params, CronExpression: "0 10 * * *", Status: "0"}
	}
	params := `{"flow":{"steps":[{"name":"action","capability":"fixture.workflowWrite","args":{}}]}}`
	j := job(params)
	if err := r.PrepareJob(ctx, &j, nil); err != nil {
		t.Fatal(err)
	}
	var p WorkflowParams
	_ = json.Unmarshal([]byte(j.JobParams), &p)
	if p.OwnerID != 7 || p.Source != capability.SourceAI || p.MaxRisk != capability.RiskHigh {
		t.Fatalf("unbound workflow: %+v", p)
	}
	forged := job(`{"ownerId":99,"flow":{"steps":[{"name":"action","capability":"fixture.workflowWrite"}]}}`)
	if err := r.PrepareJob(ctx, &forged, nil); err == nil {
		t.Fatal("accepted forged owner")
	}
	editor := capability.WithActor(context.Background(), &capability.Actor{UserID: 99, Source: capability.SourceHTTP, MaxRisk: capability.RiskHigh, Perms: []string{capability.SuperPerm}})
	updated := job(params)
	if err := r.PrepareJob(editor, &updated, &j); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(updated.JobParams), &p)
	if p.OwnerID != 7 || len(p.Perms) != 1 {
		t.Fatal("editor replaced execution identity")
	}
	r.ActorLoader = func(context.Context, int64) (*capability.Actor, error) {
		copy := *a
		copy.Perms = []string{"fixture:write", "new:grant"}
		return &copy, nil
	}
	systemCtx := capability.WithActor(context.Background(), capability.SystemActor("定时流程"))
	handler := r.handlers[WorkflowHandler]
	if err := handler.Fn(systemCtx, json.RawMessage(j.JobParams)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("flow did not execute")
	}
	r.ActorLoader = func(context.Context, int64) (*capability.Actor, error) {
		copy := *a
		copy.Perms = nil
		return &copy, nil
	}
	if err := handler.Fn(systemCtx, json.RawMessage(j.JobParams)); err == nil {
		t.Fatal("revoked grant executed")
	}
	if calls != 1 {
		t.Fatal("revoked call changed data")
	}
	r.ActorLoader = func(context.Context, int64) (*capability.Actor, error) { return nil, errors.New("disabled account") }
	if err := handler.Fn(systemCtx, json.RawMessage(j.JobParams)); err == nil {
		t.Fatal("disabled account executed")
	}
	// A workflow must not wrap credential operations or another job manager.
	bad := job(`{"flow":{"steps":[{"name":"nested","capability":"monitor.job.create"}]}}`)
	if err := r.PrepareJob(ctx, &bad, nil); err == nil {
		t.Fatal("nested scheduler accepted")
	}
}

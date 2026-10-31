package aitool

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/junler/intent-admin/internal/plan"
)

type capturingPlanEngine struct {
	input plan.CreateIn
	calls int
}

func (e *capturingPlanEngine) Create(_ context.Context, in plan.CreateIn) (*plan.Plan, error) {
	e.input = in
	e.calls++
	return &plan.Plan{ID: "proposed-plan", Status: "previewed", Steps: in.Steps}, nil
}

func TestProposalErrorPolicyThroughTool(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", "stop"}, {"stop", "stop"}, {" STOP ", "stop"},
		{"continue", "continue"}, {" Continue ", "continue"},
		{"abort", "stop"}, {"ignore", "stop"}, {"continue_on_error", "stop"}, {"停止", "stop"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			engine := &capturingPlanEngine{}
			kit := &Tools{Plans: engine, Intent: "创建每分钟发布公共公告的任务"}
			tools, err := kit.Build()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, base := range tools {
				info, err := base.Info(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if info.Name != "propose_plan" {
					continue
				}
				found = true
				schema, err := info.ParamsOneOf.ToJSONSchema()
				if err != nil {
					t.Fatal(err)
				}
				property, ok := schema.Properties.Get("onError")
				if !ok || !reflect.DeepEqual(property.Enum, []any{"stop", "continue"}) || property.Default != "stop" {
					t.Fatalf("tool lacks the error policy contract: %+v", property)
				}
				args := map[string]any{"steps": []map[string]any{{"capability": "monitor.job.create", "args": map[string]any{"jobGroup": "DEFAULT"}}}}
				if tc.input != "" {
					args["onError"] = tc.input
				}
				raw, _ := json.Marshal(args)
				if _, err := base.(tool.InvokableTool).InvokableRun(context.Background(), string(raw)); err != nil {
					t.Fatal(err)
				}
			}
			if !found || engine.calls != 1 || engine.input.OnError != tc.want || engine.input.Intent != kit.Intent || !kit.Proposed.Load() {
				t.Fatalf("proposal not forwarded correctly: calls=%d, input=%+v", engine.calls, engine.input)
			}
		})
	}
}

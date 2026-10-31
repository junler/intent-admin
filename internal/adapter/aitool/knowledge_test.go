package aitool

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/knowledge"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type topicKnowledge struct{ err error }

func (k topicKnowledge) Get(context.Context, string) (knowledge.Knowledge, error) {
	return knowledge.Knowledge{}, k.err
}

func TestMissingKnowledgeDoesNotBlockClosedTokenFormDraft(t *testing.T) {
	capability.Register(capability.Capability[capability.Empty, capability.Empty]{Meta: capability.Meta{Name: "fixture.privateToken.list", Summary: "令牌表单页面", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/test/form/list"}}, Fn: func(context.Context, capability.Empty) (capability.Empty, error) { return capability.Empty{}, nil }})
	type tokenForm struct {
		Name string `json:"name"`
	}
	writes := 0
	capability.Register(capability.Capability[tokenForm, capability.Empty]{Meta: capability.Meta{Name: "fixture.privateToken.create", Summary: "个人访问令牌", Risk: capability.RiskMedium, Mutating: true, HTTP: &capability.HTTPBinding{Method: "POST", Path: "/test/form"}}, Fn: func(context.Context, tokenForm) (capability.Empty, error) { writes++; return capability.Empty{}, nil }, DryRun: func(context.Context, tokenForm) (*capability.Preview, error) { return &capability.Preview{}, nil }})
	ctx := capability.WithActor(context.Background(), &capability.Actor{UserID: 1, Source: capability.SourceAI, MaxRisk: capability.RiskLow})
	filled := false
	kit := &Tools{Knowledge: topicKnowledge{err: errcode.NotFoundf("知识主题不存在")}, Navigation: formPages{}, PageContext: map[string]any{
		"focusedForm": map[string]any{"formId": "ui.tokenEditor", "mode": "create", "route": "/test/form"},
		"forms":       []uiForm{{Name: "ui.tokenEditor", Summary: "个人访问令牌", Route: "/test/form", Mode: "create", Fields: []string{"name", "maxRisk"}, Defaults: map[string]any{"name": "个人访问令牌", "maxRisk": "low"}}},
	}, Emit: func(kind string, data any) {
		if kind == "form_fill" {
			payload := data.(map[string]any)
			filled = payload["formId"] == "ui.tokenEditor" && payload["defaults"].(map[string]any)["name"] == "个人访问令牌" && len(payload["values"].(map[string]any)) == 0
		}
	}}
	tools, err := kit.Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []struct{ name, args string }{
		{"get_knowledge", `{"topic":"个人访问令牌"}`},
		{"list_forms", `{"keyword":"个人访问令牌"}`},
		{"describe_form", `{"name":"fixture.privateToken.create"}`},
		{"fill_form", `{"name":"fixture.privateToken.create","values":{}}`},
	} {
		found := false
		for _, base := range tools {
			info, err := base.Info(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if info.Name != call.name {
				continue
			}
			found = true
			if _, err := base.(tool.InvokableTool).InvokableRun(ctx, call.args); err != nil {
				t.Fatalf("%s interrupted form preparation: %v", call.name, err)
			}
		}
		if !found {
			t.Fatalf("tool %s missing", call.name)
		}
	}
	if !filled || writes != 0 || kit.Proposed.Load() {
		t.Fatal("default values were not delivered independently of token creation")
	}
}

func TestUnknownKnowledgeTopicAllowsFormDiscoveryToContinue(t *testing.T) {
	kit := &Tools{Knowledge: topicKnowledge{err: errcode.NotFoundf("知识主题不存在")}}
	out, err := kit.getKnowledge(context.Background(), "个人访问令牌")
	if err != nil {
		t.Fatal("unknown topic must not terminate the agent", err)
	}
	result, ok := out.(map[string]any)
	if !ok || result["found"] != false || result["hint"] == "" || kit.FilledForm.Load() || kit.Proposed.Load() {
		t.Fatalf("missing recoverable discovery hint: %+v", out)
	}
	for _, code := range []int{errcode.Unauthorized, errcode.Forbidden, errcode.InternalError} {
		kit.Knowledge = topicKnowledge{err: errcode.New(code, "查询失败")}
		if _, err := kit.getKnowledge(context.Background(), "rules"); err == nil || errcode.From(err).Code != code {
			t.Fatalf("knowledge discovery swallowed error %d: %v", code, err)
		}
	}
}

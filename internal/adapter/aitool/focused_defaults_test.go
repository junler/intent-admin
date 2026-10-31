package aitool

import (
	"context"
	"testing"

	"github.com/junler/intent-admin/internal/capability"
)

func TestFocusedDefaultFillDoesNotAskAgainOrDiscardExplicitValues(t *testing.T) {
	ctx := capability.WithActor(context.Background(), &capability.Actor{UserID: 1, Source: capability.SourceAI})
	for _, request := range []string{"请帮我新增“个人访问令牌”表单", "请帮我新增“个人访问令牌”表单，你填一些默认数据", "帮我增加一个"} {
		events := 0
		kit := &Tools{Navigation: formPages{}, PageContext: map[string]any{
			"focusedForm": map[string]any{"formId": "ui.token", "title": "个人访问令牌", "route": "/test/form", "mode": "create"},
			"forms":       []uiForm{{Name: "ui.token", Route: "/test/form", Mode: "create", Fields: []string{"name", "maxRisk", "scopes", "expiresAt"}, Required: []string{"name"}, Defaults: map[string]any{"name": "个人访问令牌", "maxRisk": "low"}}},
		}, Emit: func(kind string, data any) {
			if kind == "form_fill" {
				events++
			}
		}}
		handled, err := kit.FillFocusedDefaults(ctx, request)
		if err != nil || !handled || events != 1 || !kit.FilledForm.Load() || kit.Proposed.Load() {
			t.Fatalf("request %q was not filled directly: %v", request, err)
		}
	}
	for _, request := range []string{"请帮我新增“其他”表单", "请帮我新增“个人访问令牌”表单，名称为报表", "请帮我新增“个人访问令牌”表单，风险设为高", "为什么要新增“个人访问令牌”表单", "先别填写个人访问令牌"} {
		if defaultFillRequest(request, "个人访问令牌", "create", "") {
			t.Errorf("default fill swallowed request %q", request)
		}
	}
}

func TestUIFormSchemaDistinguishesOptionalFields(t *testing.T) {
	ctx := capability.WithActor(context.Background(), &capability.Actor{UserID: 1, Source: capability.SourceAI})
	kit := &Tools{Navigation: formPages{}, PageContext: map[string]any{"forms": []uiForm{{Name: "ui.token", Route: "/test/form", Fields: []string{"name", "scopes", "expiresAt"}, Required: []string{"name"}, Descriptions: map[string]string{"scopes": "留空表示不额外收窄当前用户权限", "expiresAt": "留空表示不过期"}}}}}
	out, err := kit.describeUIForm(ctx, "ui.token")
	if err != nil {
		t.Fatal(err)
	}
	schema := out.(map[string]any)["schema"].(map[string]any)
	if required := schema["required"].([]string); len(required) != 1 || required[0] != "name" {
		t.Fatalf("optional fields incorrectly required: %+v", required)
	}
	if schema["properties"].(map[string]any)["expiresAt"].(map[string]any)["description"] != "留空表示不过期" {
		t.Fatal("missing optional blank semantics")
	}
}

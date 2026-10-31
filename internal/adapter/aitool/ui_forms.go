package aitool

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type uiForm struct {
	Name       string   `json:"name"`
	Summary    string   `json:"summary"`
	Route      string   `json:"route"`
	Permission string   `json:"permission"`
	Fields     []string `json:"fields"`
}

// Browser manifests describe local controls only. They never authorize a
// business operation; submitting still uses the independently authorized API.
func (t *Tools) uiForms(ctx context.Context) []uiForm {
	a, err := actor(ctx)
	if err != nil {
		return nil
	}
	raw, _ := json.Marshal(t.PageContext["forms"])
	var forms []uiForm
	if json.Unmarshal(raw, &forms) != nil || len(forms) > 40 {
		return nil
	}
	allowed := []uiForm{}
	for _, form := range forms {
		if !strings.HasPrefix(form.Name, "ui.") || len(form.Name) > 200 || len(form.Fields) > 80 || !a.Can(form.Permission) {
			continue
		}
		if _, err := t.findPage(ctx, form.Route); err != nil {
			continue
		}
		allowed = append(allowed, form)
	}
	return allowed
}

func (t *Tools) describeUIForm(ctx context.Context, name string) (any, error) {
	for _, form := range t.uiForms(ctx) {
		if form.Name != name {
			continue
		}
		properties := map[string]any{}
		for _, field := range form.Fields {
			properties[field] = map[string]any{}
		}
		return map[string]any{"form": form, "schema": map[string]any{"type": "object", "properties": properties, "additionalProperties": false}}, nil
	}
	return nil, errcode.NotFoundf("表单未打开或当前账号无权填写")
}

func (t *Tools) fillUIForm(ctx context.Context, in FormInput) (any, error) {
	for _, form := range t.uiForms(ctx) {
		if form.Name != in.Name {
			continue
		}
		raw, err := json.Marshal(in.Values)
		if err != nil || len(raw) > 16<<10 {
			return nil, errcode.BadRequestf("表单内容过大或格式无效")
		}
		fields := map[string]bool{}
		for _, field := range form.Fields {
			fields[field] = true
		}
		for key := range in.Values {
			if !fields[key] || !formFieldName.MatchString(key) {
				return nil, errcode.BadRequestf("表单字段无效，请先查询表单结构")
			}
		}
		if t.Emit == nil {
			return nil, errcode.Internalf("表单填写不可用")
		}
		t.FilledForm.Store(true)
		t.Emit("form_fill", map[string]any{"route": form.Route, "formId": form.Name, "permission": form.Permission, "mode": "action", "values": in.Values})
		return map[string]any{"route": form.Route, "message": "表单已填写，等待用户检查和确认"}, nil
	}
	return nil, errcode.NotFoundf("表单未打开或当前账号无权填写")
}

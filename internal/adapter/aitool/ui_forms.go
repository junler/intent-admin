package aitool

import (
	"context"
	"encoding/json"
	"github.com/junler/intent-admin/internal/capability"
	"strings"

	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type uiForm struct {
	Name         string            `json:"name"`
	Summary      string            `json:"summary"`
	Route        string            `json:"route"`
	Permission   string            `json:"permission"`
	Fields       []string          `json:"fields"`
	Defaults     map[string]any    `json:"defaults,omitempty"`
	Mode         string            `json:"mode,omitempty"`
	Required     []string          `json:"required,omitempty"`
	Descriptions map[string]string `json:"descriptions,omitempty"`
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
			property := map[string]any{}
			if description := form.Descriptions[field]; description != "" {
				property["description"] = description
			}
			properties[field] = property
		}
		for field, value := range formDefaults(form) {
			properties[field].(map[string]any)["default"] = value
		}
		required := []string{}
		for _, field := range form.Required {
			if _, ok := properties[field]; ok {
				required = append(required, field)
			}
		}
		return map[string]any{"form": form, "schema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}, "fillPolicy": "用户已请求填充时直接调用 fill_form，不需要再次确认默认值。required 之外的字段可选，不要把可选空值称为必须补充；已有默认值先填充，只有保存提交才需用户确认。"}, nil
	}
	return nil, errcode.NotFoundf("表单未打开或当前账号无权填写")
}

func (t *Tools) fillUIForm(ctx context.Context, in FormInput) (any, error) {
	for _, form := range t.uiForms(ctx) {
		if form.Name != in.Name {
			continue
		}
		raw, err := json.Marshal(map[string]any{"values": in.Values, "defaults": formDefaults(form)})
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
		defaults := formDefaults(form)
		if len(defaults) == 0 && len(in.Values) == 0 {
			return nil, errcode.BadRequestf("当前表单没有可用的默认值，请补充需要填写的内容")
		}
		if t.Emit == nil {
			return nil, errcode.Internalf("表单填写不可用")
		}
		t.FilledForm.Store(true)
		mode := uiFormMode(form)
		t.Emit("form_fill", map[string]any{"route": form.Route, "formId": form.Name, "permission": form.Permission, "mode": mode, "values": in.Values, "defaults": defaults})
		return map[string]any{"route": form.Route, "message": "表单已填写，等待用户检查和确认"}, nil
	}
	return nil, errcode.NotFoundf("表单未打开或当前账号无权填写")
}

// Defaults are declared suggestions, never current control values or secrets.
func formDefaults(form uiForm) map[string]any {
	out := map[string]any{}
	for _, field := range form.Fields {
		if value, ok := form.Defaults[field]; ok && formFieldName.MatchString(field) && !capability.IsSensitive(field) && field != "configValue" && field != "config_value" {
			out[field] = capability.SanitizeValue(value)
		}
	}
	return out
}

func (t *Tools) focusedUIForm(ctx context.Context, desc FormDescriptor, recordID string) string {
	raw, _ := json.Marshal(t.PageContext["focusedForm"])
	var focus struct {
		FormID   string `json:"formId"`
		Mode     string `json:"mode"`
		Route    string `json:"route"`
		RecordID string `json:"recordId"`
	}
	if json.Unmarshal(raw, &focus) != nil || focus.Route != desc.Route || focus.Mode != desc.Mode {
		return ""
	}
	if desc.Mode != "create" && recordID != "" && focus.RecordID != recordID {
		return ""
	}
	for _, form := range t.uiForms(ctx) {
		if form.Name == focus.FormID && form.Route == desc.Route && form.Mode == desc.Mode {
			return form.Name
		}
	}
	return ""
}

func uiFormMode(form uiForm) string {
	switch form.Mode {
	case "create", "update", "query":
		return form.Mode
	default:
		return "action"
	}
}

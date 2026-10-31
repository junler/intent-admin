package aitool

import (
	"context"
	"encoding/json"
	"strings"
)

// FillFocusedDefaults handles the composer's narrow, explicit default-fill
// requests without asking a model to authorize an already requested UI draft.
// Any additional values, different target or ambiguous intent stays with the
// model. This never submits the form or invokes a business capability.
func (t *Tools) FillFocusedDefaults(ctx context.Context, message string) (bool, error) {
	raw, _ := json.Marshal(t.PageContext["focusedForm"])
	var focus struct {
		FormID   string `json:"formId"`
		Title    string `json:"title"`
		Route    string `json:"route"`
		Mode     string `json:"mode"`
		RecordID string `json:"recordId"`
		Field    string `json:"field"`
	}
	if json.Unmarshal(raw, &focus) != nil || focus.Title == "" || focus.Field != "" {
		return false, nil
	}
	for _, form := range t.uiForms(ctx) {
		if form.Name != focus.FormID || form.Route != focus.Route || uiFormMode(form) != focus.Mode || len(formDefaults(form)) == 0 {
			continue
		}
		if !defaultFillRequest(message, focus.Title, focus.Mode, focus.RecordID) {
			return false, nil
		}
		if err := t.before(ctx, "fill_form"); err != nil {
			return true, err
		}
		out, err := t.FillForm(ctx, FormInput{Name: form.Name, Values: map[string]any{}})
		t.after("fill_form", out, err)
		return true, err
	}
	return false, nil
}

func defaultFillRequest(message, title, mode, recordID string) bool {
	normalize := func(s string) string {
		s = strings.NewReplacer("“", "", "”", "", `"`, "", "（", "(", "）", ")", "：", ":", "，", ",").Replace(s)
		return strings.ToLower(strings.Trim(strings.Join(strings.Fields(s), ""), "。.!！?？"))
	}
	verb := map[string]string{"create": "新增", "update": "修改", "query": "查询", "action": "填写"}[mode]
	if verb == "" {
		return false
	}
	bases := []string{"请帮我" + verb + "“" + title + "”表单", "请帮我填写“" + title + "”表单"}
	if mode == "update" && recordID != "" {
		bases = append(bases, "请帮我修改“"+title+"”表单（记录 ID："+recordID+"）")
	}
	if mode == "create" {
		bases = append(bases, "帮我增加一个", "帮我新增一个", "Please help me prepare a new "+title+" form")
	}
	if mode == "update" {
		bases = append(bases, "Please help me edit the "+title+" form", "Please help me edit the "+title+" form (record ID: "+recordID+")")
	}
	if mode == "query" {
		bases = append(bases, "Please help me fill in the "+title+" search form")
	}
	bases = append(bases, "帮我填一些默认数据", "请帮我填充默认值", "用默认值填充当前表单")
	request := normalize(message)
	for _, base := range bases {
		if request == normalize(base) {
			return true
		}
		for _, suffix := range []string{"你填一些默认数据", "填一些默认数据", "请填一些默认数据", "按默认值填充", "使用默认值", "默认填写", "未指定的字段使用默认值"} {
			if request == normalize(base+","+suffix) {
				return true
			}
		}
	}
	return false
}

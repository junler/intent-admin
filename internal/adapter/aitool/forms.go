package aitool

import (
	"context"
	"encoding/json"
	"github.com/junler/intent-admin/internal/capability"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/junler/intent-admin/internal/pkg/errcode"
)

// UserForm contains only editable identity fields, never grants or an execution command.
type UserForm struct {
	UserName    string `json:"userName,omitempty" jsonschema:"description=用户明确提供的登录账号，未知时留空让用户在表单补充"`
	NickName    string `json:"nickName,omitempty" jsonschema:"description=昵称"`
	Password    string `json:"password,omitempty" jsonschema:"description=用户明确提供的初始密码，仅临时填入表单，不写入操作计划"`
	Email       string `json:"email,omitempty"`
	Phonenumber string `json:"phonenumber,omitempty"`
	Sex         string `json:"sex,omitempty"`
	Remark      string `json:"remark,omitempty"`
}

func (t *Tools) FillUserForm(ctx context.Context, values UserForm) (any, error) {
	t.FormPassword = values.Password
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !a.Can("system:user:add") {
		return nil, errcode.Forbiddenf("没有创建用户的权限")
	}
	page, err := t.findPage(ctx, "/system/user")
	if err != nil {
		return nil, err
	}
	if len(values.UserName) > 30 || len(values.NickName) > 120 || len(values.Email) > 200 || len(values.Phonenumber) > 32 || len(values.Remark) > 500 {
		return nil, errcode.BadRequestf("表单字段超过长度限制")
	}
	if values.Password != "" && (len(values.Password) < 6 || len(values.Password) > 72) {
		return nil, errcode.BadRequestf("密码为 6–72 字节")
	}
	if values.Sex != "" && (values.Sex != "0" && values.Sex != "1" && values.Sex != "2") {
		return nil, errcode.BadRequestf("性别值无效")
	}
	if t.Emit == nil {
		return nil, errcode.Internalf("表单填写不可用")
	}
	t.FilledForm.Store(true)
	t.Emit("form_fill", map[string]any{"route": page.Route, "values": values})
	return map[string]any{"route": page.Route, "message": "已填写创建用户表单，请检查后确认提交；尚未创建用户"}, nil
}

func (t *Tools) formTool() (tool.BaseTool, error) {
	return utils.InferTool("fill_user_form", "打开创建用户表单并填入用户提供的账号、昵称、初始密码等信息，不提交、不创建用户。涉及初始密码时使用此工具，不要 propose_plan。未知字段留空，禁止编造密码或授权角色，最终由用户检查表单并确认提交。", func(ctx context.Context, in UserForm) (out any, err error) {
		t.FormPassword = in.Password
		if err = t.before(ctx, "fill_user_form"); err != nil {
			return nil, err
		}
		defer func() { t.after("fill_user_form", out, err) }()
		return t.FillUserForm(ctx, in)
	})
}

type FormDescriptor struct {
	Name       string `json:"name"`
	Summary    string `json:"summary"`
	Route      string `json:"route"`
	Path       string `json:"path"`
	Permission string `json:"permission"`
	Mode       string `json:"mode"`
}
type formNameInput struct {
	Name string `json:"name" jsonschema:"description=从 list_forms 返回的 name 选择，包括 ui. 开头的当前页面表单；不是知识主题或表单中文标题"`
}

type FormInput struct {
	Name     string         `json:"name" jsonschema:"description=从 list_forms 选择的表单能力名称"`
	RecordID string         `json:"recordId,omitempty" jsonschema:"description=修改或行操作的目标记录 ID，必须来自用户或真实查询，字符串形式"`
	Values   map[string]any `json:"values" jsonschema:"description=按 describe_form 提供的字段填充用户要求的数据；未指定字段由工具自动附加表单默认值，不要把默认值重复放入 values 覆盖已有输入；没有默认值的普通名称或标题可根据表单用途准备，凭据和权限不得猜测。不得提交表单"`
}

func formDescriptor(e *capability.Entry) (FormDescriptor, bool) {
	if e == nil || e.HTTP == nil || !e.Mutating || (e.HTTP.Method != "POST" && e.HTTP.Method != "PUT" && e.HTTP.Method != "PATCH") {
		return FormDescriptor{}, false
	}
	route := Route(e)
	if strings.HasPrefix(e.Name, "system.profile.") {
		route = "/system/profile"
	}
	if route == "" {
		return FormDescriptor{}, false
	}
	mode := "action"
	if strings.HasSuffix(e.Name, ".create") {
		mode = "create"
	} else if strings.HasSuffix(e.Name, ".update") {
		mode = "update"
	}
	return FormDescriptor{Name: e.Name, Summary: e.Summary, Route: route, Path: e.HTTP.Path, Permission: e.Perm, Mode: mode}, true
}
func (t *Tools) findForm(ctx context.Context, name string) (FormDescriptor, *capability.Entry, error) {
	a, err := actor(ctx)
	if err != nil {
		return FormDescriptor{}, nil, err
	}
	e, ok := capability.Lookup(name)
	if !ok || !a.Can(e.Perm) {
		return FormDescriptor{}, nil, errcode.NotFoundf("表单不存在或无权访问")
	}
	desc, ok := formDescriptor(e)
	if !ok {
		return FormDescriptor{}, nil, errcode.NotFoundf("未找到可填写的表单")
	}
	if _, err := t.findPage(ctx, desc.Route); err != nil {
		return FormDescriptor{}, nil, err
	}
	return desc, e, nil
}

var formFieldName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// FillForm only emits a browser draft. Business writes remain in the ordinary HTTP executor.
func (t *Tools) FillForm(ctx context.Context, in FormInput) (any, error) {
	var collect func(string, any)
	collect = func(key string, value any) {
		sensitive := capability.IsSensitive(key) || key == "configValue"
		switch value := value.(type) {
		case string:
			if sensitive && value != "" {
				t.FormSecrets = append(t.FormSecrets, value)
			}
		case map[string]any:
			for child, item := range value {
				if sensitive {
					collect(key, item)
				} else {
					collect(child, item)
				}
			}
		case []any:
			for _, item := range value {
				collect(key, item)
			}
		}
	}
	for key, value := range in.Values {
		collect(key, value)
	}
	if strings.HasPrefix(in.Name, "ui.") {
		return t.fillUIForm(ctx, in)
	}
	desc, e, err := t.findForm(ctx, in.Name)
	if err != nil {
		return nil, err
	}
	if name := t.focusedUIForm(ctx, desc, in.RecordID); name != "" {
		in.Name = name
		return t.fillUIForm(ctx, in)
	}
	if len(in.Values) == 0 {
		return nil, errcode.BadRequestf("当前表单没有可用的默认值，请补充需要填写的内容")
	}
	raw, err := json.Marshal(in.Values)
	if err != nil || len(raw) > 16<<10 || len(in.Values) > 80 {
		return nil, errcode.BadRequestf("表单内容过大或格式无效")
	}
	for key := range in.Values {
		if !formFieldName.MatchString(key) || e.InputSchema == nil || e.InputSchema.Properties[key] == nil {
			return nil, errcode.BadRequestf("表单字段无效，请先查询表单结构")
		}
	}
	if desc.Mode != "create" && !strings.HasPrefix(in.Name, "system.profile.") && in.RecordID == "" {
		return nil, errcode.BadRequestf("修改表单需要明确的目标记录 ID")
	}
	if len(in.RecordID) > 64 || strings.ContainsAny(in.RecordID, "/\\?#") {
		return nil, errcode.BadRequestf("记录 ID 无效")
	}
	if t.Emit == nil {
		return nil, errcode.Internalf("表单填写不可用")
	}
	t.FilledForm.Store(true)
	t.Emit("form_fill", map[string]any{"route": desc.Route, "path": desc.Path, "capability": desc.Name, "permission": desc.Permission, "mode": desc.Mode, "recordId": in.RecordID, "values": in.Values})
	return map[string]any{"route": desc.Route, "message": "已填充表单，请检查后确认提交；尚未保存"}, nil
}
func (t *Tools) genericFormTools() ([]tool.BaseTool, error) {
	list, err := utils.InferTool("list_forms", "搜索有权填写的新增、修改和操作表单。仅协助填表，包含须人工保存的配置、密码和授权表单。先查目录，不得猜测名称。", func(ctx context.Context, in keywordInput) (out any, err error) {
		if err = t.before(ctx, "list_forms"); err != nil {
			return nil, err
		}
		defer func() { t.after("list_forms", out, err) }()
		a, err := actor(ctx)
		if err != nil {
			return nil, err
		}
		rows := []FormDescriptor{}
		for _, form := range t.uiForms(ctx) {
			if in.Keyword == "" || strings.Contains(strings.ToLower(form.Name+form.Summary+form.Route), strings.ToLower(in.Keyword)) {
				rows = append(rows, FormDescriptor{Name: form.Name, Summary: form.Summary, Route: form.Route, Mode: uiFormMode(form), Permission: form.Permission})
			}
		}
		for _, e := range capability.All() {
			desc, ok := formDescriptor(e)
			if !ok || !a.Can(e.Perm) {
				continue
			}
			if in.Keyword != "" && !strings.Contains(strings.ToLower(desc.Name+desc.Summary+desc.Route), strings.ToLower(in.Keyword)) {
				continue
			}
			if _, err := t.findPage(ctx, desc.Route); err == nil {
				rows = append(rows, desc)
			}
			if len(rows) >= 40 {
				break
			}
		}
		return rows, nil
	})
	if err != nil {
		return nil, err
	}
	describe, err := utils.InferTool("describe_form", "读取表单字段 JSON Schema；新增和修改均使用真实字段，禁止猜测。这里只读结构，不读取凭据。", func(ctx context.Context, in formNameInput) (out any, err error) {
		if err = t.before(ctx, "describe_form"); err != nil {
			return nil, err
		}
		defer func() { t.after("describe_form", out, err) }()
		if strings.HasPrefix(in.Name, "ui.") {
			return t.describeUIForm(ctx, in.Name)
		}
		desc, e, err := t.findForm(ctx, in.Name)
		if err != nil {
			return nil, err
		}
		if name := t.focusedUIForm(ctx, desc, ""); name != "" {
			return t.describeUIForm(ctx, name)
		}
		return map[string]any{"form": desc, "schema": e.InputSchema}, nil
	})
	if err != nil {
		return nil, err
	}
	fill, err := utils.InferTool("fill_form", "填充当前新增、修改、查询或行操作表单的数据；仅未打开时才准备对应表单。未指定的字段使用已声明默认值并保留已有输入，不能只打开空表单后声称已填写。先 list_forms 和 describe_form；优先当前 ui 表单。用户确认后才保存，不执行业务写入。可填写用户明确提供的密码，但不得复述或放入计划。", func(ctx context.Context, in FormInput) (out any, err error) {
		if err = t.before(ctx, "fill_form"); err != nil {
			return nil, err
		}
		defer func() { t.after("fill_form", out, err) }()
		return t.FillForm(ctx, in)
	})
	if err != nil {
		return nil, err
	}
	return []tool.BaseTool{list, describe, fill}, nil
}

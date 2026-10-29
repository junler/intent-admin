// Package system projects the system management service into the one shared
// Capability execution path, including previews, changesets, undo and export.
package system

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/capability/undo"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	svc "github.com/junler/intent-admin/internal/service/system"
)

type Service = svc.Service

func principal(ctx context.Context) svc.Principal {
	a, _ := capability.ActorFrom(ctx)
	if a == nil {
		return svc.Principal{}
	}
	return svc.Principal{UserID: a.UserID, Name: a.UserName, Super: a.IsSuper(), Source: string(a.Source)}
}
func Register(db *gorm.DB, c *cache.Cache, cfg *config.Config) *Service {
	s := svc.New(db, c, cfg)
	for _, r := range svc.Resources {
		registerResource(s, r)
	}
	registerActions(s)
	registerProfile(s)
	registerTokens(s)
	return s
}
func meta(name, summary, perm, method, path string, risk capability.Risk, write, reversible bool) capability.Meta {
	m := capability.Meta{Name: name, Summary: summary, Perm: perm, Risk: risk, Mutating: write, Reversible: reversible, Idempotent: method != "POST", HTTP: &capability.HTTPBinding{Method: method, Path: path}, AIEnabled: true, MCPEnabled: true, LogTitle: summary, Tags: []string{"系统管理"}}
	if write {
		switch method {
		case "POST":
			m.BizType = capability.BizCreate
		case "PUT":
			m.BizType = capability.BizUpdate
		case "DELETE":
			m.BizType = capability.BizDelete
		}
		args := "{}"
		switch {
		case strings.HasSuffix(name, ".remove"):
			args = `{"ids":[2]}`
		case strings.HasSuffix(name, ".authRole"):
			args = `{"userId":2,"roleIds":[2]}`
		case strings.HasSuffix(name, ".authMenu"):
			args = `{"roleId":2,"menuIds":[2,3]}`
		case strings.HasSuffix(name, ".changeStatus"):
			args = `{"userId":2,"status":"1"}`
		}
		m.Examples = []capability.Example{{Utterance: summary, Args: args}}
	}
	return m
}
func registerResource(s *Service, r svc.Resource) {
	prefix := "system." + r.Name
	perm := "system:" + r.Perm + ":"
	m := meta(prefix+".list", "查询"+resourceTitle(r.Name), perm+"list", "GET", r.Route+"/list", capability.RiskLow, false, false)
	if r.Export {
		m.Export = &capability.ExportSpec{FileName: resourceTitle(r.Name) + "_{date}", Perm: perm + "export", Columns: exportColumns(r)}
	}
	capability.Register(capability.Capability[svc.Input, svc.ListOut]{Meta: m, Fn: func(ctx context.Context, in svc.Input) (svc.ListOut, error) { return s.List(ctx, r, in) }})
	capability.Register(capability.Capability[svc.Input, map[string]any]{Meta: meta(prefix+".get", "查看"+resourceTitle(r.Name), perm+"query", "GET", r.Route+"/:id", capability.RiskLow, false, false), Fn: func(ctx context.Context, in svc.Input) (map[string]any, error) {
		return s.Get(ctx, r, svc.InputID(r, in))
	}})
	for _, action := range []string{"create", "update", "remove"} {
		op, method, risk := action, "PUT", capability.RiskMedium
		permission := "edit"
		path := r.Route
		if action == "create" {
			method = "POST"
			permission = "add"
		}
		if action == "remove" {
			op = "delete"
			method = "DELETE"
			permission = "remove"
			path += "/:ids"
			risk = capability.RiskHigh
			if r.Name == "role" {
				risk = capability.RiskCritical
			}
		}
		capability.Register(capability.Capability[svc.Input, map[string]any]{
			Meta: resourceMeta(r, action, perm+permission, method, path, risk),
			Fn: func(ctx context.Context, in svc.Input) (map[string]any, error) {
				changes, err := s.Plan(ctx, r, in, op, principal(ctx))
				if err != nil {
					return nil, err
				}
				changes, err = s.Apply(ctx, r, changes, principal(ctx))
				if err != nil {
					return nil, err
				}
				if err = recordResource(ctx, s, r, changes); err != nil {
					return nil, err
				}
				if len(changes) == 1 {
					return map[string]any{svc.Camel(r.ID): changes[0].ID}, nil
				}
				return map[string]any{"affected": len(changes)}, nil
			},
			DryRun: func(ctx context.Context, in svc.Input) (*capability.Preview, error) {
				changes, err := s.Plan(ctx, r, in, op, principal(ctx))
				if err != nil {
					return nil, err
				}
				return preview(r.Table, r.Route, changes), nil
			},
			Undo: undoResource(s, r),
		})
	}
}
func record(ctx context.Context, entity string, changes []svc.Change) {
	rec := capability.RecorderFrom(ctx)
	for _, ch := range changes {
		switch ch.Op {
		case "create":
			rec.Create(entity, capability.ID(ch.ID), ch.Label, ch.After)
		case "delete":
			rec.Delete(entity, capability.ID(ch.ID), ch.Label, ch.Before)
		default:
			if len(ch.After) == 0 {
				rec.Skip(entity, capability.ID(ch.ID), ch.Label, "值未发生变化")
			} else {
				rec.Update(entity, capability.ID(ch.ID), ch.Label, ch.Before, ch.After)
			}
		}
	}
}
func preview(entity, route string, changes []svc.Change) *capability.Preview {
	p := &capability.Preview{Evidence: &capability.Evidence{Route: route}}
	for _, ch := range changes {
		op := capability.Op(ch.Op)
		if strings.HasPrefix(ch.Op, "sys_") {
			op = capability.OpUpdate
		}
		c := capability.Change{Entity: entity, ID: capability.ID(ch.ID), Label: ch.Label, Op: op}
		keys := []string{}
		for k := range ch.After {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if capability.IsSensitive(k) {
				continue
			}
			diff := capability.FieldDiff{Field: svc.Camel(k), Label: fieldLabel(k), Before: ch.Before[k], After: ch.After[k]}
			if ch.SecretConfig && k == "config_value" {
				if diff.Before != nil {
					diff.Before = "***"
				}
				if diff.After != nil {
					diff.After = "***"
				}
				p.Warn("凭据值已隐藏，回滚使用受保护的备份")
			}

			if dict := fieldDictionary(entity, k); dict != "" {
				if ch.Before[k] != nil {
					diff.BeforeText = dict + ":" + fmt.Sprint(ch.Before[k])
				}
				if ch.After[k] != nil {
					diff.AfterText = dict + ":" + fmt.Sprint(ch.After[k])
				}
			}
			c.Fields = append(c.Fields, diff)
		}
		if op == capability.OpUpdate && len(c.Fields) == 0 {
			c.Op = capability.OpSkip
			c.Reason = "值未发生变化"
		}
		p.AddChange(c)
	}
	p.Summary = fmt.Sprintf("将影响 %d 条记录", p.Affected)
	return p
}
func undoResource(s *Service, r svc.Resource) func(context.Context, json.RawMessage) error {
	return func(ctx context.Context, raw json.RawMessage) error {
		if r.Name == "config" {
			var custom configUndoData
			if json.Unmarshal(raw, &custom) == nil && len(custom.Rows) > 0 {
				return undoConfig(s, ctx, custom)
			}
		}
		var p undo.Payload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.Entity != r.Table {
			return errcode.BadRequestf("回滚实体不匹配")
		}
		id, err := parseID(p.ID)
		if err != nil {
			return err
		}
		policy := p
		if p.Op == capability.OpDelete {
			policy.Op = capability.OpUpdate
			policy.Before = cloneFields(p.Before)
			policy.Before["del_flag"] = "0"
		}
		if err = AuthorizeUndo(s)(ctx, policy); err != nil {
			return err
		}
		// Re-enter the policy service before compensating; rollback cannot bypass
		// administrator protection that a normal write would enforce.
		if p.Op == capability.OpCreate {
			if err = s.CanUndoCreate(ctx, r, id); err != nil {
				return err
			}
			if _, err = s.Plan(ctx, r, svc.Input{ID: id, IDs: []int64{id}}, "delete", principal(ctx)); err != nil {
				return err
			}
		} else if p.Op == capability.OpUpdate {
			input, err := inputPatch(r, id, p.Before)
			if err != nil {
				return err
			}
			if _, err = s.Plan(ctx, r, input, "update", principal(ctx)); err != nil {
				return err
			}
		}

		// Account creation undo preserves the bcrypt hash in the hidden row. That
		// lets an inverse rollback restore the account without persisting a hash in
		// any changeset snapshot or compensation payload.
		if p.Op == capability.OpCreate && r.Name == "user" {
			changes, err := s.Plan(ctx, r, svc.Input{ID: id, IDs: []int64{id}}, "delete", principal(ctx))
			if err != nil {
				return err
			}
			_, err = s.Apply(ctx, r, changes, principal(ctx))
			return err
		}
		fn := undo.Auto(s.DB, undo.Table{Name: r.Table, IDField: r.ID}, "del_flag", "0")
		if err = fn(ctx, raw); err != nil {
			return err
		}
		dbx.AfterCommit(ctx, func() {
			_ = s.Invalidate(context.Background(), r.Table, p.ID)
			if r.Name == "dictType" || r.Name == "dictData" {
				_ = s.ClearCache(context.Background(), "dict")
			}
			if r.Name == "config" {
				_ = s.ClearCache(context.Background(), "config")
			}
		})
		return nil
	}
}
func inputPatch(r svc.Resource, id int64, patch map[string]any) (svc.Input, error) {
	d := map[string]any{"id": id}
	for k, v := range patch {
		d[svc.Camel(k)] = v
	}
	raw, _ := json.Marshal(d)
	var in svc.Input
	err := json.Unmarshal(raw, &in)
	return in, err
}

func parseID(id string) (int64, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return 0, errcode.BadRequestf("编号无效")
	}
	return n, nil
}

func resourceTitle(name string) string {
	titles := map[string]string{"user": "用户", "role": "角色", "menu": "菜单", "dictType": "字典类型", "dictData": "字典数据", "config": "系统参数", "notice": "通知公告"}
	return titles[name]
}
func resourceMeta(r svc.Resource, action, perm, method, path string, risk capability.Risk) capability.Meta {
	verb := map[string]string{"create": "新增", "update": "修改", "remove": "删除"}[action]
	m := meta("system."+r.Name+"."+action, verb+resourceTitle(r.Name), perm, method, path, risk, true, true)
	args := map[string]any{}
	if action == "remove" {
		args["ids"] = []int64{2}
	} else if action == "update" {
		args[svc.Camel(r.ID)] = 2
		args["remark"] = "已复核"
	} else {
		for _, field := range r.Required {
			args[svc.Camel(field)] = "example"
		}
		if r.Name == "menu" {
			args["menuType"] = "C"
			args["perms"] = "example:item:list"
			args["path"] = "/example"
			args["component"] = "demo/item/index"
		}
	}
	raw, _ := json.Marshal(args)
	m.Examples = []capability.Example{{Utterance: verb + "一个" + resourceTitle(r.Name), Args: string(raw)}}
	return m
}

func exportColumns(r svc.Resource) []capability.ExportColumn {
	cols := []capability.ExportColumn{{Field: svc.Camel(r.ID), Label: "编号"}, {Field: svc.Camel(r.Label), Label: "名称", Width: 24}}
	switch r.Name {
	case "user":
		cols = append(cols, capability.ExportColumn{Field: "nickName", Label: "昵称"}, capability.ExportColumn{Field: "email", Label: "邮箱", Width: 28}, capability.ExportColumn{Field: "phonenumber", Label: "手机"})
	case "role":
		cols = append(cols, capability.ExportColumn{Field: "roleKey", Label: "权限标识", Width: 28}, capability.ExportColumn{Field: "roleSort", Label: "排序"})
	case "config":
		cols = append(cols, capability.ExportColumn{Field: "configKey", Label: "参数键", Width: 32}, capability.ExportColumn{Field: "configValue", Label: "参数值", Width: 32})
	case "dictData":
		cols = append(cols, capability.ExportColumn{Field: "dictType", Label: "字典类型", Width: 24}, capability.ExportColumn{Field: "dictValue", Label: "字典键值"}, capability.ExportColumn{Field: "dictSort", Label: "排序"})
	}
	return append(cols, capability.ExportColumn{Field: "status", Label: "状态", Dict: "sys_normal_disable"}, capability.ExportColumn{Field: "createTime", Label: "创建时间", Format: capability.FormatDateTime, Width: 22})
}

func fieldDictionary(entity, field string) string {
	switch field {
	case "status":
		return "sys_normal_disable"
	case "sex":
		return "sys_user_sex"
	case "visible":
		return "sys_show_hide"
	case "is_default", "config_type":
		return "sys_yes_no"
	case "notice_type":
		return "sys_notice_type"
	case "ai_max_risk":
		return "sys_risk_level"
	}
	return ""
}
func fieldLabel(field string) string {
	labels := map[string]string{"user_name": "用户名", "nick_name": "昵称", "email": "邮箱", "phonenumber": "手机", "sex": "性别", "avatar": "头像", "status": "状态", "remark": "备注", "role_name": "角色名称", "role_key": "角色标识", "role_sort": "角色排序", "menu_check_strictly": "菜单父子联动", "menu_name": "菜单名称", "parent_id": "父菜单", "order_num": "显示排序", "path": "路由路径", "component": "页面组件", "query": "默认查询参数", "is_frame": "外链设置", "is_cache": "缓存设置", "menu_type": "菜单类型", "visible": "显示设置", "perms": "权限字符", "icon": "图标", "dict_name": "字典名称", "dict_type": "字典类型", "dict_label": "字典标签", "dict_value": "字典键值", "dict_sort": "字典排序", "css_class": "样式", "list_class": "标签颜色", "is_default": "默认设置", "config_name": "参数名称", "config_key": "参数键", "config_value": "参数值", "config_type": "内置参数", "notice_title": "标题", "notice_type": "通知类型", "notice_content": "内容", "ai_max_risk": "AI 风险上限", "role_ids": "角色授权", "menu_ids": "菜单授权", "permissions": "额外授权", "del_flag": "删除标记", "name": "名称", "scopes": "权限范围", "maxRisk": "风险上限", "expiresAt": "过期时间"}
	if label := labels[field]; label != "" {
		return label
	}
	return svc.Camel(field)
}

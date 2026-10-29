package system

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/capability/undo"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	svc "github.com/junler/intent-admin/internal/service/system"
)

func registerActions(s *Service) {
	r := svc.Spec("user")
	capability.Register(capability.Capability[svc.Input, capability.Empty]{
		Meta: meta("system.user.changeStatus", "启停用户", "system:user:edit", "PUT", r.Route+"/changeStatus", capability.RiskMedium, true, true),
		Fn: func(ctx context.Context, in svc.Input) (capability.Empty, error) {
			if in.Status == nil {
				return capability.Empty{}, errcode.BadRequestf("status 必填")
			}
			in = svc.Input{ID: svc.InputID(r, in), Status: in.Status}
			changes, err := s.Plan(ctx, r, in, "update", principal(ctx))
			if err == nil {
				changes, err = s.Apply(ctx, r, changes, principal(ctx))
				record(ctx, r.Table, changes)
			}
			return capability.Empty{}, err
		},
		DryRun: func(ctx context.Context, in svc.Input) (*capability.Preview, error) {
			if in.Status == nil {
				return nil, errcode.BadRequestf("status 必填")
			}
			changes, err := s.Plan(ctx, r, svc.Input{ID: svc.InputID(r, in), Status: in.Status}, "update", principal(ctx))
			return preview(r.Table, r.Route, changes), err
		}, Undo: undoResource(s, r),
	})
	passwordMeta := meta("system.user.resetPwd", "重置用户密码", "system:user:resetPwd", "PUT", r.Route+"/resetPwd", capability.RiskHigh, true, false)
	passwordMeta.AIEnabled = false
	passwordMeta.MCPEnabled = false
	capability.Register(capability.Capability[svc.Input, capability.Empty]{Meta: passwordMeta,
		Fn: func(ctx context.Context, in svc.Input) (capability.Empty, error) {
			if in.Password == nil {
				return capability.Empty{}, errcode.BadRequestf("password 必填")
			}
			id := svc.InputID(r, in)
			err := s.Password(ctx, id, *in.Password, principal(ctx), nil)
			if err == nil {
				capability.RecorderFrom(ctx).Update("sys_user", capability.ID(id), "密码已变更", nil, map[string]any{"credentialChanged": true})
			}
			return capability.Empty{}, err
		},
		DryRun: func(ctx context.Context, in svc.Input) (*capability.Preview, error) {
			if in.Password == nil || len(*in.Password) < 6 || len(*in.Password) > 72 {
				return nil, errcode.BadRequestf("密码为 6–72 字节")
			}
			if _, err := s.Plan(ctx, r, svc.Input{ID: svc.InputID(r, in)}, "update", principal(ctx)); err != nil {
				return nil, err
			}
			p := &capability.Preview{Summary: "重置用户密码并退出全部浏览器会话"}
			p.AddChange(capability.Change{Entity: r.Table, ID: capability.ID(svc.InputID(r, in)), Label: "重置登录密码", Op: capability.OpUpdate})
			p.Warn("密码变更不可回滚，将使已有登录会话失效")
			return p, nil
		},
	})
	for _, g := range []struct {
		kind, name, perm, path string
		risk                   capability.Risk
	}{{"userRole", "system.user.authRole", "system:user:edit", "/system/user/authRole", capability.RiskHigh}, {"userPerm", "system.user.authPerm", "system:user:authPerm", "/system/user/authPerm", capability.RiskCritical}, {"roleMenu", "system.role.authMenu", "system:role:edit", "/system/role/authMenu", capability.RiskHigh}} {
		m := meta(g.name, "配置授权", g.perm, "PUT", g.path, g.risk, true, true)
		if g.kind == "userPerm" {
			m.AIEnabled = false
			m.MCPEnabled = false
		}
		capability.Register(capability.Capability[svc.Input, capability.Empty]{Meta: m,
			Fn: func(ctx context.Context, in svc.Input) (capability.Empty, error) {
				ch, err := s.GrantPlan(ctx, g.kind, in, principal(ctx))
				if err != nil {
					return capability.Empty{}, err
				}
				if err = s.ApplyGrant(ctx, ch, principal(ctx)); err != nil {
					return capability.Empty{}, err
				}
				record(ctx, ch.Op, []svc.Change{ch})
				return capability.Empty{}, nil
			},
			DryRun: func(ctx context.Context, in svc.Input) (*capability.Preview, error) {
				ch, err := s.GrantPlan(ctx, g.kind, in, principal(ctx))
				if err != nil {
					return nil, err
				}
				return preview(ch.Op, g.path, []svc.Change{ch}), nil
			},
			Undo: func(ctx context.Context, raw json.RawMessage) error {
				var p undo.Payload
				if err := json.Unmarshal(raw, &p); err != nil {
					return err
				}
				id, err := parseID(p.ID)
				if err != nil {
					return err
				}
				in := svc.Input{}
				switch g.kind {
				case "userRole":
					in.UserID = &id
					ids, err := numericIDs(p.Before["role_ids"])
					if err != nil {
						return err
					}
					in.RoleIDs = &ids
				case "roleMenu":
					in.RoleID = &id
					ids, err := numericIDs(p.Before["menu_ids"])
					if err != nil {
						return err
					}
					in.MenuIDs = &ids
				case "userPerm":
					in.UserID = &id
					perms := []string{}
					switch v := p.Before["permissions"].(type) {
					case []any:
						for _, p := range v {
							perms = append(perms, fmt.Sprint(p))
						}
					case []string:
						perms = v
					}
					in.Permissions = &perms
				}
				ch, err := s.GrantPlan(ctx, g.kind, in, principal(ctx))
				if err != nil {
					return err
				}
				if ch.Op != p.Entity {
					return errcode.BadRequestf("回滚实体不匹配")
				}
				return s.ApplyGrant(ctx, ch, principal(ctx))
			},
		})
	}
	m := meta("system.user.setAiRisk", "设置用户 AI 风险上限", "system:user:authPerm", "PUT", "/system/user/aiRisk", capability.RiskCritical, true, true)
	m.AIEnabled = false
	m.MCPEnabled = false
	capability.Register(capability.Capability[svc.Input, capability.Empty]{Meta: m,
		Fn: func(ctx context.Context, in svc.Input) (capability.Empty, error) {
			ch, err := riskPlan(ctx, s, in)
			if err != nil {
				return capability.Empty{}, err
			}
			changes, err := s.Apply(ctx, r, []svc.Change{ch}, principal(ctx))
			if err == nil {
				record(ctx, r.Table, changes)
			}
			return capability.Empty{}, err
		},
		DryRun: func(ctx context.Context, in svc.Input) (*capability.Preview, error) {
			ch, err := riskPlan(ctx, s, in)
			return preview(r.Table, r.Route, []svc.Change{ch}), err
		},
		Undo: func(ctx context.Context, raw json.RawMessage) error {
			var p undo.Payload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			id, err := parseID(p.ID)
			if err != nil {
				return err
			}
			in := svc.Input{UserID: &id}
			if v := p.Before["ai_max_risk"]; v != nil {
				value := fmt.Sprint(v)
				in.AIMaxRisk = &value
			}
			ch, err := riskPlan(ctx, s, in)
			if err != nil {
				return err
			}
			_, err = s.Apply(ctx, r, []svc.Change{ch}, principal(ctx))
			return err
		},
	})
	capability.Register(capability.Capability[svc.Input, []map[string]any]{Meta: meta("system.menu.tree", "菜单选择树", "system:menu:list", "GET", "/system/menu/treeselect", capability.RiskLow, false, false), Fn: func(ctx context.Context, in svc.Input) ([]map[string]any, error) {
		in.Query.PageSize = 50000
		in.Query.Unbounded = true
		out, err := s.List(ctx, svc.Spec("menu"), in)
		if err != nil {
			return nil, err
		}
		return menuTree(out.Rows, 0, map[int64]bool{}), nil
	}})
	capability.Register(capability.Capability[svc.Input, []map[string]any]{Meta: meta("system.dictData.byType", "按类型查询字典", "", "GET", "/system/dict/data/type/:dictType", capability.RiskLow, false, false), Fn: func(ctx context.Context, in svc.Input) ([]map[string]any, error) {
		if in.DictType == nil {
			return nil, errcode.BadRequestf("dictType 必填")
		}
		return s.Dictionary(ctx, *in.DictType)
	}})
	capability.Register(capability.Capability[svc.Input, string]{Meta: meta("system.config.byKey", "按键查询参数", "system:config:query", "GET", "/system/config/configKey/:configKey", capability.RiskLow, false, false), Fn: func(ctx context.Context, in svc.Input) (string, error) {
		if in.ConfigKey == nil {
			return "", errcode.BadRequestf("configKey 必填")
		}
		if svc.IsCredentialConfigKey(*in.ConfigKey) {
			if principal(ctx).Source != "http" {
				return "", errcode.Forbiddenf("凭据参数不能通过 AI 或 MCP 读取")
			}
			if _, err := s.ConfigValue(ctx, *in.ConfigKey); err != nil {
				return "", err
			}
			return "***", nil
		}
		return s.ConfigValue(ctx, *in.ConfigKey)
	}})
	for _, kind := range []string{"dict", "config"} {
		capability.Register(capability.Capability[capability.Empty, capability.Empty]{Meta: meta("system."+kind+".refreshCache", "刷新缓存", "system:"+kind+":edit", "DELETE", "/system/"+kind+"/refreshCache", capability.RiskLow, true, false), Fn: func(ctx context.Context, in capability.Empty) (capability.Empty, error) {
			return capability.Empty{}, s.ClearCache(ctx, kind)
		}, DryRun: func(ctx context.Context, in capability.Empty) (*capability.Preview, error) {
			return &capability.Preview{Summary: "清空可重新生成的缓存", Affected: 1}, nil
		}})
	}
}
func numericIDs(value any) ([]int64, error) {
	out := []int64{}
	switch v := value.(type) {
	case []int64:
		return v, nil
	case []any:
		for _, n := range v {
			if f, ok := n.(float64); ok {
				out = append(out, int64(f))
				continue
			}
			f, e := strconv.ParseInt(fmt.Sprint(n), 10, 64)
			if e != nil {
				return nil, e
			}
			out = append(out, f)
		}
	}
	return out, nil
}
func riskPlan(ctx context.Context, s *Service, in svc.Input) (svc.Change, error) {
	id := svc.InputID(svc.Spec("user"), in)
	if _, err := s.Plan(ctx, svc.Spec("user"), svc.Input{ID: id}, "update", principal(ctx)); err != nil {
		return svc.Change{}, err
	}
	row, err := s.Get(ctx, svc.Spec("user"), id)
	if err != nil {
		return svc.Change{}, err
	}
	var desired any
	if in.AIMaxRisk != nil && *in.AIMaxRisk != "" {
		risk, err := capability.ParseRisk(*in.AIMaxRisk)
		if err != nil {
			return svc.Change{}, errcode.BadRequestf("风险等级无效")
		}
		global, err := capability.ParseRisk(s.Cfg.AI.MaxRisk)
		if err != nil {
			return svc.Change{}, err
		}
		if risk.Level() > global.Level() {
			return svc.Change{}, errcode.BadRequestf("用户风险上限不能高于全局配置")
		}
		desired = string(risk)
	}
	return svc.Change{ID: id, Label: fmt.Sprint(row["userName"]), Op: "update", Before: map[string]any{"ai_max_risk": row["aiMaxRisk"]}, After: map[string]any{"ai_max_risk": desired}}, nil
}
func menuTree(rows []map[string]any, parent int64, seen map[int64]bool) []map[string]any {
	out := []map[string]any{}
	for _, row := range rows {
		id := svc.ID(row, "menuId")
		if svc.ID(row, "parentId") != parent || seen[id] {
			continue
		}
		seen[id] = true
		children := menuTree(rows, id, seen)
		delete(seen, id)
		node := map[string]any{"id": id, "label": row["menuName"]}
		if len(children) > 0 {
			node["children"] = children
		}
		out = append(out, node)
	}
	return out
}

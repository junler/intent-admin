package system

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"gorm.io/gorm/clause"

	"github.com/junler/intent-admin/internal/pkg/errcode"
)

// GuardCompensation applies the same hard policies to inverse rollback writes.
// It accepts the persisted changed fields, never caller-selected SQL columns.
func (s *Service) GuardCompensation(ctx context.Context, entity, id, op string, before, after map[string]any, p Principal) error {
	known := entity == "sys_user_role" || entity == "sys_role_menu" || entity == "sys_user_perm"
	for _, r := range Resources {
		if r.Table == entity {
			known = true
			break
		}
	}
	if !known {
		return nil
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return errcode.BadRequestf("补偿记录编号无效")
	}
	if entity == "sys_user_role" || entity == "sys_role_menu" || entity == "sys_user_perm" {
		in := Input{}
		kind, field := "userRole", "role_ids"
		in.UserID = &n
		if entity == "sys_role_menu" {
			kind, field = "roleMenu", "menu_ids"
			in.RoleID = &n
		}
		if entity == "sys_user_perm" {
			kind, field = "userPerm", "permissions"
		}
		raw, _ := json.Marshal(before[field])
		if kind == "userPerm" {
			v := []string{}
			if err = json.Unmarshal(raw, &v); err != nil {
				return err
			}
			in.Permissions = &v
		} else {
			v := []int64{}
			if err = json.Unmarshal(raw, &v); err != nil {
				return err
			}
			if kind == "userRole" {
				in.RoleIDs = &v
			} else {
				in.MenuIDs = &v
			}
		}
		_, err = s.GrantPlan(ctx, kind, in, p)
		return err
	}
	var resource *Resource
	for _, r := range Resources {
		if r.Table == entity {
			copy := r
			resource = &copy
			break
		}
	}
	if resource == nil {
		return nil
	}
	r := *resource
	if op == "create" {
		if err = s.CanUndoCreate(ctx, r, n); err != nil {
			return err
		}
		_, err = s.Plan(ctx, r, Input{ID: n, IDs: []int64{n}}, "delete", p)
		return err
	}

	if r.Name == "config" && IsCredentialConfigKey(fmt.Sprint(before["config_key"])) && p.Source != "http" {
		return errcode.Forbiddenf("凭据参数只能在页面恢复")
	}
	if op == "delete" {
		if r.Name == "user" {
			return errcode.Conflictf("已物理删除的账号无法从不含凭据的快照自动重建")
		}
		return s.validate(ctx, r, nil, before, true, p)
	}
	// Compensation can restore a soft-delete flag, so the current row may be
	// hidden from ordinary CRUD lookups. It still has to pass the hard guards.
	row := map[string]any{}
	err = s.db(ctx).Table(entity).Clauses(clause.Locking{Strength: "UPDATE"}).Where(r.ID+"=?", n).Take(&row).Error
	if err != nil {
		return err
	}
	if r.Name == "config" && IsCredentialConfigKey(fmt.Sprint(row["config_key"])) && p.Source != "http" {
		return errcode.Forbiddenf("凭据参数只能在页面恢复")
	}
	operation := "update"
	if before["del_flag"] == "2" {
		operation = "delete"
	}
	if err = s.protect(ctx, r, row, before, operation, p); err != nil {
		return err
	}
	if op != "update" {
		return errcode.BadRequestf("不支持的补偿类型 %s", op)
	}
	if risk, ok := before["ai_max_risk"]; ok && risk != nil {
		levels := map[string]int{"low": 0, "medium": 1, "high": 2, "critical": 3}
		level, ok := levels[fmt.Sprint(risk)]
		global, globalOK := levels[s.Cfg.AI.MaxRisk]
		if !ok || !globalOK || level > global {
			return errcode.BadRequestf("恢复的 AI 风险上限超过当前全局配置")
		}
	}
	return s.validate(ctx, r, row, before, false, p)
}

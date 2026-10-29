package system

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

func (s *Service) GrantPlan(ctx context.Context, kind string, in Input, p Principal) (Change, error) {
	userSpec, roleSpec := Spec("user"), Spec("role")
	id := InputID(userSpec, in)
	entity := "sys_user_role"
	field := "role_ids"
	if kind == "roleMenu" {
		id = InputID(roleSpec, in)
		entity = "sys_role_menu"
		field = "menu_ids"
	}
	if kind == "userPerm" {
		entity = "sys_user_perm"
		field = "permissions"
	}
	target := userSpec
	if kind == "roleMenu" {
		target = roleSpec
	}
	row, err := s.row(ctx, target, id, true)
	if err != nil {
		return Change{}, err
	}
	if err = s.protect(ctx, target, row, nil, "update", p); err != nil {
		return Change{}, err
	}
	before := map[string]any{}
	after := map[string]any{}
	switch kind {
	case "userRole":
		if !dbx.IsReadOnly(ctx) {
			if err = s.db(ctx).Exec("SELECT pg_advisory_xact_lock(684286211)").Error; err != nil {
				return Change{}, err
			}
		}
		if in.RoleIDs == nil {
			return Change{}, errcode.BadRequestf("roleIds 必填（可为空数组）")
		}
		ids := slices.Clone(*in.RoleIDs)
		slices.Sort(ids)
		ids = slices.Compact(ids)
		if len(ids) > 200 {
			return Change{}, errcode.BadRequestf("角色过多")
		}
		for _, rid := range ids {
			r, err := s.row(ctx, roleSpec, rid, false)
			if err != nil {
				return Change{}, err
			}
			if r["status"] != "0" {
				return Change{}, errcode.BadRequestf("不能授予停用角色")
			}
			if r["role_key"] == "admin" && !p.Super {
				return Change{}, errcode.Forbiddenf("只有超管可以授予超管角色")
			}
		}
		old := []int64{}
		if err = s.db(ctx).Table(entity).Where("user_id=?", id).Order("role_id").Pluck("role_id", &old).Error; err != nil {
			return Change{}, err
		}
		// Never remove the final usable administrator while changing role links.
		wasAdmin := false
		willAdmin := false
		for _, rid := range old {
			r, e := s.row(ctx, roleSpec, rid, false)
			if e == nil && r["role_key"] == "admin" {
				wasAdmin = true
			}
		}
		for _, rid := range ids {
			r, _ := s.row(ctx, roleSpec, rid, false)
			if r["role_key"] == "admin" {
				willAdmin = true
			}
		}
		if wasAdmin && !willAdmin {
			var n int64
			if err = s.db(ctx).Raw(`SELECT COUNT(DISTINCT u.user_id) FROM sys_user u JOIN sys_user_role ur ON ur.user_id=u.user_id JOIN sys_role r ON r.role_id=ur.role_id WHERE u.status='0' AND u.del_flag='0' AND r.role_key='admin' AND r.status='0' AND r.del_flag='0' AND u.user_id<>?`, id).Scan(&n).Error; err != nil {
				return Change{}, err
			}
			if n == 0 {
				return Change{}, errcode.Forbiddenf("至少保留一个可用超管")
			}
		}
		before[field] = old
		after[field] = ids
	case "roleMenu":
		if in.MenuIDs == nil {
			return Change{}, errcode.BadRequestf("menuIds 必填（可为空数组）")
		}
		ids := slices.Clone(*in.MenuIDs)
		slices.Sort(ids)
		ids = slices.Compact(ids)
		if len(ids) > 2000 {
			return Change{}, errcode.BadRequestf("菜单过多")
		}
		for _, mid := range ids {
			m, e := s.row(ctx, Spec("menu"), mid, false)
			if e != nil {
				return Change{}, e
			}
			if m["perms"] == "*:*:*" && !p.Super {
				return Change{}, errcode.Forbiddenf("只有超管可以授予通配权限")
			}
		}
		old := []int64{}
		if err = s.db(ctx).Table(entity).Where("role_id=?", id).Order("menu_id").Pluck("menu_id", &old).Error; err != nil {
			return Change{}, err
		}
		before[field] = old
		after[field] = ids
	case "userPerm":
		if in.Permissions == nil {
			return Change{}, errcode.BadRequestf("permissions 必填（可为空数组）")
		}
		perms := slices.Clone(*in.Permissions)
		slices.Sort(perms)
		perms = slices.Compact(perms)
		if len(perms) > 2000 {
			return Change{}, errcode.BadRequestf("权限过多")
		}
		for _, perm := range perms {
			if len(perm) > 100 || strings.TrimSpace(perm) == "" {
				return Change{}, errcode.BadRequestf("权限字符不合法")
			}
			if perm == "*:*:*" && !p.Super {
				return Change{}, errcode.Forbiddenf("只有超管可以授予通配权限")
			}
			if perm != "*:*:*" {
				var n int64
				if err = s.db(ctx).Table("sys_menu").Where("perms=? AND status='0' AND del_flag='0'", perm).Count(&n).Error; err != nil {
					return Change{}, err
				}
				if n == 0 {
					return Change{}, errcode.BadRequestf("权限 %s 不存在", perm)
				}
			}
		}
		old := []string{}
		if err = s.db(ctx).Table(entity).Where("user_id=?", id).Order("perm").Pluck("perm", &old).Error; err != nil {
			return Change{}, err
		}
		before[field] = old
		after[field] = perms
	default:
		return Change{}, errcode.BadRequestf("未知授权类型")
	}
	return Change{ID: id, Label: fmt.Sprint(row[target.Label]), Op: entity, Before: before, After: after}, nil
}
func (s *Service) ApplyGrant(ctx context.Context, ch Change, p Principal) error {
	d := s.db(ctx)
	owner, column, field := "user_id", "role_id", "role_ids"
	if ch.Op == "sys_role_menu" {
		owner, column, field = "role_id", "menu_id", "menu_ids"
	}
	if ch.Op == "sys_user_perm" {
		column, field = "perm", "permissions"
	}
	if ch.Op != "sys_user_role" && ch.Op != "sys_role_menu" && ch.Op != "sys_user_perm" {
		return errcode.BadRequestf("授权表不合法")
	}
	if err := d.Exec("DELETE FROM "+ch.Op+" WHERE "+owner+" = ?", ch.ID).Error; err != nil {
		return err
	}
	values := []any{}
	switch v := ch.After[field].(type) {
	case []int64:
		for _, n := range v {
			values = append(values, n)
		}
	case []string:
		for _, n := range v {
			values = append(values, n)
		}
	case []any:
		values = v
	}
	for _, v := range values {
		row := map[string]any{owner: ch.ID, column: v}
		if field == "permissions" {
			row["create_by"] = p.Name
			row["create_time"] = time.Now()
		}
		if err := d.Table(ch.Op).Create(&row).Error; err != nil {
			return dbError(err)
		}
	}
	dbx.AfterCommit(ctx, func() { _ = s.Invalidate(context.Background(), ch.Op, fmt.Sprint(ch.ID)) })
	return nil
}
func (s *Service) Password(ctx context.Context, id int64, password string, p Principal, old *string) error {
	if len(password) < 6 || len(password) > 72 {
		return errcode.BadRequestf("密码为 6–72 字节")
	}
	row, err := s.row(ctx, Spec("user"), id, true)
	if err != nil {
		return err
	}
	if old != nil {
		if id != p.UserID {
			return errcode.Forbiddenf("只能修改自己的密码")
		}
		if bcrypt.CompareHashAndPassword([]byte(fmt.Sprint(row["password"])), []byte(*old)) != nil {
			return errcode.BadRequestf("原密码不正确")
		}
	} else if err = s.protect(ctx, Spec("user"), row, nil, "update", p); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err = s.db(ctx).Table("sys_user").Where("user_id=?", id).Updates(map[string]any{"password": string(hash), "update_by": p.Name, "update_time": time.Now()}).Error; err != nil {
		return err
	}
	dbx.AfterCommit(ctx, func() { _ = s.RevokeSessions(context.Background(), id) })
	return nil
}
func (s *Service) RevokeSessions(ctx context.Context, id int64) error {
	var cursor uint64
	for {
		keys, next, err := s.Cache.Scan(ctx, cursor, s.Cache.Key("login_token", "*"), 100).Result()
		if err != nil {
			return err
		}
		for _, key := range keys {
			raw, e := s.Cache.Get(ctx, key).Result()
			if e != nil {
				continue
			}
			var snapshot struct {
				UserID int64 `json:"userId"`
			}
			if json.Unmarshal([]byte(raw), &snapshot) == nil && snapshot.UserID == id {
				if e = s.Cache.Del(ctx, key).Err(); e != nil {
					return e
				}
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func (s *Service) CheckPassword(ctx context.Context, id int64, old string) error {
	row, err := s.row(ctx, Spec("user"), id, false)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(fmt.Sprint(row["password"])), []byte(old)) != nil {
		return errcode.BadRequestf("原密码不正确")
	}
	return nil
}

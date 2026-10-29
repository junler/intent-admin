package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type Service struct {
	DB                 *gorm.DB
	Cache              *cache.Cache
	Cfg                *config.Config
	OnDictionaryChange func()
}

func New(db *gorm.DB, c *cache.Cache, cfg *config.Config) *Service {
	return &Service{DB: db, Cache: c, Cfg: cfg}
}
func (s *Service) db(ctx context.Context) *gorm.DB { return dbx.DBFrom(ctx, s.DB) }
func ID(row map[string]any, key string) int64 {
	switch v := row[key].(type) {
	case int64:
		return v
	case int32:
		return int64(v)
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}
func InputID(r Resource, in Input) int64 {
	if in.ID > 0 {
		return in.ID
	}
	return ID(Data(in), r.ID)
}
func dbError(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			return errcode.Conflictf("名称或标识已存在")
		case "23503":
			return errcode.Conflictf("记录仍被使用，或关联记录不存在")
		case "23514", "22001", "22P02":
			return errcode.BadRequestf("字段值不符合要求")
		}
	}
	return err
}
func (s *Service) List(ctx context.Context, r Resource, in Input) (ListOut, error) {
	d := s.db(ctx).Table(r.Table).Where("del_flag = '0'")
	data := Data(in)
	for _, f := range r.Fields {
		v, ok := data[f]
		if !ok || v == "" {
			continue
		}
		if strings.Contains(f, "name") || f == r.Label {
			d = d.Where(f+" LIKE ?", "%"+fmt.Sprint(v)+"%")
		} else if slices.Contains([]string{"status", "dict_type", "menu_type", "notice_type", "config_key", "parent_id", "sex", "user_id"}, f) {
			d = d.Where(f+" = ?", v)
		}
	}
	var total int64
	if err := d.Count(&total).Error; err != nil {
		return ListOut{}, err
	}
	allowed := map[string]string{"createTime": "create_time", "updateTime": "update_time", Camel(r.ID): r.ID, Camel(r.Label): r.Label}
	for _, f := range r.Fields {
		allowed[Camel(f)] = f
	}
	order, err := in.OrderScope(allowed, r.ID)
	if err != nil {
		return ListOut{}, err
	}
	rows := []map[string]any{}
	if err = d.Scopes(order, in.Scope()).Find(&rows).Error; err != nil {
		return ListOut{}, err
	}
	out := ListOut{Rows: make([]map[string]any, 0, len(rows)), Total: total, PageNum: in.PageNum, PageSize: in.PageSize}
	for _, row := range rows {
		out.Rows = append(out.Rows, View(row))
	}
	return out, nil
}
func (s *Service) row(ctx context.Context, r Resource, id int64, lock bool) (map[string]any, error) {
	if id <= 0 {
		return nil, errcode.BadRequestf("记录编号必须大于 0")
	}
	d := s.db(ctx).Table(r.Table).Where(r.ID+" = ? AND del_flag = '0'", id)
	if lock && !dbx.IsReadOnly(ctx) {
		d = d.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	row := map[string]any{}
	if err := d.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFoundf("记录不存在")
		}
		return nil, err
	}
	return row, nil
}
func (s *Service) Get(ctx context.Context, r Resource, id int64) (map[string]any, error) {
	row, err := s.row(ctx, r, id, false)
	if err != nil {
		return nil, err
	}
	out := View(row)
	if r.Name == "user" {
		roles := []int64{}
		if err = s.db(ctx).Table("sys_user_role").Where("user_id = ?", id).Pluck("role_id", &roles).Error; err != nil {
			return nil, err
		}
		perms := []string{}
		if err = s.db(ctx).Table("sys_user_perm").Where("user_id = ?", id).Pluck("perm", &perms).Error; err != nil {
			return nil, err
		}
		out["roleIds"] = roles
		out["permissions"] = perms
	}
	if r.Name == "role" {
		ids := []int64{}
		if err = s.db(ctx).Table("sys_role_menu").Where("role_id = ?", id).Pluck("menu_id", &ids).Error; err != nil {
			return nil, err
		}
		out["menuIds"] = ids
	}
	return out, nil
}
func (s *Service) IsSuperUser(ctx context.Context, id int64) (bool, error) {
	var count int64
	err := s.db(ctx).Raw(`SELECT COUNT(*) FROM sys_user u WHERE u.user_id = ? AND (EXISTS(SELECT 1 FROM sys_user_role ur JOIN sys_role r ON r.role_id=ur.role_id WHERE ur.user_id=u.user_id AND r.role_key='admin' AND r.del_flag='0') OR EXISTS(SELECT 1 FROM sys_user_perm p WHERE p.user_id=u.user_id AND p.perm='*:*:*') OR EXISTS(SELECT 1 FROM sys_user_role ur JOIN sys_role r ON r.role_id=ur.role_id JOIN sys_role_menu rm ON rm.role_id=r.role_id JOIN sys_menu m ON m.menu_id=rm.menu_id WHERE ur.user_id=u.user_id AND r.status='0' AND r.del_flag='0' AND m.status='0' AND m.del_flag='0' AND m.perms='*:*:*'))`, id).Scan(&count).Error
	return count > 0, err
}
func (s *Service) protect(ctx context.Context, r Resource, row, patch map[string]any, op string, p Principal) error {
	if r.Name == "user" {
		super, err := s.IsSuperUser(ctx, ID(row, r.ID))
		if err != nil {
			return err
		}
		if super && (op == "delete" || patch["status"] == "1") {
			return errcode.Forbiddenf("超管账号不可删除或停用")
		}
		if super && !p.Super {
			return errcode.Forbiddenf("只有超管可以修改超管账号")
		}
	}
	if r.Name == "role" && row["role_key"] == "admin" {
		if op == "delete" || patch["status"] == "1" || (patch["role_key"] != nil && patch["role_key"] != "admin") {
			return errcode.Forbiddenf("内置超管角色不可删除、停用或改标识")
		}
		if !p.Super {
			return errcode.Forbiddenf("只有超管可以修改超管角色")
		}
	}
	if r.Name == "config" && IsCredentialConfigKey(fmt.Sprint(row["config_key"])) && patch["config_key"] != nil && patch["config_key"] != row["config_key"] {
		return errcode.Forbiddenf("凭据参数不能通过改名改变其安全分类")
	}
	if r.Name == "config" && row["config_type"] == "Y" && (op == "delete" || patch["config_type"] == "N" || patch["config_key"] != nil && patch["config_key"] != row["config_key"]) {
		return errcode.Forbiddenf("内置系统参数不可删除或改标识")
	}
	if r.Name == "menu" && op == "delete" {
		var n int64
		if err := s.db(ctx).Table("sys_menu").Where("parent_id = ? AND del_flag='0'", ID(row, r.ID)).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return errcode.Conflictf("请先删除子菜单")
		}
	}
	if r.Name == "dictType" && (op == "delete" || patch["dict_type"] != nil && patch["dict_type"] != row["dict_type"]) {
		var n int64
		if err := s.db(ctx).Table("sys_dict_data").Where("dict_type = ? AND del_flag='0'", row["dict_type"]).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return errcode.Conflictf("请先清理字典数据")
		}
	}
	return nil
}

var userNameRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@-]{1,29}$`)

func (s *Service) validate(ctx context.Context, r Resource, row, patch map[string]any, creating bool, p Principal) error {
	merged := map[string]any{}
	for k, v := range row {
		merged[k] = v
	}
	for k, v := range patch {
		merged[k] = v
	}
	for _, f := range r.Required {
		if strings.TrimSpace(fmt.Sprint(merged[f])) == "" || merged[f] == nil {
			return errcode.BadRequestf("%s 必填", Camel(f))
		}
	}
	for f, max := range r.Lengths {
		if v, ok := patch[f]; ok && utf8.RuneCountInString(fmt.Sprint(v)) > max {
			return errcode.BadRequestf("%s 最多 %d 字", Camel(f), max)
		}
	}
	if v, ok := patch["remark"]; ok && utf8.RuneCountInString(fmt.Sprint(v)) > 500 {
		return errcode.BadRequestf("备注最多 500 字")
	}
	for _, f := range []string{"status", "sex", "is_frame", "is_cache", "visible", "is_default", "config_type", "notice_type"} {
		if v, ok := patch[f]; ok {
			allowed := map[string]string{"status": "01", "sex": "012", "is_frame": "01", "is_cache": "01", "visible": "01", "is_default": "YN", "config_type": "YN", "notice_type": "12"}[f]
			if len(fmt.Sprint(v)) != 1 || !strings.Contains(allowed, fmt.Sprint(v)) {
				return errcode.BadRequestf("%s 不合法", Camel(f))
			}
		}
	}
	if r.Name == "user" {
		if v, ok := patch["user_name"]; ok && !userNameRE.MatchString(fmt.Sprint(v)) {
			return errcode.BadRequestf("用户名需为 2–30 位字母、数字或 _.@-")
		}
		if v, ok := patch["email"]; ok && v != "" {
			if _, err := mail.ParseAddress(fmt.Sprint(v)); err != nil {
				return errcode.BadRequestf("邮箱格式不正确")
			}
		}
	}
	if r.Name == "role" && patch["role_key"] == "admin" && !p.Super {
		return errcode.Forbiddenf("只有超管可以设置超管角色")
	}
	if r.Name == "menu" {
		typ := fmt.Sprint(merged["menu_type"])
		if !slices.Contains([]string{"M", "C", "F"}, typ) {
			return errcode.BadRequestf("菜单类型不合法")
		}
		if typ == "C" && strings.TrimSpace(fmt.Sprint(merged["perms"])) == "" {
			return errcode.BadRequestf("页面菜单必须声明权限")
		}
		if merged["perms"] == "*:*:*" && !p.Super {
			return errcode.Forbiddenf("只有超管可以设置通配权限")
		}
		if merged["is_frame"] == "0" {
			raw := fmt.Sprint(merged["path"])
			u, e := url.Parse(raw)
			valid := e == nil && strings.TrimSpace(raw) == raw && u != nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) && u.Hostname() != "" && u.User == nil && u.Opaque == ""
			for _, r := range raw {
				if unicode.IsControl(r) {
					valid = false
				}
			}
			if !valid {
				return errcode.BadRequestf("外链必须是无用户信息的 http 或 https 地址")
			}
		}
		parent := ID(merged, "parent_id")
		id := ID(row, r.ID)
		seen := map[int64]bool{id: true}
		for parent > 0 {
			if seen[parent] {
				return errcode.BadRequestf("菜单不能成为自身的后代")
			}
			seen[parent] = true
			pr, err := s.row(ctx, r, parent, false)
			if err != nil {
				return err
			}
			if pr["menu_type"] == "F" {
				return errcode.BadRequestf("按钮不能包含子菜单")
			}
			parent = ID(pr, "parent_id")
		}
	}
	if r.Name == "dictData" {
		var n int64
		if err := s.db(ctx).Table("sys_dict_type").Where("dict_type = ? AND del_flag='0'", merged["dict_type"]).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return errcode.BadRequestf("字典类型不存在")
		}
	}
	if r.Name == "config" {
		key := fmt.Sprint(merged["config_key"])
		if key == "sys.user.initPassword" {
			value := fmt.Sprint(merged["config_value"])
			if len(value) < 6 || len(value) > 72 {
				return errcode.BadRequestf("初始密码为 6–72 字节")
			}
		}
		if key == "sys.account.captchaEnabled" || key == "sys.account.registerUser" {
			if _, err := strconv.ParseBool(fmt.Sprint(merged["config_value"])); err != nil {
				return errcode.BadRequestf("账号开关参数必须为 true 或 false")
			}
		}
	}
	_ = creating
	return nil
}
func (s *Service) Plan(ctx context.Context, r Resource, in Input, op string, p Principal) ([]Change, error) {
	data := Data(in)
	patch := map[string]any{}
	for _, f := range r.Fields {
		if v, ok := data[f]; ok {
			patch[f] = v
		}
	}
	if op == "create" {
		secret := r.Name == "config" && IsCredentialConfigKey(fmt.Sprint(patch["config_key"]))
		if secret && p.Source != "http" {
			return nil, errcode.Forbiddenf("凭据参数只能在专用页面管理")
		}
		if secret && patch["config_value"] == "***" {
			return nil, errcode.BadRequestf("请填写新的凭据值")
		}
		if err := s.validate(ctx, r, nil, patch, true, p); err != nil {
			return nil, err
		}
		change := Change{Op: "create", Label: fmt.Sprint(patch[r.Label]), After: patch, SecretConfig: secret, ConfigKey: fmt.Sprint(patch["config_key"])}
		if r.Name == "user" {
			password := ""
			if in.Password != nil {
				password = *in.Password
			} else {
				var err error
				password, err = s.ConfigValue(ctx, "sys.user.initPassword")
				if err != nil {
					return nil, errcode.BadRequestf("未设置 sys.user.initPassword，请明确提供初始密码")
				}
			}
			if len(password) < 6 || len(password) > 72 {
				return nil, errcode.BadRequestf("密码为 6–72 字节")
			}
			change.Password = password
		}
		return []Change{change}, nil
	}
	ids := in.IDs
	if op != "delete" {
		ids = []int64{InputID(r, in)}
	}
	if len(ids) == 0 || len(ids) > 200 {
		return nil, errcode.BadRequestf("请选择 1–200 条记录")
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	out := []Change{}
	for _, id := range ids {
		row, err := s.row(ctx, r, id, true)
		if err != nil {
			return nil, err
		}
		secret := r.Name == "config" && (IsCredentialConfigKey(fmt.Sprint(row["config_key"])) || IsCredentialConfigKey(fmt.Sprint(patch["config_key"])))
		if secret && p.Source != "http" {
			return nil, errcode.Forbiddenf("凭据参数只能在专用页面管理")
		}
		if secret && (patch["config_value"] == "***" || patch["config_value"] == "") {
			delete(patch, "config_value")
		}
		if err = s.protect(ctx, r, row, patch, op, p); err != nil {
			return nil, err
		}
		if op == "delete" {
			out = append(out, Change{ID: id, Label: fmt.Sprint(row[r.Label]), Op: op, Before: map[string]any{"del_flag": "0"}, After: map[string]any{"del_flag": "2"}, SecretConfig: secret, ConfigKey: fmt.Sprint(row["config_key"])})
			continue
		}
		if err = s.validate(ctx, r, row, patch, false, p); err != nil {
			return nil, err
		}
		before, after := map[string]any{}, map[string]any{}
		for f, v := range patch {
			if fmt.Sprint(row[f]) != fmt.Sprint(v) {
				before[f] = row[f]
				after[f] = v
			}
		}
		out = append(out, Change{ID: id, Label: fmt.Sprint(row[r.Label]), Op: "update", Before: before, After: after, SecretConfig: secret, ConfigKey: fmt.Sprint(row["config_key"])})
	}
	return out, nil
}
func (s *Service) Apply(ctx context.Context, r Resource, changes []Change, p Principal) ([]Change, error) {
	d := s.db(ctx)
	for i, ch := range changes {
		row := map[string]any{}
		for k, v := range ch.After {
			row[k] = v
		}
		row["update_time"] = time.Now()
		row["update_by"] = p.Name
		if ch.Op == "create" {
			row["create_time"] = time.Now()
			row["create_by"] = p.Name
			if r.Name == "user" {
				hash, err := bcrypt.GenerateFromPassword([]byte(ch.Password), bcrypt.DefaultCost)
				if err != nil {
					return nil, err
				}
				row["password"] = string(hash)
			}
			if err := d.Table(r.Table).Clauses(clause.Returning{Columns: []clause.Column{{Name: r.ID}}}).Create(&row).Error; err != nil {
				return nil, dbError(err)
			}
			changes[i].ID = ID(row, r.ID)
			saved, err := s.row(ctx, r, changes[i].ID, false)
			if err != nil {
				return nil, err
			}
			safe := map[string]any{}
			for _, f := range r.Fields {
				safe[f] = saved[f]
			}
			changes[i].After = safe
		} else if len(ch.After) > 0 {
			res := d.Table(r.Table).Where(r.ID+" = ? AND del_flag='0'", ch.ID).Updates(row)
			if res.Error != nil {
				return nil, dbError(res.Error)
			}
			if res.RowsAffected == 0 {
				return nil, errcode.Conflictf("记录已变化，请刷新")
			}
		}
	}
	if r.Auth {
		dbx.AfterCommit(ctx, func() { _ = s.Invalidate(context.Background(), r.Table, "") })
	}
	if r.Name == "dictType" || r.Name == "dictData" {
		dbx.AfterCommit(ctx, func() { _ = s.ClearCache(context.Background(), "dict") })
	}
	if r.Name == "config" {
		dbx.AfterCommit(ctx, func() { _ = s.ClearCache(context.Background(), "config") })
	}
	return changes, nil
}

// Invalidate drops permission snapshots after authorization changes commit.
// SCAN keeps this bounded per round and never blocks Redis with KEYS.
func (s *Service) Invalidate(ctx context.Context, entity, id string) error {
	switch entity {
	case "sys_dict_type", "sys_dict_data":
		return s.ClearCache(ctx, "dict")
	case "sys_config":
		return s.ClearCache(ctx, "config")
	}
	if !strings.HasPrefix(entity, "sys_user") && !strings.HasPrefix(entity, "sys_role") && entity != "sys_menu" {
		return nil
	}
	return s.clearPattern(ctx, s.Cache.Key("user_perms", "*"))
}
func (s *Service) clearPattern(ctx context.Context, pattern string) error {
	var cursor uint64
	for {
		keys, next, err := s.Cache.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err = s.Cache.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}
func (s *Service) ClearCache(ctx context.Context, kind string) error {
	if kind == "dict" && s.OnDictionaryChange != nil {
		s.OnDictionaryChange()
	}
	return s.clearPattern(ctx, s.Cache.Key(kind, "*"))
}
func (s *Service) Dictionary(ctx context.Context, typ string) ([]map[string]any, error) {
	key := ""
	if s.Cache != nil {
		key = s.Cache.Key("dict", typ)
	}
	cacheable := s.cacheable(ctx)
	rows := []map[string]any{}
	if cacheable {
		if raw, err := s.Cache.Get(ctx, key).Bytes(); err == nil && json.Unmarshal(raw, &rows) == nil {
			return rows, nil
		}
	}
	err := s.db(ctx).Table("sys_dict_data AS d").Select("d.*").Joins("JOIN sys_dict_type t ON t.dict_type=d.dict_type").Where("d.dict_type=? AND d.status='0' AND d.del_flag='0' AND t.status='0' AND t.del_flag='0'", typ).Order("d.dict_sort ASC,d.dict_code ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i] = View(rows[i])
	}
	if cacheable {
		if raw, err := json.Marshal(rows); err == nil {
			_ = s.Cache.Set(ctx, key, raw, 30*time.Minute).Err()
		}
	}
	return rows, nil
}
func (s *Service) cacheable(ctx context.Context) bool {
	if s.Cache == nil || dbx.IsReadOnly(ctx) {
		return false
	}
	_, transaction := s.db(ctx).Statement.ConnPool.(gorm.TxCommitter)
	return !transaction
}
func (s *Service) ConfigValue(ctx context.Context, key string) (string, error) {
	cacheKey := ""
	if s.Cache != nil {
		cacheKey = s.Cache.Key("config", key)
	}
	cacheable := s.cacheable(ctx)
	if cacheable {
		if v, err := s.Cache.Get(ctx, cacheKey).Result(); err == nil {
			return v, nil
		}
	}
	var v string
	tx := s.db(ctx).Table("sys_config").Select("config_value").Where("config_key=? AND status='0' AND del_flag='0'", key).Scan(&v)
	if tx.Error != nil {
		return "", tx.Error
	}
	if tx.RowsAffected == 0 {
		return "", errcode.NotFoundf("参数不存在")
	}
	if cacheable {
		_ = s.Cache.Set(ctx, cacheKey, v, 30*time.Minute).Err()
	}
	return v, nil
}

// CanUndoCreate prevents a later grant from being silently cascaded away when
// undoing an older create. The later change must be undone first.
func (s *Service) CanUndoCreate(ctx context.Context, r Resource, id int64) error {
	checks := [][3]string{}
	switch r.Name {
	case "user":
		checks = append(checks, [3]string{"sys_user_role", "user_id", ""}, [3]string{"sys_user_perm", "user_id", ""}, [3]string{"sys_access_token", "user_id", ""})
	case "role":
		checks = append(checks, [3]string{"sys_user_role", "role_id", ""})
	case "menu":
		checks = append(checks, [3]string{"sys_role_menu", "menu_id", ""})
	}
	for _, check := range checks {
		var count int64
		if err := s.db(ctx).Table(check[0]).Where(check[1]+" = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errcode.Conflictf("记录已被后续授权或令牌使用，请先撤销后续变更")
		}
	}
	return nil
}

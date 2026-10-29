package service

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

// Identity provides account queries without depending on an HTTP or AI entry
// point. Authentication and permission caching live above this database layer.
type Identity struct{ db *gorm.DB }

func NewIdentity(db *gorm.DB) *Identity { return &Identity{db: db} }

// FindUser excludes soft-deleted accounts. The authentication layer separately
// checks Status so disabled accounts receive the same credential error.
func (s *Identity) FindUser(ctx context.Context, name string) (*model.User, error) {
	var user model.User
	err := s.db.WithContext(ctx).Where("user_name = ? AND del_flag = ?", name, model.DelFlagAlive).First(&user).Error
	return &user, err
}

func (s *Identity) User(ctx context.Context, id int64) (*model.User, error) {
	var user model.User
	err := s.db.WithContext(ctx).Where("user_id = ? AND del_flag = ?", id, model.DelFlagAlive).First(&user).Error
	return &user, err
}

// RolesAndPermissions merges active role/menu grants with per-user grants.
// Disabled roles and menus cannot continue to contribute role permissions.
func (s *Identity) RolesAndPermissions(ctx context.Context, id int64) ([]string, []string, error) {
	roles := []string{}
	perms := []string{}
	if err := s.db.WithContext(ctx).Table("sys_role AS r").
		Joins("JOIN sys_user_role AS ur ON ur.role_id = r.role_id").
		Where("ur.user_id = ? AND r.status = ? AND r.del_flag = ?", id, model.StatusNormal, model.DelFlagAlive).
		Distinct().Pluck("r.role_key", &roles).Error; err != nil {
		return nil, nil, err
	}
	slices.Sort(roles)
	roles = slices.Compact(roles)
	if slices.Contains(roles, "admin") {
		return roles, []string{"*:*:*"}, nil
	}

	if err := s.db.WithContext(ctx).Raw(`
		SELECT m.perms AS perm
		FROM sys_menu m
		JOIN sys_role_menu rm ON rm.menu_id = m.menu_id
		JOIN sys_role r ON r.role_id = rm.role_id
		JOIN sys_user_role ur ON ur.role_id = r.role_id
		WHERE ur.user_id = ? AND r.status = '0' AND r.del_flag = '0'
		  AND m.status = '0' AND m.del_flag = '0' AND m.perms <> ''
		UNION
		SELECT perm FROM sys_user_perm WHERE user_id = ? AND perm <> ''`, id, id).
		Scan(&perms).Error; err != nil {
		return nil, nil, err
	}
	slices.Sort(perms)
	return roles, slices.Compact(perms), nil
}

// Menus includes hidden routes; visibility only controls sidebar presentation.
func (s *Identity) Menus(ctx context.Context) ([]model.Menu, error) {
	menus := []model.Menu{}
	err := s.db.WithContext(ctx).
		Where("status = ? AND del_flag = ?", model.StatusNormal, model.DelFlagAlive).
		Order("parent_id ASC, order_num ASC, menu_id ASC").Find(&menus).Error
	return menus, err
}

// ConfigBool falls back only for an absent setting. Invalid values and database
// failures are errors, so a broken switch cannot accidentally enable signup.
func (s *Identity) ConfigBool(ctx context.Context, key string, fallback bool) (bool, error) {
	var row struct {
		ConfigValue string `gorm:"column:config_value"`
	}
	tx := s.db.WithContext(ctx).Table("sys_config").
		Where("config_key = ? AND status = ? AND del_flag = ?", key, model.StatusNormal, model.DelFlagAlive).
		Take(&row)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return fallback, nil
	}
	if tx.Error != nil {
		return false, tx.Error
	}
	value, err := strconv.ParseBool(strings.TrimSpace(row.ConfigValue))
	if err != nil {
		return false, errcode.Internalf("系统参数 %s 不是有效开关", key).Wrap(err)
	}
	return value, nil
}

// Register starts new accounts without privileged roles or implicit grants.
func (s *Identity) Register(ctx context.Context, name, nickname, passwordHash string) (*model.User, error) {
	now := time.Now()
	user := &model.User{
		UserName: name, NickName: nickname, Password: passwordHash,
		UserType: "00", Sex: "2", Status: model.StatusNormal, DelFlag: model.DelFlagAlive,
		IdentityAudit: model.IdentityAudit{CreateBy: name, UpdateBy: name, CreateTime: now, UpdateTime: now},
	}
	if err := s.db.WithContext(ctx).Create(user).Error; err != nil {
		var pgErr *pgconn.PgError
		if errors.Is(err, gorm.ErrDuplicatedKey) || (errors.As(err, &pgErr) && pgErr.Code == "23505") {
			return nil, errcode.Conflictf("用户名已存在")
		}
		return nil, err
	}
	return user, nil
}

// RecordLogin stores only a bounded user-agent summary; credentials never enter
// login logs. Keeping this synchronous lets callers observe persistence errors.
func (s *Identity) RecordLogin(ctx context.Context, name, ip, userAgent, status, msg string) error {
	osName := ""
	for _, candidate := range []string{"Windows", "Android", "iPhone", "iPad", "Mac OS", "Linux"} {
		if strings.Contains(userAgent, candidate) {
			osName = candidate
			break
		}
	}
	return s.db.WithContext(ctx).Table("sys_login_log").Create(map[string]any{
		"user_name": identityTruncate(name, 50), "ipaddr": identityTruncate(ip, 128),
		"login_location": "", "browser": identityTruncate(userAgent, 64), "os": osName,
		"status": status, "msg": identityTruncate(msg, 255), "login_time": time.Now(),
	}).Error
}

func (s *Identity) UpdateLogin(ctx context.Context, id int64, ip string) error {
	return s.db.WithContext(ctx).Model(&model.User{}).
		Where("user_id = ? AND del_flag = ?", id, model.DelFlagAlive).
		Updates(map[string]any{"login_ip": identityTruncate(ip, 128), "login_date": time.Now()}).Error
}

func identityTruncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max])
	}
	return value
}

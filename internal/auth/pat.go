package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type accessToken struct {
	TokenID    int64      `gorm:"column:token_id;primaryKey;autoIncrement" json:"tokenId"`
	UserID     int64      `gorm:"column:user_id" json:"userId"`
	Name       string     `gorm:"column:name" json:"name"`
	TokenHash  string     `gorm:"column:token_hash" json:"-"`
	Prefix     string     `gorm:"column:prefix" json:"prefix"`
	Scopes     string     `gorm:"column:scopes" json:"scopes"`
	MaxRisk    *string    `gorm:"column:max_risk" json:"maxRisk,omitempty"`
	ExpiresAt  *time.Time `gorm:"column:expires_at" json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `gorm:"column:last_used_at" json:"lastUsedAt,omitempty"`
	Status     string     `gorm:"column:status" json:"status"`
	DelFlag    string     `gorm:"column:del_flag" json:"-"`
	CreateBy   string     `gorm:"column:create_by" json:"createBy"`
	CreateTime time.Time  `gorm:"column:create_time" json:"createTime"`
}

func (accessToken) TableName() string { return "sys_access_token" }
func tokenDigest(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// AuthenticatePAT reloads the token and live account/permissions on every call,
// so revocation and role changes affect existing MCP sessions immediately.
func (s *Service) AuthenticatePAT(ctx context.Context, plain string) (*capability.Actor, error) {
	if !strings.HasPrefix(plain, "iadm_") || len(plain) < 32 || len(plain) > 128 {
		return nil, errcode.Unauthorizedf("访问令牌无效")
	}
	var tok accessToken
	err := dbx.DBFrom(ctx, s.db).Where("token_hash=? AND status='0' AND del_flag='0'", tokenDigest(plain)).First(&tok).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errcode.Unauthorizedf("访问令牌无效或已吊销")
	}
	if err != nil {
		return nil, err
	}
	if tok.ExpiresAt != nil && !tok.ExpiresAt.After(s.now()) {
		return nil, errcode.Unauthorizedf("访问令牌已过期")
	}
	user, err := s.identity.User(ctx, tok.UserID)
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && user.Status != "0" {
		return nil, errcode.Unauthorizedf("令牌用户已停用")
	}
	if err != nil {
		return nil, err
	}
	_, perms, err := s.identity.RolesAndPermissions(ctx, user.UserID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(tok.Scopes) != "" {
		scoped := []string{}
		super := slices.Contains(perms, capability.SuperPerm)
		for _, scope := range strings.Split(tok.Scopes, ",") {
			scope = strings.TrimSpace(scope)
			if scope != "" && (super || slices.Contains(perms, scope)) {
				scoped = append(scoped, scope)
			}
		}
		perms = scoped
	}
	risk := s.effectiveRisk(user)
	mcp, err := capability.ParseRisk(s.cfg.MCP.MaxRisk)
	if err != nil {
		return nil, errcode.Internalf("MCP 风险配置无效")
	}
	if mcp.Level() < risk.Level() {
		risk = mcp
	}
	if tok.MaxRisk != nil {
		tr, e := capability.ParseRisk(*tok.MaxRisk)
		if e != nil {
			return nil, errcode.Unauthorizedf("令牌风险上限无效")
		}
		if tr.Level() < risk.Level() {
			risk = tr
		}
	}
	// A conditional write throttles both cross-process and concurrent requests.
	now := s.now()
	_ = dbx.DBFrom(ctx, s.db).Model(&accessToken{}).Where("token_id=? AND (last_used_at IS NULL OR last_used_at < ?)", tok.TokenID, now.Add(-time.Minute)).Update("last_used_at", now).Error
	return &capability.Actor{Type: capability.ActorMCPClient, Source: capability.SourceMCP, UserID: user.UserID, UserName: user.UserName, Perms: perms, MaxRisk: risk, TokenID: tok.TokenID, ApprovalMode: "confirmed"}, nil
}
func (s *Service) ValidatePAT(ctx context.Context, name string, scopes []string, maxRisk string, expires *time.Time) error {
	actor, ok := capability.ActorFrom(ctx)
	if !ok {
		return errcode.Unauthorizedf("请先登录")
	}
	if actor.Source != capability.SourceHTTP {
		return errcode.Forbiddenf("只能在页面管理访问令牌")
	}
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 64 {
		return errcode.BadRequestf("令牌名称为 1–64 字")
	}
	if len(scopes) > 2000 {
		return errcode.BadRequestf("权限范围过多")
	}
	_, perms, err := s.identity.RolesAndPermissions(ctx, actor.UserID)
	if err != nil {
		return err
	}
	super := slices.Contains(perms, capability.SuperPerm)
	for _, scope := range scopes {
		if scope == "" || strings.Contains(scope, ",") || len(scope) > 100 {
			return errcode.BadRequestf("权限范围不合法")
		}
		if !super && !slices.Contains(perms, scope) {
			return errcode.Forbiddenf("不能授予当前账号没有的权限 %s", scope)
		}
	}
	if maxRisk != "" {
		risk, e := capability.ParseRisk(maxRisk)
		if e != nil {
			return errcode.BadRequestf("风险上限无效")
		}
		user, e := s.identity.User(ctx, actor.UserID)
		if e != nil {
			return e
		}
		if risk.Level() > s.effectiveRisk(user).Level() {
			return errcode.BadRequestf("令牌风险上限不能超过用户上限")
		}
	}
	if expires != nil && !expires.After(s.now()) {
		return errcode.BadRequestf("过期时间必须晚于当前时间")
	}
	return nil
}
func (s *Service) CreatePAT(ctx context.Context, name string, scopes []string, maxRisk string, expires *time.Time) (map[string]any, error) {
	if err := s.ValidatePAT(ctx, name, scopes, maxRisk, expires); err != nil {
		return nil, err
	}
	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		return nil, err
	}
	// Preserve all 256 random bits while representing the bearer secret in base62.
	encoded := new(big.Int).SetBytes(entropy).Text(62)
	plain := "iadm_" + strings.Repeat("0", 43-len(encoded)) + encoded
	actor, _ := capability.ActorFrom(ctx)
	scopes = slices.Clone(scopes)
	slices.Sort(scopes)
	scopes = slices.Compact(scopes)
	tok := accessToken{UserID: actor.UserID, Name: strings.TrimSpace(name), TokenHash: tokenDigest(plain), Prefix: plain[:12], Scopes: strings.Join(scopes, ","), ExpiresAt: expires, Status: "0", DelFlag: "0", CreateBy: actor.UserName, CreateTime: s.now()}
	if maxRisk != "" {
		tok.MaxRisk = &maxRisk
	}
	if err := dbx.DBFrom(ctx, s.db).Create(&tok).Error; err != nil {
		return nil, err
	}
	capability.RecorderFrom(ctx).Create("sys_access_token", capability.ID(tok.TokenID), tok.Name, map[string]any{"prefix": tok.Prefix, "name": tok.Name, "scopes": tok.Scopes, "max_risk": tok.MaxRisk})
	return map[string]any{"token": plain, "tokenId": tok.TokenID, "prefix": tok.Prefix}, nil
}
func (s *Service) RevokePAT(ctx context.Context, ids []int64, apply bool) error {
	actor, ok := capability.ActorFrom(ctx)
	if !ok {
		return errcode.Unauthorizedf("请先登录")
	}
	if len(ids) == 0 || len(ids) > 200 {
		return errcode.BadRequestf("请选择 1–200 个令牌")
	}
	rows := []accessToken{}
	if err := dbx.DBFrom(ctx, s.db).Where("token_id IN ? AND user_id=? AND del_flag='0'", ids, actor.UserID).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) != len(slices.Compact(slices.Sorted(slices.Values(ids)))) {
		return errcode.NotFoundf("令牌不存在或不属于当前账号")
	}
	if !apply {
		return nil
	}
	if err := dbx.DBFrom(ctx, s.db).Model(&accessToken{}).Where("token_id IN ? AND user_id=?", ids, actor.UserID).Updates(map[string]any{"status": "1", "del_flag": "2", "update_time": s.now(), "update_by": actor.UserName}).Error; err != nil {
		return err
	}
	for _, tok := range rows {
		capability.RecorderFrom(ctx).Delete("sys_access_token", fmt.Sprint(tok.TokenID), tok.Name, map[string]any{"prefix": tok.Prefix, "name": tok.Name})
	}
	return nil
}

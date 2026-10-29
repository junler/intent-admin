package auth

import (
	"context"
	"errors"
	"slices"

	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

// CurrentMCPActor resolves a delegated subject against live account grants.
// A nil scope requests current grants for consent; an empty scope grants none.
func (s *Service) CurrentMCPActor(ctx context.Context, userID int64, scopes []string, maxRisk capability.Risk) (*capability.Actor, error) {
	u, err := s.identity.User(ctx, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && u.Status != "0" {
		return nil, errcode.Unauthorizedf("授权用户已停用或不存在")
	}
	if err != nil {
		return nil, err
	}
	_, perms, err := s.identity.RolesAndPermissions(ctx, userID)
	if err != nil {
		return nil, err
	}
	if scopes != nil {
		effective := []string{}
		for _, scope := range scopes {
			if scope != capability.SuperPerm && (slices.Contains(perms, capability.SuperPerm) || slices.Contains(perms, scope)) {
				effective = append(effective, scope)
			}
		}
		perms = effective
	}
	risk := s.effectiveRisk(u)
	mcp, err := capability.ParseRisk(s.cfg.MCP.MaxRisk)
	if err != nil {
		return nil, errcode.Internalf("MCP 风险配置无效")
	}
	for _, ceiling := range []capability.Risk{mcp, maxRisk} {
		if !ceiling.Valid() {
			return nil, errcode.Unauthorizedf("授权风险上限无效")
		}
		if ceiling.Level() < risk.Level() {
			risk = ceiling
		}
	}
	return &capability.Actor{Type: capability.ActorMCPClient, Source: capability.SourceMCP, UserID: u.UserID, UserName: u.UserName, Perms: perms, MaxRisk: risk, ApprovalMode: "confirmed"}, nil
}

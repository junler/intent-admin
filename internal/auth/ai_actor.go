package auth

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/service"
)

// CurrentAIActor reloads an active account, effective grants and AI risk ceiling.
// It deliberately bypasses Redis and joins the caller's authorization locks.
func (s *Service) CurrentAIActor(ctx context.Context, userID int64) (*capability.Actor, error) {
	identity := service.NewIdentity(dbx.DBFrom(ctx, s.db))
	u, err := identity.User(ctx, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && u.Status != model.StatusNormal) {
		return nil, errcode.Forbiddenf("审批参与人已停用或不存在")
	}
	if err != nil {
		return nil, err
	}
	_, perms, err := identity.RolesAndPermissions(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &capability.Actor{UserID: u.UserID, UserName: u.UserName, Type: capability.ActorHuman, Source: capability.SourceHTTP, Perms: perms, MaxRisk: s.effectiveRisk(u), ApprovalMode: "manual"}, nil
}

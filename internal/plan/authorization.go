package plan

import (
	"context"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

func (s *Service) liveActor(ctx context.Context, id int64) (*capability.Actor, error) {
	if s.ActorLoader == nil {
		return nil, errcode.Forbiddenf("尚未配置实时权限校验")
	}
	a, err := s.ActorLoader(ctx, id)
	if err != nil {
		return nil, err
	}
	if a == nil || a.UserID != id || id <= 0 || !a.Can("ai:agent:use") {
		return nil, errcode.Forbiddenf("没有使用 AI 计划的权限")
	}
	copy := *a
	copy.Type = capability.ActorHuman
	copy.Source = capability.SourceHTTP
	copy.TokenID = 0
	if !copy.MaxRisk.Valid() {
		return nil, errcode.New(errcode.RiskExceeded, "用户的 AI 风险上限无效")
	}
	if copy.MaxRisk.Level() > s.opts.MaxRisk.Level() {
		copy.MaxRisk = s.opts.MaxRisk
	}
	return &copy, nil
}

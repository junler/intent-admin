package system

import (
	"context"

	"github.com/junler/intent-admin/internal/auth"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	svc "github.com/junler/intent-admin/internal/service/system"
)

func registerTokens(s *Service) {
	a := auth.New(s.DB, s.Cache, s.Cfg)
	r := svc.Resource{Name: "token", Table: "sys_access_token", ID: "token_id", Label: "name", Route: "/system/token", Fields: []string{"name", "status", "user_id"}}
	list := meta("system.token.list", "我的访问令牌", "", "GET", r.Route+"/list", capability.RiskLow, false, false)
	list.AIEnabled = false
	list.MCPEnabled = false
	capability.Register(capability.Capability[svc.Input, svc.ListOut]{Meta: list, Fn: func(ctx context.Context, in svc.Input) (svc.ListOut, error) {
		id := principal(ctx).UserID
		in.UserID = &id
		return s.List(ctx, r, in)
	}})
	create := meta("system.token.create", "创建个人访问令牌", "", "POST", r.Route, capability.RiskMedium, true, false)
	create.AIEnabled = false
	create.MCPEnabled = false
	capability.Register(capability.Capability[svc.Input, map[string]any]{Meta: create,
		Fn: func(ctx context.Context, in svc.Input) (map[string]any, error) {
			name, scopes, risk, err := tokenArgs(in)
			if err != nil {
				return nil, err
			}
			return a.CreatePAT(ctx, name, scopes, risk, in.ExpiresAt)
		},
		DryRun: func(ctx context.Context, in svc.Input) (*capability.Preview, error) {
			name, scopes, risk, err := tokenArgs(in)
			if err != nil {
				return nil, err
			}
			if err = a.ValidatePAT(ctx, name, scopes, risk, in.ExpiresAt); err != nil {
				return nil, err
			}
			p := preview(r.Table, "/profile", []svc.Change{{Label: name, Op: "create", After: map[string]any{"name": name, "scopes": scopes, "maxRisk": risk, "expiresAt": in.ExpiresAt}}})
			p.Warn("令牌明文仅在创建成功时显示一次，请妥善保存")
			return p, nil
		},
	})
	revoke := meta("system.token.revoke", "吊销个人访问令牌", "", "DELETE", r.Route+"/:ids", capability.RiskMedium, true, false)
	revoke.AIEnabled = false
	revoke.MCPEnabled = false
	capability.Register(capability.Capability[svc.Input, capability.Empty]{Meta: revoke,
		Fn: func(ctx context.Context, in svc.Input) (capability.Empty, error) {
			return capability.Empty{}, a.RevokePAT(ctx, in.IDs, true)
		},
		DryRun: func(ctx context.Context, in svc.Input) (*capability.Preview, error) {
			if err := a.RevokePAT(ctx, in.IDs, false); err != nil {
				return nil, err
			}
			p := &capability.Preview{Summary: "吊销访问令牌，使所有关联 MCP 连接立即失效", Affected: len(in.IDs)}
			p.Warn("令牌吊销不可回滚")
			return p, nil
		},
	})
}
func tokenArgs(in svc.Input) (string, []string, string, error) {
	if in.Name == nil {
		return "", nil, "", errcode.BadRequestf("name 必填")
	}
	scopes := []string{}
	if in.Scopes != nil {
		scopes = *in.Scopes
	}
	risk := ""
	if in.MaxRisk != nil {
		risk = *in.MaxRisk
	}
	return *in.Name, scopes, risk, nil
}

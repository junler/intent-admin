package system

import (
	"context"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	svc "github.com/junler/intent-admin/internal/service/system"
)

func registerProfile(s *Service) {
	r := svc.Spec("user")
	get := meta("system.profile.get", "个人信息", "", "GET", "/system/user/profile", capability.RiskLow, false, false)
	get.AIEnabled = false
	get.MCPEnabled = false
	capability.Register(capability.Capability[capability.Empty, map[string]any]{Meta: get, Fn: func(ctx context.Context, in capability.Empty) (map[string]any, error) {
		return s.Get(ctx, r, principal(ctx).UserID)
	}})
	limited := r
	limited.Fields = []string{"nick_name", "email", "phonenumber", "sex", "avatar"}
	update := meta("system.profile.update", "更新个人信息", "", "PUT", "/system/user/profile", capability.RiskMedium, true, true)
	update.AIEnabled = false
	update.MCPEnabled = false
	capability.Register(capability.Capability[svc.Input, capability.Empty]{Meta: update,
		Fn: func(ctx context.Context, in svc.Input) (capability.Empty, error) {
			in.ID = principal(ctx).UserID
			changes, err := s.Plan(ctx, limited, in, "update", principal(ctx))
			if err == nil {
				changes, err = s.Apply(ctx, limited, changes, principal(ctx))
				record(ctx, r.Table, changes)
			}
			return capability.Empty{}, err
		},
		DryRun: func(ctx context.Context, in svc.Input) (*capability.Preview, error) {
			in.ID = principal(ctx).UserID
			changes, err := s.Plan(ctx, limited, in, "update", principal(ctx))
			return preview(r.Table, "/profile", changes), err
		},
		Undo: undoResource(s, limited),
	})
	pwd := meta("system.profile.updatePwd", "修改个人密码", "", "PUT", "/system/user/profile/updatePwd", capability.RiskHigh, true, false)
	pwd.AIEnabled = false
	pwd.MCPEnabled = false
	capability.Register(capability.Capability[svc.Input, capability.Empty]{Meta: pwd,
		Fn: func(ctx context.Context, in svc.Input) (capability.Empty, error) {
			if in.OldPassword == nil || in.NewPassword == nil {
				return capability.Empty{}, errcode.BadRequestf("oldPassword 和 newPassword 必填")
			}
			p := principal(ctx)
			err := s.Password(ctx, p.UserID, *in.NewPassword, p, in.OldPassword)
			if err == nil {
				capability.RecorderFrom(ctx).Update("sys_user", capability.ID(p.UserID), "密码已变更", nil, map[string]any{"credentialChanged": true})
			}
			return capability.Empty{}, err
		},
		DryRun: func(ctx context.Context, in svc.Input) (*capability.Preview, error) {
			if in.OldPassword == nil || in.NewPassword == nil || len(*in.NewPassword) < 6 || len(*in.NewPassword) > 72 {
				return nil, errcode.BadRequestf("请填写原密码和 6–72 字节的新密码")
			}
			if err := s.CheckPassword(ctx, principal(ctx).UserID, *in.OldPassword); err != nil {
				return nil, err
			}
			p := &capability.Preview{Summary: "修改密码并退出全部浏览器会话", Affected: 1}
			p.Warn("密码变更不可回滚")
			return p, nil
		},
	})
}

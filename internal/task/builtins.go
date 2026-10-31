package task

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type retentionIn struct {
	Days int `json:"days" validate:"omitempty,min=1,max=3650"`
}

func (r *Runtime) registerBuiltins() {
	r.registerWorkflowHandler()
	r.registerAnnouncementHandler()
	r.InvokeHandler(NotificationHandler, "向任务创建者发送个人系统通知（铃铛），参数 title、content；recipientId 由服务端绑定，无需填写", "task.system.notifyUser", map[string]any{"title": "每分钟提醒", "content": "这是一条定时系统通知", "recipientId": 0})
	// Async export is deliberately absent from the user-selectable cron
	// allowlist. Only EnqueueExport may bind an authenticated user's identity;
	// a system job must never choose another user's ID to gain their grants.
	for _, x := range []struct{ name, summary, cap string }{{"system:cleanOperLog", "按保留天数清理操作日志", "task.system.cleanOperLog"}, {"system:cleanLoginLog", "按保留天数清理登录日志", "task.system.cleanLoginLog"}, {"system:cleanJobLog", "按保留天数清理任务日志", "task.system.cleanJobLog"}} {
		x := x
		r.RegisterHandler(Handler{Name: x.name, Summary: x.summary, ParamsProto: map[string]any{"days": 90}, Permission: retentionPermission(x.name), Fn: func(ctx context.Context, p json.RawMessage) error {
			_, err := capability.InvokeJSON(ctx, x.cap, p)
			return err
		}})
	}
	for _, x := range []struct {
		name, summary, cap string
		params             map[string]any
	}{{"system:refreshDashboard", "刷新首页统计缓存", "task.system.refreshDashboard", map[string]any{}}, {"system:refreshKnowledge", "刷新动态知识缓存", "task.system.refreshKnowledge", map[string]any{}}, {"system:cleanExportFiles", "清理过期导出文件", "task.system.cleanExportFiles", map[string]any{"days": 7}}} {
		x := x
		r.RegisterHandler(Handler{Name: x.name, Summary: x.summary, Permission: builtinPermission(x.name), ParamsProto: x.params, Fn: func(ctx context.Context, p json.RawMessage) error {
			_, err := capability.InvokeJSON(ctx, x.cap, p)
			return err
		}})
	}
}

// InvokeHandler registers a scheduler handler that forwards to a capability.
// A feature package uses this to ship its own sample or maintenance jobs so
// they live and die with that package, rather than being listed here.
func (r *Runtime) InvokeHandler(name, summary, capabilityName string, params map[string]any) {
	r.RegisterHandler(Handler{Name: name, Summary: summary, ParamsProto: params, Fn: func(ctx context.Context, p json.RawMessage) error {
		if len(p) == 0 {
			p = json.RawMessage(`{}`)
		}
		_, err := capability.InvokeJSON(ctx, capabilityName, p)
		return err
	}})
}

// RegisterCapabilities supplies auditable, bounded retention operations for
// scheduler handlers. It must be called once during feature registration.
func (r *Runtime) RegisterCapabilities() {
	r.registerNotificationCapability()
	defer r.registerServiceCapabilities()
	for _, x := range []struct{ name, title, table, id, date string }{{"cleanOperLog", "清理过期操作日志", "sys_oper_log", "oper_id", "oper_time"}, {"cleanLoginLog", "清理过期登录日志", "sys_login_log", "info_id", "login_time"}, {"cleanJobLog", "清理过期任务日志", "sys_job_log", "job_log_id", "create_time"}} {
		x := x
		selector := func(ctx context.Context, in retentionIn) ([]string, error) {
			days := in.Days
			if days <= 0 {
				days = 90
			}
			var ids []string
			err := dbx.DBFrom(ctx, r.DB).Table(x.table).Where(x.date+" < ?", time.Now().AddDate(0, 0, -days)).Order(x.date+" ASC").Limit(1000).Pluck(x.id, &ids).Error
			return ids, err
		}
		capability.Register(capability.Capability[retentionIn, capability.Empty]{Meta: capability.Meta{Name: "task.system." + x.name, Summary: x.title, Perm: retentionPermission("system:" + x.name), Risk: capability.RiskHigh, Mutating: true, Idempotent: true, AIEnabled: true, MCPEnabled: true, LogTitle: "定时清理", BizType: capability.BizDelete}, Fn: func(ctx context.Context, in retentionIn) (capability.Empty, error) {
			ids, err := selector(ctx, in)
			if err != nil {
				return capability.Empty{}, err
			}
			if len(ids) == 0 {
				return capability.Empty{}, nil
			}
			if err := dbx.DBFrom(ctx, r.DB).Table(x.table).Where(x.id+" IN ?", ids).Delete(nil).Error; err != nil {
				return capability.Empty{}, err
			}
			for _, id := range ids {
				capability.RecorderFrom(ctx).Delete(x.table, id, id, nil)
			}
			return capability.Empty{}, nil
		}, DryRun: func(ctx context.Context, in retentionIn) (*capability.Preview, error) {
			ids, err := selector(ctx, in)
			if err != nil {
				return nil, err
			}
			return &capability.Preview{Summary: fmt.Sprintf("永久清理 %d 条过期记录，每批最多 1000 条", len(ids)), Affected: len(ids), Warnings: []string{"物理删除不可撤销"}}, nil
		}})
	}
}

func (r *Runtime) registerServiceCapabilities() {
	for _, name := range []string{"refreshDashboard", "refreshKnowledge"} {
		name := name
		capability.Register(capability.Capability[capability.Empty, capability.Empty]{Meta: capability.Meta{Name: "task.system." + name, Summary: "刷新后台动态缓存：" + name, Perm: refreshPermission(name), Risk: capability.RiskLow, Mutating: true, Idempotent: true, LogTitle: "系统维护", AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, _ capability.Empty) (capability.Empty, error) {
			var err error
			if name == "refreshDashboard" {
				if r.RefreshDashboard != nil {
					err = r.RefreshDashboard(ctx)
				}
			} else if r.RefreshKnowledge != nil {
				err = r.RefreshKnowledge(ctx)
			} else {
				err = r.Cache.Del(ctx, r.Cache.Key("knowledge", "enums")).Err()
			}
			return capability.Empty{}, err
		}, DryRun: func(context.Context, capability.Empty) (*capability.Preview, error) {
			return &capability.Preview{Summary: "刷新动态缓存", Affected: 1}, nil
		}})
	}
	capability.Register(capability.Capability[retentionIn, capability.Empty]{Meta: capability.Meta{Name: "task.system.cleanExportFiles", Summary: "清理过期导出文件", Perm: "system:upload:remove", AIEnabled: true, MCPEnabled: true, Risk: capability.RiskHigh, Mutating: true, LogTitle: "系统任务"}, Fn: func(ctx context.Context, in retentionIn) (capability.Empty, error) {
		days := in.Days
		if days <= 0 {
			days = r.Cfg.Export.AsyncRetainDays
		}
		if days <= 0 {
			days = 7
		}
		if r.FileCleaner == nil {
			return capability.Empty{}, errcode.Internalf("文件清理服务不可用")
		}
		cutoff := time.Now().AddDate(0, 0, -days)
		var ids []string
		if err := dbx.DBFrom(ctx, r.DB).Table("sys_upload").Where("biz_type='export' AND del_flag='0' AND create_time<?", cutoff).Limit(1000).Pluck("upload_id", &ids).Error; err != nil {
			return capability.Empty{}, err
		}
		if err := r.FileCleaner(ctx, cutoff); err != nil {
			return capability.Empty{}, err
		}
		for _, id := range ids {
			capability.RecorderFrom(ctx).Delete("sys_upload", id, id, nil)
		}
		return capability.Empty{}, nil
	}, DryRun: func(ctx context.Context, in retentionIn) (*capability.Preview, error) {
		days := in.Days
		if days <= 0 {
			days = 7
		}
		var count int64
		err := dbx.DBFrom(ctx, r.DB).Table("sys_upload").Where("biz_type='export' AND del_flag='0' AND create_time<?", time.Now().AddDate(0, 0, -days)).Count(&count).Error
		return &capability.Preview{Summary: fmt.Sprintf("永久清理 %d 个过期导出文件", count), Affected: int(count), Warnings: []string{"文件删除不可撤销"}}, err
	}})
	capability.Register(capability.Capability[exportPayload, capability.Empty]{Meta: capability.Meta{Name: "task.system.exportAsync", Summary: "生成后台导出文件", Perm: "monitor:job:edit", Risk: capability.RiskMedium, Mutating: true, LogTitle: "异步导出", BizType: capability.BizExport}, Fn: func(ctx context.Context, in exportPayload) (capability.Empty, error) {
		return capability.Empty{}, r.export(ctx, in)
	}, DryRun: func(ctx context.Context, in exportPayload) (*capability.Preview, error) {
		e, ok := capability.Lookup(in.Capability)
		if !ok || e.Export == nil {
			return nil, errcode.NotFoundf("导出功能不存在")
		}
		return &capability.Preview{Summary: "为指定用户后台导出 " + e.Summary, Affected: 1}, nil
	}})
}

// Maintenance operations share business permissions across interactive and
// scheduled use, rather than requiring job-edit permission for direct calls.
func retentionPermission(handler string) string {
	switch handler {
	case "system:cleanOperLog":
		return "monitor:operlog:remove"
	case "system:cleanLoginLog":
		return "monitor:loginlog:remove"
	default:
		return "monitor:job:remove"
	}
}
func refreshPermission(name string) string {
	if name == "refreshDashboard" {
		return "dashboard:view"
	}
	return "monitor:cache:remove"
}

func builtinPermission(name string) string {
	switch name {
	case "system:refreshDashboard":
		return refreshPermission("refreshDashboard")
	case "system:refreshKnowledge":
		return refreshPermission("refreshKnowledge")
	default:
		return "system:upload:remove"
	}
}

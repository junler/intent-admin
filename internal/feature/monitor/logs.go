package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
	"gorm.io/gorm"
)

type LogListIn struct {
	paginate.Query
	UserName    string `json:"userName" form:"userName"`
	OperName    string `json:"operName" form:"operName"`
	JobName     string `json:"jobName" form:"jobName"`
	JobID       int64  `json:"jobId" form:"jobId"`
	Status      string `json:"status" form:"status"`
	ActorType   string `json:"actorType" form:"actorType"`
	Source      string `json:"source" form:"source"`
	Risk        string `json:"risk" form:"risk"`
	Intent      string `json:"intent" form:"intent"`
	Capability  string `json:"capability" form:"capability"`
	ChangesetID string `json:"changesetId" form:"changesetId"`
	PlanID      string `json:"planId" form:"planId"`
	BeginTime   string `json:"beginTime" form:"beginTime"`
	EndTime     string `json:"endTime" form:"endTime"`
	RolledBack  *bool  `json:"rolledBack" form:"rolledBack"`
	MinFailures int    `json:"minFailures" form:"minFailures"`
	IPAddr      string `json:"ipaddr" form:"ipaddr"`
}
type LogIDs struct {
	IDs []int64 `json:"ids" uri:"ids" collection_format:"csv" validate:"required,min=1,max=200,dive,gt=0"`
}
type logSpec struct {
	name, table, id, date, title, perm string
	cols                               []capability.ExportColumn
}

func (s *Service) logQuery(ctx context.Context, sp logSpec, in LogListIn) (*gorm.DB, error) {
	q := dbx.DBFrom(ctx, s.db).Table(sp.table)
	if in.Status != "" {
		q = q.Where("status=?", in.Status)
	}
	if in.BeginTime != "" {
		t, err := time.Parse(time.RFC3339, in.BeginTime)
		if err != nil {
			t, err = time.Parse("2006-01-02 15:04:05", in.BeginTime)
			if err != nil {
				t, err = time.Parse("2006-01-02", in.BeginTime)
				if err != nil {
					return nil, errcode.BadRequestf("开始时间格式错误")
				}
			}
		}
		q = q.Where(sp.date+" >= ?", t)
	}
	if in.EndTime != "" {
		t, err := time.Parse(time.RFC3339, in.EndTime)
		if err != nil {
			t, err = time.Parse("2006-01-02 15:04:05", in.EndTime)
			if err != nil {
				t, err = time.Parse("2006-01-02", in.EndTime)
				if err != nil {
					return nil, errcode.BadRequestf("结束时间格式错误")
				}
				t = t.AddDate(0, 0, 1).Add(-time.Nanosecond)
			}
		}
		q = q.Where(sp.date+" <= ?", t)
	}
	switch sp.name {
	case "operlog":
		if in.RolledBack != nil {
			predicate := "EXISTS (SELECT 1 FROM sys_changeset c WHERE c.changeset_id=sys_oper_log.changeset_id AND c.status='rolled_back')"
			if !*in.RolledBack {
				predicate = "NOT " + predicate
			}
			q = q.Where(predicate)
		}
		for _, f := range []struct{ col, value string }{{"oper_name", in.UserName}, {"oper_name", in.OperName}, {"intent", in.Intent}, {"capability", in.Capability}} {
			if f.value != "" {
				q = q.Where(f.col+" LIKE ?", "%"+f.value+"%")
			}
		}
		for _, f := range []struct{ col, value string }{{"actor_type", in.ActorType}, {"source", in.Source}, {"risk", in.Risk}, {"changeset_id", in.ChangesetID}, {"plan_id", in.PlanID}} {
			if f.value != "" {
				q = q.Where(f.col+" = ?", f.value)
			}
		}
	case "loginlog":
		if in.MinFailures > 0 {
			sub := dbx.DBFrom(ctx, s.db).Table("sys_login_log").Select("user_name").Where("status='1'")
			if in.BeginTime != "" {
				sub = sub.Where("login_time>=?", in.BeginTime)
			}
			if in.EndTime != "" {
				sub = sub.Where("login_time<=?", in.EndTime)
			}
			q = q.Where("user_name IN (?)", sub.Group("user_name").Having("count(*)>=?", in.MinFailures))
		}
		if in.UserName != "" {
			q = q.Where("user_name LIKE ?", "%"+in.UserName+"%")
		}
		if in.IPAddr != "" {
			q = q.Where("ipaddr LIKE ?", "%"+in.IPAddr+"%")
		}
	case "jobLog":
		if in.JobName != "" {
			q = q.Where("job_name LIKE ?", "%"+in.JobName+"%")
		}
		if in.JobID > 0 {
			q = q.Where("job_id=?", in.JobID)
		}
	}
	return q, nil
}
func (s *Service) registerLogs() {
	specs := []logSpec{{"operlog", "sys_oper_log", "oper_id", "oper_time", "操作日志", "monitor:operlog", []capability.ExportColumn{{Field: "operId", Label: "编号"}, {Field: "operName", Label: "操作人"}, {Field: "capability", Label: "功能"}, {Field: "actorType", Label: "主体", Dict: "sys_actor_type"}, {Field: "source", Label: "来源", Dict: "sys_call_source"}, {Field: "risk", Label: "风险", Dict: "sys_risk_level"}, {Field: "intent", Label: "意图"}, {Field: "status", Label: "状态"}, {Field: "operTime", Label: "时间", Format: capability.FormatDateTime}}}, {"loginlog", "sys_login_log", "info_id", "login_time", "登录日志", "monitor:loginlog", []capability.ExportColumn{{Field: "infoId", Label: "编号"}, {Field: "userName", Label: "账号"}, {Field: "ipaddr", Label: "IP"}, {Field: "browser", Label: "浏览器"}, {Field: "status", Label: "状态"}, {Field: "msg", Label: "提示"}, {Field: "loginTime", Label: "时间", Format: capability.FormatDateTime}}}, {"jobLog", "sys_job_log", "job_log_id", "create_time", "任务日志", "monitor:job", []capability.ExportColumn{{Field: "jobLogId", Label: "编号"}, {Field: "jobName", Label: "任务"}, {Field: "invokeTarget", Label: "处理器"}, {Field: "status", Label: "状态"}, {Field: "jobMessage", Label: "消息"}, {Field: "costTime", Label: "耗时毫秒"}, {Field: "createTime", Label: "时间", Format: capability.FormatDateTime}}}}
	for _, sp := range specs {
		sp := sp
		name := "monitor." + sp.name
		path := "/monitor/" + sp.name
		export := &capability.ExportSpec{SheetName: sp.title, FileName: sp.title + "_{date}", Perm: sp.perm + ":export", Columns: sp.cols}
		capability.Register(capability.Capability[LogListIn, Page[map[string]any]]{Meta: capability.Meta{Name: name + ".list", Summary: "查询" + sp.title, Perm: sp.perm + ":list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: path + "/list"}, AIEnabled: true, MCPEnabled: true, Export: export}, Fn: func(ctx context.Context, in LogListIn) (Page[map[string]any], error) {
			q, err := s.logQuery(ctx, sp, in)
			if err != nil {
				return Page[map[string]any]{}, err
			}
			var total int64
			if err = q.Count(&total).Error; err != nil {
				return Page[map[string]any]{}, err
			}
			order, err := in.OrderScope(map[string]string{"operId": sp.id, "infoId": sp.id, "jobLogId": sp.id, "createTime": sp.date, "operTime": sp.date, "loginTime": sp.date}, sp.id)
			if err != nil {
				return Page[map[string]any]{}, err
			}
			rows := []map[string]any{}
			if err = q.Scopes(order, in.Scope()).Find(&rows).Error; err != nil {
				return Page[map[string]any]{}, err
			}
			for i := range rows {
				rows[i] = camelMap(rows[i])
			}
			return Page[map[string]any]{rows, total, in.PageNum, in.PageSize}, nil
		}})
		capability.Register(capability.Capability[LogIDs, capability.Empty]{Meta: capability.Meta{Name: name + ".remove", Summary: "删除" + sp.title, Perm: sp.perm + ":remove", Risk: capability.RiskHigh, Mutating: true, HTTP: &capability.HTTPBinding{Method: "DELETE", Path: path + "/:ids"}, AIEnabled: true, MCPEnabled: true, LogTitle: sp.title, BizType: capability.BizDelete}, Fn: func(ctx context.Context, in LogIDs) (capability.Empty, error) {
			q := dbx.DBFrom(ctx, s.db).Table(sp.table)
			if err := q.Where(sp.id+" IN ?", in.IDs).Delete(nil).Error; err != nil {
				return capability.Empty{}, err
			}
			for _, id := range in.IDs {
				capability.RecorderFrom(ctx).Delete(sp.table, fmt.Sprint(id), fmt.Sprint(id), nil)
			}
			return capability.Empty{}, nil
		}, DryRun: func(ctx context.Context, in LogIDs) (*capability.Preview, error) {
			var n int64
			err := dbx.DBFrom(ctx, s.db).Table(sp.table).Where(sp.id+" IN ?", in.IDs).Count(&n).Error
			return &capability.Preview{Summary: fmt.Sprintf("永久删除 %d 条%s", n, sp.title), Affected: int(n), Warnings: []string{"日志物理删除不可恢复"}}, err
		}})
		capability.Register(capability.Capability[capability.Empty, capability.Empty]{Meta: capability.Meta{Name: name + ".clean", Summary: "清空" + sp.title, Perm: sp.perm + ":remove", Risk: capability.RiskCritical, Mutating: true, HTTP: &capability.HTTPBinding{Method: "DELETE", Path: path + "/clean"}, AIEnabled: false, MCPEnabled: false, LogTitle: sp.title, BizType: capability.BizTruncate}, Fn: func(ctx context.Context, _ capability.Empty) (capability.Empty, error) {
			var n int64
			q := dbx.DBFrom(ctx, s.db).Table(sp.table)
			if err := q.Count(&n).Error; err != nil {
				return capability.Empty{}, err
			}
			if err := q.Where("1=1").Delete(nil).Error; err != nil {
				return capability.Empty{}, err
			}
			capability.RecorderFrom(ctx).Delete(sp.table, "all", fmt.Sprintf("%d 条日志", n), nil)
			return capability.Empty{}, nil
		}, DryRun: func(ctx context.Context, _ capability.Empty) (*capability.Preview, error) {
			var n int64
			err := dbx.DBFrom(ctx, s.db).Table(sp.table).Count(&n).Error
			return &capability.Preview{Summary: fmt.Sprintf("永久清空全部 %d 条%s", n, sp.title), Affected: int(n), Warnings: []string{"不可撤销，请先导出需要保留的日志"}}, err
		}})
	}
	type unlockIn struct {
		UserName string `json:"userName" validate:"required,min=2,max=30"`
	}
	capability.Register(capability.Capability[unlockIn, capability.Empty]{Meta: capability.Meta{Name: "monitor.loginlog.unlock", Summary: "解除账号登录锁定", Perm: "monitor:loginlog:unlock", Risk: capability.RiskMedium, Mutating: true, HTTP: &capability.HTTPBinding{Method: "PUT", Path: "/monitor/loginlog/unlock"}, AIEnabled: true, MCPEnabled: true, LogTitle: "登录日志", BizType: capability.BizUpdate}, Fn: func(ctx context.Context, in unlockIn) (capability.Empty, error) {
		h := sha256.Sum256([]byte(in.UserName))
		err := s.cache.Del(ctx, s.cache.Key("login_fail", hex.EncodeToString(h[:]))).Err()
		return capability.Empty{}, err
	}, DryRun: func(context.Context, unlockIn) (*capability.Preview, error) {
		return &capability.Preview{Summary: "清除该账号的连续登录失败计数", Affected: 1, Warnings: []string{"此操作不可撤销"}}, nil
	}})
}

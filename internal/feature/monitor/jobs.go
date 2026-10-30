// Package monitor registers system monitoring and scheduled-job Capabilities.
package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/capability/undo"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
	"github.com/junler/intent-admin/internal/task"
	"gorm.io/gorm"
)

type Service struct {
	db      *gorm.DB
	cache   *cache.Cache
	cfg     *config.Config
	runtime *task.Runtime
}

func Register(db *gorm.DB, c *cache.Cache, cfg *config.Config, r *task.Runtime) {
	s := &Service{db, c, cfg, r}
	r.RegisterCapabilities()
	s.registerJobs()
	s.registerLogs()
	s.registerCache()
	s.registerOnline()
	s.registerServer()
	s.registerQueue()
}

type JobListIn struct {
	paginate.Query
	JobName  string `json:"jobName" form:"jobName"`
	JobGroup string `json:"jobGroup" form:"jobGroup"`
	Status   string `json:"status" form:"status"`
}
type JobInput struct {
	JobID          int64   `json:"jobId" uri:"jobId"`
	JobName        *string `json:"jobName"`
	JobGroup       *string `json:"jobGroup"`
	InvokeTarget   *string `json:"invokeTarget"`
	JobParams      *string `json:"jobParams"`
	CronExpression *string `json:"cronExpression"`
	MisfirePolicy  *string `json:"misfirePolicy"`
	Concurrent     *string `json:"concurrent"`
	Status         *string `json:"status"`
	Remark         *string `json:"remark"`
}
type JobIDs struct {
	JobIDs []int64 `json:"jobIds" uri:"jobIds" collection_format:"csv" validate:"required,min=1,max=200,dive,gt=0"`
}
type JobID struct {
	JobID int64 `json:"jobId" uri:"jobId" validate:"required,gt=0"`
}
type Page[T any] struct {
	Rows     []T   `json:"rows"`
	Total    int64 `json:"total"`
	PageNum  int   `json:"pageNum"`
	PageSize int   `json:"pageSize"`
}

func (s *Service) job(ctx context.Context, id int64) (task.Job, error) {
	var j task.Job
	err := dbx.DBFrom(ctx, s.db).Where("job_id=? AND del_flag='0'", id).First(&j).Error
	if err == gorm.ErrRecordNotFound {
		return j, errcode.NotFoundf("任务不存在")
	}
	if err == nil {
		if schedule, e := task.ValidateCron(j.CronExpression); e == nil {
			j.NextExecution = schedule.Next(time.Now())
		}
	}
	return j, err
}
func jobMap(j task.Job) map[string]any {
	return map[string]any{"job_name": j.JobName, "job_group": j.JobGroup, "invoke_target": j.InvokeTarget, "job_params": j.JobParams, "cron_expression": j.CronExpression, "misfire_policy": j.MisfirePolicy, "concurrent": j.Concurrent, "status": j.Status, "remark": j.Remark}
}
func (s *Service) jobFrom(ctx context.Context, in JobInput, create bool) (task.Job, error) {
	j := task.Job{Status: "1", Concurrent: "1", DelFlag: "0", JobGroup: "DEFAULT", MisfirePolicy: "3", JobParams: "{}"}
	if !create {
		var err error
		j, err = s.job(ctx, in.JobID)
		if err != nil {
			return j, err
		}
	}
	for _, p := range []struct {
		dst *string
		src *string
	}{{&j.JobName, in.JobName}, {&j.JobGroup, in.JobGroup}, {&j.InvokeTarget, in.InvokeTarget}, {&j.JobParams, in.JobParams}, {&j.CronExpression, in.CronExpression}, {&j.MisfirePolicy, in.MisfirePolicy}, {&j.Concurrent, in.Concurrent}, {&j.Status, in.Status}, {&j.Remark, in.Remark}} {
		if p.src != nil {
			*p.dst = *p.src
		}
	}
	return j, s.runtime.ValidateJob(&j)
}
func (s *Service) jobPreview(ctx context.Context, in JobInput, create bool) (*capability.Preview, error) {
	j, err := s.jobFrom(ctx, in, create)
	if err != nil {
		return nil, err
	}
	p := &capability.Preview{}
	op := capability.OpCreate
	before := map[string]any{}
	if !create {
		old, er := s.job(ctx, in.JobID)
		if er != nil {
			return nil, er
		}
		before = jobMap(old)
		op = capability.OpUpdate
	}
	change := capability.Change{Entity: "sys_job", ID: fmt.Sprint(j.JobID), Label: j.JobName, Op: op}
	for k, v := range jobMap(j) {
		if fmt.Sprint(before[k]) != fmt.Sprint(v) {
			change.Fields = append(change.Fields, capability.FieldDiff{Field: k, Before: before[k], After: v})
		}
	}
	p.AddChange(change)
	p.Summary = "保存任务 " + j.JobName
	return p, nil
}
func (s *Service) registerJobs() {
	type dispatchIn struct {
		TaskID string `json:"taskId" uri:"taskId" validate:"required,uuid"`
	}
	capability.Register(capability.Capability[dispatchIn, task.Outbox]{Meta: capability.Meta{Name: "monitor.job.dispatchStatus", Summary: "查询任务投递状态", Perm: "monitor:job:list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/monitor/job/dispatch/:taskId"}}, Fn: func(ctx context.Context, in dispatchIn) (task.Outbox, error) {
		return s.runtime.DispatchStatus(ctx, in.TaskID)
	}})

	capability.Register(capability.Capability[JobListIn, Page[task.Job]]{Meta: capability.Meta{Name: "monitor.job.list", Summary: "查询定时任务", Perm: "monitor:job:list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/monitor/job/list"}, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, in JobListIn) (Page[task.Job], error) {
		q := dbx.DBFrom(ctx, s.db).Model(&task.Job{}).Where("del_flag='0'")
		if in.JobName != "" {
			q = q.Where("job_name LIKE ?", "%"+in.JobName+"%")
		}
		if in.JobGroup != "" {
			q = q.Where("job_group=?", in.JobGroup)
		}
		if in.Status != "" {
			q = q.Where("status=?", in.Status)
		}
		var count int64
		if err := q.Count(&count).Error; err != nil {
			return Page[task.Job]{}, err
		}
		order, err := in.OrderScope(map[string]string{"jobId": "job_id", "jobName": "job_name", "createTime": "create_time"}, "job_id")
		if err != nil {
			return Page[task.Job]{}, err
		}
		rows := []task.Job{}
		if err = q.Scopes(order, in.Scope()).Find(&rows).Error; err != nil {
			return Page[task.Job]{}, err
		}
		for i := range rows {
			if schedule, err := task.ValidateCron(rows[i].CronExpression); err == nil {
				rows[i].NextExecution = schedule.Next(time.Now())
			}
		}
		return Page[task.Job]{rows, count, in.PageNum, in.PageSize}, nil
	}})
	capability.Register(capability.Capability[JobID, task.Job]{Meta: capability.Meta{Name: "monitor.job.get", Summary: "查询任务详情", Perm: "monitor:job:query", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/monitor/job/:jobId"}, AIEnabled: true, MCPEnabled: true}, Fn: func(ctx context.Context, in JobID) (task.Job, error) { return s.job(ctx, in.JobID) }})
	capability.Register(capability.Capability[capability.Empty, []task.Handler]{Meta: capability.Meta{Name: "monitor.job.handlers", Summary: "列出任务处理器", Perm: "monitor:job:list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/monitor/job/handlers"}, AIEnabled: true, MCPEnabled: true}, Fn: func(context.Context, capability.Empty) ([]task.Handler, error) {
		out := s.runtime.Handlers()
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out, nil
	}})
	for _, create := range []bool{true, false} {
		create := create
		action, method := "update", "PUT"
		summary, perm := "修改任务", "monitor:job:edit"
		if create {
			action, method = "create", "POST"
			summary, perm = "新增任务", "monitor:job:add"
		}
		capability.Register(capability.Capability[JobInput, task.Job]{Meta: capability.Meta{Name: "monitor.job." + action, Summary: summary, Perm: perm, Risk: capability.RiskMedium, Mutating: true, Reversible: true, HTTP: &capability.HTTPBinding{Method: method, Path: "/monitor/job/" + action}, AIEnabled: true, MCPEnabled: true, LogTitle: "定时任务", BizType: capability.BizUpdate}, Fn: func(ctx context.Context, in JobInput) (task.Job, error) {
			j, err := s.jobFrom(ctx, in, create)
			if err != nil {
				return j, err
			}
			actor, _ := capability.ActorFrom(ctx)
			now := time.Now()
			j.UpdateTime = now
			j.UpdateBy = actor.UserName
			q := dbx.DBFrom(ctx, s.db)
			rec := capability.RecorderFrom(ctx)
			if create {
				j.JobID = 0
				j.CreateBy = actor.UserName
				j.CreateTime = now
				err = q.Create(&j).Error
				if err == nil {
					rec.Create("sys_job", fmt.Sprint(j.JobID), j.JobName, jobMap(j))
				}
			} else {
				old, er := s.job(ctx, in.JobID)
				if er != nil {
					return j, er
				}
				patch := jobMap(j)
				patch["update_time"] = now
				patch["update_by"] = actor.UserName
				err = q.Model(&task.Job{}).Where("job_id=?", in.JobID).Updates(patch).Error
				if err == nil {
					rec.Update("sys_job", fmt.Sprint(in.JobID), j.JobName, jobMap(old), jobMap(j))
				}
			}
			return j, err
		}, DryRun: func(ctx context.Context, in JobInput) (*capability.Preview, error) {
			return s.jobPreview(ctx, in, create)
		}, Undo: undo.Auto(s.db, undo.Table{Name: "sys_job", IDField: "job_id"}, "del_flag", "0")})
	}
	capability.Register(capability.Capability[JobInput, capability.Empty]{Meta: capability.Meta{Name: "monitor.job.changeStatus", Summary: "启停定时任务", Perm: "monitor:job:changeStatus", Risk: capability.RiskMedium, Mutating: true, Reversible: true, HTTP: &capability.HTTPBinding{Method: "PUT", Path: "/monitor/job/changeStatus"}, AIEnabled: true, MCPEnabled: true, LogTitle: "定时任务", BizType: capability.BizUpdate}, Fn: func(ctx context.Context, in JobInput) (capability.Empty, error) {
		if in.Status == nil || (*in.Status != "0" && *in.Status != "1") {
			return capability.Empty{}, errcode.BadRequestf("状态必须为 0 或 1")
		}
		old, err := s.job(ctx, in.JobID)
		if err != nil {
			return capability.Empty{}, err
		}
		err = dbx.DBFrom(ctx, s.db).Model(&task.Job{}).Where("job_id=?", in.JobID).Updates(map[string]any{"status": *in.Status, "update_time": time.Now()}).Error
		if err == nil {
			capability.RecorderFrom(ctx).Update("sys_job", fmt.Sprint(in.JobID), old.JobName, map[string]any{"status": old.Status}, map[string]any{"status": *in.Status})
		}
		return capability.Empty{}, err
	}, DryRun: func(ctx context.Context, in JobInput) (*capability.Preview, error) {
		if in.Status == nil || (*in.Status != "0" && *in.Status != "1") {
			return nil, errcode.BadRequestf("状态必须为 0 或 1")
		}
		return s.jobPreview(ctx, JobInput{JobID: in.JobID, Status: in.Status}, false)
	}, Undo: undo.RestoreFieldsFn(s.db, undo.Table{Name: "sys_job", IDField: "job_id"})})
	capability.Register(capability.Capability[JobIDs, capability.Empty]{Meta: capability.Meta{Name: "monitor.job.remove", Summary: "删除定时任务", Perm: "monitor:job:remove", Risk: capability.RiskHigh, Mutating: true, Reversible: true, HTTP: &capability.HTTPBinding{Method: "DELETE", Path: "/monitor/job/:jobIds"}, AIEnabled: true, MCPEnabled: true, LogTitle: "定时任务", BizType: capability.BizDelete}, Fn: func(ctx context.Context, in JobIDs) (capability.Empty, error) {
		var jobs []task.Job
		q := dbx.DBFrom(ctx, s.db)
		if err := q.Where("job_id IN ? AND del_flag='0'", in.JobIDs).Find(&jobs).Error; err != nil {
			return capability.Empty{}, err
		}
		if err := q.Model(&task.Job{}).Where("job_id IN ?", in.JobIDs).Updates(map[string]any{"del_flag": "2", "status": "1", "update_time": time.Now()}).Error; err != nil {
			return capability.Empty{}, err
		}
		for _, j := range jobs {
			capability.RecorderFrom(ctx).Delete("sys_job", fmt.Sprint(j.JobID), j.JobName, jobMap(j))
		}
		return capability.Empty{}, nil
	}, DryRun: func(ctx context.Context, in JobIDs) (*capability.Preview, error) {
		p := &capability.Preview{}
		for _, id := range in.JobIDs {
			j, err := s.job(ctx, id)
			if err != nil {
				return nil, err
			}
			p.AddChange(capability.Change{Entity: "sys_job", ID: fmt.Sprint(id), Label: j.JobName, Op: capability.OpDelete})
		}
		p.Summary = fmt.Sprintf("删除 %d 个定时任务", p.Affected)
		return p, nil
	}, Undo: func(ctx context.Context, raw json.RawMessage) error {
		var p undo.Payload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.Entity != "sys_job" || p.Op != capability.OpDelete {
			return errcode.BadRequestf("回滚数据不匹配")
		}
		patch := p.Before
		if patch == nil {
			patch = map[string]any{}
		}
		patch["del_flag"] = "0"
		return dbx.DBFrom(ctx, s.db).Table("sys_job").Where("job_id=?", p.ID).Updates(patch).Error
	}})
	capability.Register(capability.Capability[JobID, map[string]string]{Meta: capability.Meta{Name: "monitor.job.run", Summary: "立即执行任务", Perm: "monitor:job:run", Risk: capability.RiskMedium, Mutating: true, HTTP: &capability.HTTPBinding{Method: "PUT", Path: "/monitor/job/run"}, AIEnabled: true, MCPEnabled: true, LogTitle: "定时任务", BizType: capability.BizOther}, Fn: func(ctx context.Context, in JobID) (map[string]string, error) {
		j, err := s.job(ctx, in.JobID)
		if err != nil {
			return nil, err
		}
		id, err := s.runtime.Enqueue(ctx, j, true)
		return map[string]string{"taskId": id, "status": "accepted"}, err
	}, DryRun: func(ctx context.Context, in JobID) (*capability.Preview, error) {
		j, err := s.job(ctx, in.JobID)
		if err != nil {
			return nil, err
		}
		if err = s.runtime.ValidateJob(&j); err != nil {
			return nil, err
		}
		return &capability.Preview{Summary: "将任务加入执行队列：" + j.JobName, Affected: 1, Warnings: []string{"入队操作不可撤销；实际数据变更另行记录"}}, nil
	}})
}
func camel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}
func camelMap(row map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range row {
		out[camel(k)] = v
	}
	return out
}

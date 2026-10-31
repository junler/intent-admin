package task

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/junler/intent-admin/internal/automation"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

const WorkflowHandler = "system:workflow"

type WorkflowParams struct {
	Flow    automation.Flow   `json:"flow"`
	OwnerID int64             `json:"ownerId"`
	Source  capability.Source `json:"source"`
	MaxRisk capability.Risk   `json:"maxRisk"`
	Perms   []string          `json:"perms"`
}

func workflowParams(raw string) (WorkflowParams, error) {
	var p WorkflowParams
	if json.Unmarshal([]byte(raw), &p) != nil || len(p.Flow.Steps) == 0 || len(p.Flow.Steps) > 25 {
		return p, errcode.BadRequestf("通用任务需要包含 1 到 25 个步骤的 flow")
	}
	return p, nil
}
func (r *Runtime) prepareWorkflow(ctx context.Context, j *Job, previous *Job) error {
	p, err := workflowParams(j.JobParams)
	if err != nil {
		return err
	}
	a, ok := capability.ActorFrom(ctx)
	if !ok || a.UserID <= 0 {
		return errcode.Unauthorizedf("请先登录")
	}
	bound := WorkflowParams{OwnerID: a.UserID, Source: capability.SourceAI, MaxRisk: a.MaxRisk, Perms: a.Perms}
	if a.Source == capability.SourceMCP {
		bound.Source = capability.SourceMCP
	}
	if previous != nil && previous.InvokeTarget == WorkflowHandler {
		bound, err = workflowParams(previous.JobParams)
		if err != nil {
			return err
		}
	}
	if (p.OwnerID != 0 && p.OwnerID != bound.OwnerID) || (p.Source != "" && p.Source != bound.Source) || (p.MaxRisk != "" && p.MaxRisk != bound.MaxRisk) || (len(p.Perms) > 0 && !reflect.DeepEqual(p.Perms, bound.Perms)) {
		return errcode.Forbiddenf("流程执行身份和权限由服务端绑定，不能指定或更换")
	}
	bound.Flow = p.Flow
	raw, _ := json.Marshal(bound)
	j.JobParams = string(raw)
	return automation.Validate(ctx, p.Flow, true)
}
func (r *Runtime) registerWorkflowHandler() {
	r.RegisterHandler(Handler{Name: WorkflowHandler, Summary: "执行通用流程：flow.steps 内使用任意已授权业务查询或操作，支持 SQL、结果引用和个人通知；ownerId/source/maxRisk/perms 由服务端绑定", ParamsProto: map[string]any{"flow": map[string]any{"steps": []any{map[string]any{"name": "stats", "capability": "automation.sql.query", "args": map[string]any{"sql": "SELECT count(*) AS count FROM sys_user WHERE create_time >= $1", "params": []any{"${today}"}}}, map[string]any{"name": "notify", "capability": "system.notification.send", "args": map[string]any{"title": "新增用户统计", "content": "今日新增 ${stats.rows.0.count} 人"}}}}, "ownerId": 0, "source": "", "maxRisk": "", "perms": []any{}}, Fn: func(ctx context.Context, raw json.RawMessage) error {
		p, err := workflowParams(string(raw))
		if err != nil {
			return err
		}
		if r.ActorLoader == nil || p.OwnerID <= 0 || !p.MaxRisk.Valid() || (p.Source != capability.SourceAI && p.Source != capability.SourceMCP) {
			return errcode.Forbiddenf("流程缺少有效的执行身份")
		}
		a, err := r.ActorLoader(ctx, p.OwnerID)
		if err != nil {
			return err
		}
		if a == nil || a.UserID != p.OwnerID {
			return errcode.Forbiddenf("流程执行账号不可用")
		}
		actor := *a
		actor.Source = p.Source
		actor.Type = capability.ActorAgent
		actor.ApprovalMode = "confirmed"
		actor.Intent = "定时通用流程"
		if p.Source == capability.SourceMCP {
			actor.Type = capability.ActorMCPClient
		}
		actor.Perms = nil
		for _, perm := range p.Perms {
			if perm == capability.SuperPerm {
				actor.Perms = append([]string{}, a.Perms...)
				break
			}
			if a.Can(perm) {
				actor.Perms = append(actor.Perms, perm)
			}
		}
		if !actor.MaxRisk.Valid() {
			return errcode.Forbiddenf("流程执行账号风险上限无效")
		}
		if actor.MaxRisk.Level() > p.MaxRisk.Level() {
			actor.MaxRisk = p.MaxRisk
		}
		ctx = capability.WithActor(ctx, &actor)
		args, _ := json.Marshal(p.Flow)
		_, err = capability.InvokeJSON(ctx, "automation.flow.run", args)
		return err
	}})
}

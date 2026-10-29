package monitor

import (
	"context"
	"fmt"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/task"
)

func (s *Service) registerQueue() {
	capability.Register(capability.Capability[capability.Empty, task.QueueSummary]{Meta: capability.Meta{Name: "monitor.queue.summary", Summary: "查询当前部署队列概览", Perm: "monitor:queue:list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/monitor/queue/summary"}}, Fn: func(ctx context.Context, _ capability.Empty) (task.QueueSummary, error) {
		return s.runtime.QueueSummary(ctx)
	}})
	capability.Register(capability.Capability[task.QueueTaskQuery, task.QueueTaskPage]{Meta: capability.Meta{Name: "monitor.queue.tasks", Summary: "分页查询当前部署队列任务", Perm: "monitor:queue:list", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/monitor/queue/tasks"}}, Fn: s.runtime.QueueTasks})
	capability.Register(capability.Capability[task.QueueTaskRef, task.QueueTask]{Meta: capability.Meta{Name: "monitor.queue.task", Summary: "查询脱敏队列任务详情", Perm: "monitor:queue:query", Risk: capability.RiskLow, HTTP: &capability.HTTPBinding{Method: "GET", Path: "/monitor/queue/task"}}, Fn: s.runtime.QueueTask})
	for _, action := range []string{"retry", "archive", "cancel", "delete"} {
		action := action
		label := map[string]string{"retry": "立即重试任务", "archive": "归档队列任务", "cancel": "请求取消运行任务", "delete": "永久删除队列任务"}[action]
		risk := capability.RiskHigh
		perm := "monitor:queue:" + action
		method := "POST"
		bizType := capability.BizUpdate
		if action == "archive" {
			risk = capability.RiskMedium
		}
		if action == "delete" {
			method = "DELETE"
			perm = "monitor:queue:remove"
			bizType = capability.BizDelete
		}
		capability.Register(capability.Capability[task.QueueTaskRef, map[string]any]{Meta: capability.Meta{Name: "monitor.queue." + action, Summary: label, Perm: perm, Risk: risk, Mutating: true, HTTP: &capability.HTTPBinding{Method: method, Path: "/monitor/queue/" + action}, LogTitle: "队列管理", BizType: bizType}, Fn: func(ctx context.Context, in task.QueueTaskRef) (map[string]any, error) {
			return s.runtime.QueueAction(ctx, action, in)
		}, DryRun: func(ctx context.Context, in task.QueueTaskRef) (*capability.Preview, error) {
			t, err := s.runtime.ValidateQueueAction(ctx, action, in)
			if err != nil {
				return nil, err
			}
			after := map[string]string{"retry": "pending", "archive": "archived", "cancel": "cancellation_requested", "delete": "deleted"}[action]
			p := &capability.Preview{Summary: fmt.Sprintf("%s：%s / %s", label, t.Queue, t.TaskID), Evidence: &capability.Evidence{Route: "/monitor/queue", Filters: map[string]any{"queue": t.Queue, "state": t.State}}}
			p.AddChange(capability.Change{Entity: "queue_task", ID: t.TaskID, Label: t.Type, Op: capability.OpUpdate, Fields: []capability.FieldDiff{{Field: "state", Label: "任务状态", Before: t.State, After: after}}})
			p.Warn("队列操作无法自动撤销；不会修改任务参数、导出身份或持久完成记录。")
			if action == "retry" {
				p.Warn("任务将立即进入待处理队列，重新执行原处理器；异步导出会重新验证原发起人的当前权限。")
			}
			if action == "cancel" {
				p.Warn("取消为尽力请求，worker 会取消任务上下文并停止后续重试；已提交的业务结果不会回滚。请刷新确认最终状态。")
			}
			if action == "delete" {
				p.Warn("永久删除 Redis 中的任务与结果。持久完成标记仍会保留，防止业务重复执行。")
			}
			return p, nil
		}})
	}
	for _, paused := range []bool{true, false} {
		paused := paused
		action, label := "resume", "恢复队列处理"
		if paused {
			action, label = "pause", "暂停队列处理"
		}
		capability.Register(capability.Capability[task.QueueRef, capability.Empty]{Meta: capability.Meta{Name: "monitor.queue." + action, Summary: label, Perm: "monitor:queue:pause", Risk: capability.RiskMedium, Mutating: true, Idempotent: true, HTTP: &capability.HTTPBinding{Method: "POST", Path: "/monitor/queue/" + action}, LogTitle: "队列管理", BizType: capability.BizUpdate}, Fn: func(_ context.Context, in task.QueueRef) (capability.Empty, error) {
			return capability.Empty{}, s.runtime.SetQueuePaused(in, paused)
		}, DryRun: func(ctx context.Context, in task.QueueRef) (*capability.Preview, error) {
			summary, err := s.runtime.QueueSummary(ctx)
			if err != nil {
				return nil, err
			}
			// QueueTasks validates the exact logical whitelist without touching data.
			if _, err = s.runtime.QueueTasks(ctx, task.QueueTaskQuery{Queue: in.Queue, State: "pending"}); err != nil {
				return nil, err
			}
			p := &capability.Preview{Summary: label + "：" + in.Queue}
			for _, q := range summary.Queues {
				if q.Queue == in.Queue {
					p.AddChange(capability.Change{Entity: "queue", ID: q.Queue, Op: capability.OpUpdate, Fields: []capability.FieldDiff{{Field: "paused", Label: "已暂停", Before: q.Paused, After: paused}}})
				}
			}
			p.Warn("暂停只阻止领取新任务，运行中的任务继续执行。可通过恢复队列处理继续消费。")
			return p, nil
		}})
	}
}

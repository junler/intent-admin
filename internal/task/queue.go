package task

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
)

// Only logical queue names cross the HTTP boundary. Inspector's global queue
// catalogue and physical Redis namespace are deliberately never exposed.
var logicalQueues = []string{"critical", "default", "low"}

type QueueInfo struct {
	Queue          string  `json:"queue"`
	Exists         bool    `json:"exists"`
	Pending        int     `json:"pending"`
	Active         int     `json:"active"`
	Scheduled      int     `json:"scheduled"`
	Retry          int     `json:"retry"`
	Archived       int     `json:"archived"`
	Completed      int     `json:"completed"`
	Size           int     `json:"size"`
	Processed      int     `json:"processed"`
	Failed         int     `json:"failed"`
	Paused         bool    `json:"paused"`
	LatencySeconds float64 `json:"latencySeconds"`
}
type QueueSummary struct {
	Queues    []QueueInfo `json:"queues"`
	Pending   int         `json:"pending"`
	Active    int         `json:"active"`
	Scheduled int         `json:"scheduled"`
	Retry     int         `json:"retry"`
	Archived  int         `json:"archived"`
	Completed int         `json:"completed"`
	Processed int         `json:"processed"`
	Failed    int         `json:"failed"`
	AsOf      time.Time   `json:"asOf"`
}
type QueueTask struct {
	TaskID           string    `json:"taskId"`
	Queue            string    `json:"queue"`
	Type             string    `json:"type"`
	State            string    `json:"state"`
	Payload          any       `json:"payload,omitempty"`
	Result           any       `json:"result,omitempty"`
	LastError        string    `json:"lastError,omitempty"`
	MaxRetry         int       `json:"maxRetry"`
	Retried          int       `json:"retried"`
	NextProcessAt    time.Time `json:"nextProcessAt"`
	LastFailedAt     time.Time `json:"lastFailedAt"`
	CompletedAt      time.Time `json:"completedAt"`
	TimeoutSeconds   int64     `json:"timeoutSeconds"`
	RetentionSeconds int64     `json:"retentionSeconds"`
	Internal         bool      `json:"internal"`
	Managed          bool      `json:"managed"`
	CanRetry         bool      `json:"canRetry"`
	CanArchive       bool      `json:"canArchive"`
	CanCancel        bool      `json:"canCancel"`
	CanDelete        bool      `json:"canDelete"`
}
type QueueTaskQuery struct {
	paginate.Query
	Queue string `json:"queue" form:"queue"`
	State string `json:"state" form:"state"`
}
type QueueTaskPage struct {
	Rows     []QueueTask `json:"rows"`
	Total    int64       `json:"total"`
	PageNum  int         `json:"pageNum"`
	PageSize int         `json:"pageSize"`
}
type QueueTaskRef struct {
	Queue         string `json:"queue" form:"queue" validate:"required"`
	TaskID        string `json:"taskId" form:"taskId" validate:"required,max=128"`
	ExpectedState string `json:"expectedState" form:"expectedState"`
}
type QueueRef struct {
	Queue string `json:"queue" form:"queue" validate:"required"`
}

func (r *Runtime) inspector() *asynq.Inspector {
	return asynq.NewInspector(asynq.RedisClientOpt{Addr: r.Cfg.Redis.Addr, Password: r.Cfg.Redis.Password, DB: r.Cfg.Redis.DB})
}

// GetQueueInfo in Asynq 0.25 returns an unwrapped internal NOT_FOUND for
// queues not yet created, so use its catalogue only to test our fixed names.
func queueExists(i *asynq.Inspector, name string) (bool, error) {
	queues, err := i.Queues()
	if err != nil {
		return false, err
	}
	for _, q := range queues {
		if q == name {
			return true, nil
		}
	}
	return false, nil
}
func (r *Runtime) physicalQueue(logical string) (string, error) {
	for _, q := range logicalQueues {
		if q == logical {
			return queueName(r.Cfg.Redis.KeyPrefix, q), nil
		}
	}
	return "", errcode.Forbiddenf("只能管理当前部署的 critical、default 或 low 队列")
}
func (r *Runtime) QueueSummary(ctx context.Context) (QueueSummary, error) {
	out := QueueSummary{Queues: []QueueInfo{}, AsOf: time.Now()}
	i := r.inspector()
	defer i.Close()
	for _, q := range logicalQueues {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		physical, _ := r.physicalQueue(q)
		exists, e := queueExists(i, physical)
		if e != nil {
			return out, e
		}
		if !exists {
			out.Queues = append(out.Queues, QueueInfo{Queue: q})
			continue
		}
		x, e := i.GetQueueInfo(physical)
		if errors.Is(e, asynq.ErrQueueNotFound) {
			out.Queues = append(out.Queues, QueueInfo{Queue: q})
			continue
		}
		if e != nil {
			return out, e
		}
		n := QueueInfo{Queue: q, Exists: true, Pending: x.Pending, Active: x.Active, Scheduled: x.Scheduled, Retry: x.Retry, Archived: x.Archived, Completed: x.Completed, Size: x.Size, Processed: x.Processed, Failed: x.Failed, Paused: x.Paused, LatencySeconds: x.Latency.Seconds()}
		out.Queues = append(out.Queues, n)
		out.Pending += n.Pending
		out.Active += n.Active
		out.Scheduled += n.Scheduled
		out.Retry += n.Retry
		out.Archived += n.Archived
		out.Completed += n.Completed
		out.Processed += n.Processed
		out.Failed += n.Failed
	}
	return out, nil
}
func queueStateCount(q *asynq.QueueInfo, state string) (int, error) {
	switch state {
	case "pending":
		return q.Pending, nil
	case "active":
		return q.Active, nil
	case "scheduled":
		return q.Scheduled, nil
	case "retry":
		return q.Retry, nil
	case "archived":
		return q.Archived, nil
	case "completed":
		return q.Completed, nil
	}
	return 0, errcode.BadRequestf("任务状态必须为 pending、active、scheduled、retry、archived 或 completed")
}
func (r *Runtime) QueueTasks(ctx context.Context, in QueueTaskQuery) (QueueTaskPage, error) {
	physical, err := r.physicalQueue(in.Queue)
	if err != nil {
		return QueueTaskPage{}, err
	}
	if _, err = queueStateCount(&asynq.QueueInfo{}, in.State); err != nil {
		return QueueTaskPage{}, err
	}
	in.Normalize()
	// Inspector pagination multiplies page by size internally; cap unreasonable offsets.
	if in.PageNum > 100000 {
		return QueueTaskPage{}, errcode.BadRequestf("页码超出允许范围")
	}
	out := QueueTaskPage{Rows: []QueueTask{}, PageNum: in.PageNum, PageSize: in.PageSize}
	i := r.inspector()
	defer i.Close()
	exists, err := queueExists(i, physical)
	if err != nil {
		return out, err
	}
	if !exists {
		return out, nil
	}
	q, err := i.GetQueueInfo(physical)
	if errors.Is(err, asynq.ErrQueueNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	n, _ := queueStateCount(q, in.State)
	out.Total = int64(n)
	opts := []asynq.ListOption{asynq.Page(in.PageNum), asynq.PageSize(in.PageSize)}
	var rows []*asynq.TaskInfo
	switch in.State {
	case "pending":
		rows, err = i.ListPendingTasks(physical, opts...)
	case "active":
		rows, err = i.ListActiveTasks(physical, opts...)
	case "scheduled":
		rows, err = i.ListScheduledTasks(physical, opts...)
	case "retry":
		rows, err = i.ListRetryTasks(physical, opts...)
	case "archived":
		rows, err = i.ListArchivedTasks(physical, opts...)
	case "completed":
		rows, err = i.ListCompletedTasks(physical, opts...)
	}
	if err != nil {
		return out, err
	}
	for _, row := range rows {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		out.Rows = append(out.Rows, queueTaskDTO(in.Queue, row, false))
	}
	return out, nil
}
func queueTaskDTO(queue string, t *asynq.TaskInfo, detail bool) QueueTask {
	managed := t.Type == "intent-admin:job" || t.Type == "intent-admin:export" || t.Type == "intent-admin:maintenance"
	internal := t.Type != "intent-admin:job"
	state := t.State.String()
	out := QueueTask{TaskID: t.ID, Queue: queue, Type: t.Type, State: state, MaxRetry: t.MaxRetry, Retried: t.Retried, NextProcessAt: t.NextProcessAt, LastFailedAt: t.LastFailedAt, CompletedAt: t.CompletedAt, TimeoutSeconds: int64(t.Timeout.Seconds()), RetentionSeconds: int64(t.Retention.Seconds()), Internal: internal, Managed: managed, CanRetry: managed && (state == "retry" || state == "scheduled" || state == "archived"), CanArchive: state == "pending" || state == "scheduled" || state == "retry", CanCancel: managed && state == "active", CanDelete: state != "active"}
	// Error strings may contain serialized parameters, SQL or remote credentials.
	if t.LastErr != "" {
		out.LastError = "执行失败，详细原因请查看有权限的业务日志"
	}
	if detail {
		if internal {
			out.Payload = "内部任务参数与身份信息已隐藏"
			out.Result = "内部任务结果已隐藏"
		} else {
			out.Payload = redactQueueJSON(t.Payload)
			out.Result = redactQueueJSON(t.Result)
		}
	}
	return out
}
func redactQueueJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > 65536 {
		return "内容过大，已隐藏"
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "非结构化内容已隐藏"
	}
	return redactQueueValue(v, 0)
}
func redactQueueValue(v any, depth int) any {
	if depth > 8 {
		return "[已隐藏]"
	}
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, value := range x {
			k := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
			hide := false
			for _, s := range []string{"password", "passwd", "pwd", "secret", "token", "credential", "apikey", "accesskey", "privatekey", "authorization", "cookie", "userid", "username", "actor", "permission", "roles", "email", "phone", "mobile"} {
				if strings.Contains(k, s) {
					hide = true
					break
				}
			}
			if hide {
				out[key] = "[已隐藏]"
			} else {
				out[key] = redactQueueValue(value, depth+1)
			}
		}
		return out
	case []any:
		if len(x) > 100 {
			return "数组过大，已隐藏"
		}
		out := make([]any, len(x))
		for k, value := range x {
			out[k] = redactQueueValue(value, depth+1)
		}
		return out
	case string:
		if json.Valid([]byte(x)) {
			return redactQueueJSON([]byte(x))
		}
		return "[文本内容已隐藏]"
	default:
		return v
	}
}
func (r *Runtime) queueTaskInfo(in QueueTaskRef) (*asynq.TaskInfo, error) {
	physical, err := r.physicalQueue(in.Queue)
	if err != nil {
		return nil, err
	}
	if in.TaskID == "" || len(in.TaskID) > 128 {
		return nil, errcode.BadRequestf("任务编号必填，最多 128 字节")
	}
	i := r.inspector()
	defer i.Close()
	t, err := i.GetTaskInfo(physical, in.TaskID)
	if errors.Is(err, asynq.ErrTaskNotFound) || errors.Is(err, asynq.ErrQueueNotFound) {
		return nil, errcode.NotFoundf("当前队列中不存在该任务")
	}
	return t, err
}
func (r *Runtime) QueueTask(ctx context.Context, in QueueTaskRef) (QueueTask, error) {
	t, err := r.queueTaskInfo(in)
	if err != nil {
		return QueueTask{}, err
	}
	out := queueTaskDTO(in.Queue, t, true)
	if out.CanRetry && r.DB != nil {
		var n int64
		if err = r.DB.WithContext(ctx).Model(&Outbox{}).Where("task_id=? AND queue_name=? AND execution_state='done'", in.TaskID, t.Queue).Count(&n).Error; err != nil {
			return out, err
		}
		if n > 0 {
			out.CanRetry = false
		}
	}
	return out, nil
}
func (r *Runtime) ValidateQueueAction(ctx context.Context, action string, in QueueTaskRef) (QueueTask, error) {
	t, err := r.QueueTask(ctx, in)
	if err != nil {
		return t, err
	}
	if in.ExpectedState == "" {
		return t, errcode.BadRequestf("执行前必须提供预览时的 expectedState")
	}
	if t.State != in.ExpectedState {
		return t, errcode.StaleDataf("任务状态已变化，请刷新后重新预览")
	}
	allowed := false
	switch action {
	case "retry":
		allowed = t.CanRetry
	case "archive":
		allowed = t.CanArchive
	case "cancel":
		allowed = t.CanCancel
	case "delete":
		allowed = t.CanDelete
	}
	if !allowed {
		return t, errcode.Conflictf("当前任务状态不支持此操作，已完成的持久任务不会重新执行")
	}
	return t, nil
}
func (r *Runtime) QueueAction(ctx context.Context, action string, in QueueTaskRef) (map[string]any, error) {
	t, err := r.ValidateQueueAction(ctx, action, in)
	if err != nil {
		return nil, err
	}
	physical, _ := r.physicalQueue(in.Queue)
	i := r.inspector()
	defer i.Close()
	status := "done"
	switch action {
	case "retry":
		if err = r.Cache.Del(ctx, r.queueCancelKey(physical, in.TaskID)).Err(); err == nil {
			err = i.RunTask(physical, in.TaskID)
		}
	case "archive":
		err = i.ArchiveTask(physical, in.TaskID)
	case "delete":
		err = i.DeleteTask(physical, in.TaskID)
	case "cancel":
		err = r.Cache.Set(ctx, r.queueCancelKey(physical, in.TaskID), "1", 31*time.Minute).Err()
		status = "cancellation_requested"
	}
	if err != nil {
		return nil, errcode.Conflictf("任务状态可能已变化，请刷新后重试").Wrap(err)
	}
	return map[string]any{"taskId": t.TaskID, "queue": t.Queue, "action": action, "status": status}, nil
}
func (r *Runtime) SetQueuePaused(in QueueRef, paused bool) error {
	physical, err := r.physicalQueue(in.Queue)
	if err != nil {
		return err
	}
	i := r.inspector()
	defer i.Close()
	exists, err := queueExists(i, physical)
	if err != nil {
		return err
	}
	if !exists {
		return errcode.NotFoundf("队列尚未创建，请等待首个任务入队")
	}
	q, err := i.GetQueueInfo(physical)
	if err != nil {
		return err
	}
	if q.Paused == paused {
		return nil
	}
	if paused {
		return i.PauseQueue(physical)
	}
	return i.UnpauseQueue(physical)
}

func (r *Runtime) queueCancelKey(physical, id string) string {
	return r.Cache.Key("queue_cancel", physical, id)
}

// Asynq's cancellation channel is global by task ID, with no queue namespace.
// Our own marker includes both deployment prefix and physical queue, so even
// equal task IDs in another deployment cannot cancel each other's workers.
func (r *Runtime) cancellable(next asynq.HandlerFunc) asynq.HandlerFunc {
	return func(parent context.Context, t *asynq.Task) error {
		id, _ := asynq.GetTaskID(parent)
		queue, _ := asynq.GetQueueName(parent)
		if id == "" || queue == "" {
			return next(parent, t)
		}
		ctx, cancel := context.WithCancel(parent)
		defer cancel()
		key := r.queueCancelKey(queue, id)
		requested := func() bool {
			v, err := r.Cache.Exists(context.WithoutCancel(parent), key).Result()
			return err == nil && v > 0
		}
		if requested() {
			return errors.Join(asynq.SkipRetry, errors.New("队列管理已取消该任务"))
		}
		done := make(chan struct{})
		defer close(done)
		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					if requested() {
						cancel()
						return
					}
				}
			}
		}()
		err := next(ctx, t)
		if requested() {
			return errors.Join(asynq.SkipRetry, errors.New("队列管理已取消该任务"))
		}
		return err
	}
}

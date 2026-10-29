// Package task owns the durable work queue, handler allowlist and DB-driven
// scheduler. Every business handler invokes a Capability with a system actor.
package task

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type Handler struct {
	Name        string                                       `json:"name"`
	Summary     string                                       `json:"summary"`
	ParamsProto any                                          `json:"paramsProto"`
	Fn          func(context.Context, json.RawMessage) error `json:"-"`
}
type Job struct {
	JobID          int64     `gorm:"column:job_id;primaryKey;autoIncrement" json:"jobId"`
	JobName        string    `json:"jobName"`
	JobGroup       string    `json:"jobGroup"`
	InvokeTarget   string    `json:"invokeTarget"`
	JobParams      string    `json:"jobParams"`
	CronExpression string    `json:"cronExpression"`
	MisfirePolicy  string    `json:"misfirePolicy"`
	Concurrent     string    `json:"concurrent"`
	Status         string    `json:"status"`
	DelFlag        string    `json:"-"`
	CreateBy       string    `json:"createBy"`
	CreateTime     time.Time `json:"createTime"`
	UpdateBy       string    `json:"updateBy"`
	UpdateTime     time.Time `json:"updateTime"`
	Remark         string    `json:"remark"`
	NextExecution  time.Time `gorm:"-" json:"nextExecution"`
}

func (Job) TableName() string { return "sys_job" }

type JobLog struct {
	JobLogID      int64     `gorm:"primaryKey;column:job_log_id" json:"jobLogId"`
	JobID         int64     `json:"jobId"`
	JobName       string    `json:"jobName"`
	JobGroup      string    `json:"jobGroup"`
	InvokeTarget  string    `json:"invokeTarget"`
	JobParams     string    `json:"jobParams"`
	TaskID        string    `json:"taskId"`
	JobMessage    string    `json:"jobMessage"`
	Status        string    `json:"status"`
	ExceptionInfo string    `json:"exceptionInfo"`
	CostTime      int64     `json:"costTime"`
	CreateTime    time.Time `json:"createTime"`
}

func (JobLog) TableName() string { return "sys_job_log" }

type jobPayload struct {
	JobID  int64           `json:"jobId"`
	Target string          `json:"target"`
	Params json.RawMessage `json:"params"`
	Manual bool            `json:"manual"`
}
type Runtime struct {
	DB               *gorm.DB
	Cache            *cache.Cache
	Cfg              *config.Config
	client           *asynq.Client
	server           *asynq.Server
	manager          *asynq.PeriodicTaskManager
	mu               sync.RWMutex
	handlers         map[string]Handler
	cancel           context.CancelFunc
	leader           string
	workerStarted    bool
	closeOnce        sync.Once
	managerStop      sync.Once
	outboxOnce       sync.Once
	FileWriter       func(context.Context, string, io.Reader, string, string) (map[string]any, error)
	FileCleaner      func(context.Context, time.Time) error
	RefreshDashboard func(context.Context) error
	RefreshKnowledge func(context.Context) error
}

func New(db *gorm.DB, c *cache.Cache, cfg *config.Config) *Runtime {
	opt := asynq.RedisClientOpt{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB}
	r := &Runtime{DB: db, Cache: c, Cfg: cfg, client: asynq.NewClient(opt), handlers: map[string]Handler{}}
	concurrency := cfg.Task.Concurrency
	if concurrency < 1 {
		concurrency = 10
	}
	r.server = asynq.NewServer(opt, asynq.Config{Concurrency: concurrency, Queues: map[string]int{queueName(cfg.Redis.KeyPrefix, "critical"): 6, queueName(cfg.Redis.KeyPrefix, "default"): 3, queueName(cfg.Redis.KeyPrefix, "low"): 1}, IsFailure: func(e error) bool { return !errors.Is(e, ErrSkipped) }})
	r.registerBuiltins()
	return r
}

var ErrSkipped = errors.New("task skipped")

func (r *Runtime) RegisterHandler(h Handler) {
	if h.Name == "" || h.Fn == nil {
		panic("invalid task handler")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.handlers[h.Name]; ok {
		panic("duplicate task handler " + h.Name)
	}
	r.handlers[h.Name] = h
}
func (r *Runtime) Handlers() []Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []Handler{}
	for _, h := range r.handlers {
		out = append(out, h)
	}
	return out
}
func ValidateCron(expression string) (cron.Schedule, error) {
	if strings.Contains(expression, "?") {
		return nil, errcode.BadRequestf("不支持 Quartz 问号占位符，请使用标准五字段 cron")
	}
	s, err := cron.ParseStandard(expression)
	if err != nil {
		return nil, errcode.BadRequestf("cron 表达式无效：请使用分 时 日 月 周五字段，或 @every 描述符（不支持 Quartz）：%v", err)
	}
	if strings.HasPrefix(expression, "@every ") {
		d, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(expression, "@every ")))
		if err != nil || d < time.Second {
			return nil, errcode.BadRequestf("@every 间隔必须至少 1 秒")
		}
	}
	return s, nil
}
func (r *Runtime) ValidateJob(j *Job) error {
	if j.InvokeTarget == "system:exportAsync" {
		return errcode.Forbiddenf("异步导出只能从已授权用户的导出接口发起，不能作为定时任务选择")
	}
	if len(strings.TrimSpace(j.JobName)) == 0 || len(j.JobName) > 64 {
		return errcode.BadRequestf("任务名称必填，最多 64 字节")
	}
	if _, err := ValidateCron(j.CronExpression); err != nil {
		return err
	}
	r.mu.RLock()
	h, ok := r.handlers[j.InvokeTarget]
	r.mu.RUnlock()
	if !ok {
		return errcode.BadRequestf("调用目标必须选择已注册的处理器")
	}
	if j.JobParams == "" {
		j.JobParams = "{}"
	}
	if len(j.JobParams) > 65536 {
		return errcode.BadRequestf("任务参数最多 64 KiB")
	}
	if !json.Valid([]byte(j.JobParams)) {
		return errcode.BadRequestf("任务参数必须为有效 JSON")
	}
	if h.ParamsProto != nil {
		b, _ := json.Marshal(h.ParamsProto)
		var schema map[string]any
		_ = json.Unmarshal(b, &schema)
		var params map[string]any
		if json.Unmarshal([]byte(j.JobParams), &params) != nil || params == nil {
			return errcode.BadRequestf("任务参数必须为 JSON 对象")
		}
		for k, v := range params {
			example, ok := schema[k]
			if !ok {
				return errcode.BadRequestf("处理器不支持参数 %s", k)
			}
			valid := true
			switch example.(type) {
			case string:
				_, valid = v.(string)
			case bool:
				_, valid = v.(bool)
			case float64:
				n, ok := v.(float64)
				valid = ok && math.Trunc(n) == n
			case map[string]any:
				_, valid = v.(map[string]any)
			}
			if !valid {
				return errcode.BadRequestf("处理器参数 %s 的类型错误", k)
			}
		}
		if days, ok := params["days"].(float64); ok && (days < 1 || days > 3650) {
			return errcode.BadRequestf("保留天数必须在 1 到 3650 之间")
		}
		if j.InvokeTarget == "demo:withParams" {
			id, _ := params["itemId"].(float64)
			status, _ := params["status"].(string)
			if id <= 0 || (status != "0" && status != "1") {
				return errcode.BadRequestf("演示处理器需要正整数 itemId 与状态 0 或 1")
			}
		}

	}
	if j.JobGroup == "" {
		j.JobGroup = "DEFAULT"
	}
	switch strings.ToLower(j.JobGroup) {
	case "default", "critical", "low":
	default:
		return errcode.BadRequestf("任务分组只能为 DEFAULT、critical 或 low")
	}
	if j.Concurrent == "" {
		j.Concurrent = "1"
	}
	if j.Concurrent != "0" && j.Concurrent != "1" {
		return errcode.BadRequestf("concurrent 必须为 0 或 1")
	}
	if j.Status == "" {
		j.Status = "1"
	}
	if j.Status != "0" && j.Status != "1" {
		return errcode.BadRequestf("status 必须为 0 或 1")
	}
	if j.MisfirePolicy == "" {
		j.MisfirePolicy = "3"
	}
	switch j.MisfirePolicy {
	case "1", "2", "3":
	default:
		return errcode.BadRequestf("重试策略必须为 1、2 或 3")
	}
	return nil
}
func (r *Runtime) Enqueue(ctx context.Context, j Job, manual bool) (string, error) {
	if err := r.ValidateJob(&j); err != nil {
		return "", err
	}
	p, _ := json.Marshal(jobPayload{JobID: j.JobID, Target: j.InvokeTarget, Params: json.RawMessage(j.JobParams), Manual: manual})
	return r.acceptTask(ctx, "intent-admin:job", p, r.options(j, !manual))
}
func (r *Runtime) options(j Job, unique bool) []asynq.Option {
	queue := strings.ToLower(j.JobGroup)
	if queue == "" {
		queue = "default"
	}
	retries := 0
	if j.MisfirePolicy == "1" {
		retries = 3
	}
	if j.MisfirePolicy == "2" {
		retries = 1
	}
	opts := []asynq.Option{asynq.Queue(queueName(r.Cfg.Redis.KeyPrefix, queue)), asynq.MaxRetry(retries), asynq.Timeout(30 * time.Minute)}
	if unique && j.Concurrent == "1" {
		s, _ := ValidateCron(j.CronExpression)
		now := time.Now()
		next := s.Next(now)
		ttl := s.Next(next).Sub(next)
		if ttl > 24*time.Hour {
			ttl = 24 * time.Hour
		}
		if ttl < time.Second {
			ttl = time.Second
		}
		opts = append(opts, asynq.Unique(ttl))
	}
	return opts
}
func (r *Runtime) GetConfigs() ([]*asynq.PeriodicTaskConfig, error) {
	var jobs []Job
	if err := r.DB.Where("status='0' AND del_flag='0'").Find(&jobs).Error; err != nil {
		return nil, err
	}
	out := []*asynq.PeriodicTaskConfig{{Cronspec: "@hourly", Task: asynq.NewTask("intent-admin:maintenance", nil), Opts: []asynq.Option{asynq.Queue(queueName(r.Cfg.Redis.KeyPrefix, "low")), asynq.MaxRetry(1), asynq.Timeout(5 * time.Minute)}}}
	for _, j := range jobs {
		if err := r.ValidateJob(&j); err != nil {
			return nil, fmt.Errorf("job %d: %w", j.JobID, err)
		}
		p, _ := json.Marshal(jobPayload{JobID: j.JobID, Target: j.InvokeTarget, Params: json.RawMessage(j.JobParams)})
		out = append(out, &asynq.PeriodicTaskConfig{Cronspec: j.CronExpression, Task: asynq.NewTask("intent-admin:job", p), Opts: r.options(j, true)})
	}
	return out, nil
}
func (r *Runtime) mux() *asynq.ServeMux {
	m := asynq.NewServeMux()
	m.HandleFunc("intent-admin:job", func(ctx context.Context, t *asynq.Task) error { return r.executeDelivery(ctx, t, r.handleJob) })
	m.HandleFunc("intent-admin:export", func(ctx context.Context, t *asynq.Task) error { return r.executeDelivery(ctx, t, r.handleExport) })
	m.HandleFunc("intent-admin:maintenance", func(ctx context.Context, _ *asynq.Task) error {
		ctx = capability.WithActor(ctx, capability.SystemActor("首页统计刷新"))
		_, err := capability.InvokeJSON(ctx, "task.system.refreshDashboard", json.RawMessage(`{}`))
		return err
	})
	return m
}
func (r *Runtime) Start(ctx context.Context) error {
	ctx, r.cancel = context.WithCancel(ctx)
	r.startOutbox(ctx)
	if r.RefreshDashboard != nil {
		go func() {
			warmCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := r.RefreshDashboard(warmCtx); err != nil {
				slog.Warn("dashboard warmup failed", "error", err)
			}
		}()
	}
	if r.Cfg.Task.Embedded {
		if err := r.server.Start(r.mux()); err != nil {
			return err
		}
		r.workerStarted = true
	}
	if r.Cfg.Task.Scheduler {
		if err := r.startScheduler(ctx); err != nil {
			r.Close()
			return err
		}
	}
	return nil
}
func (r *Runtime) RunWorker(ctx context.Context) error {
	ctx, r.cancel = context.WithCancel(ctx)
	r.startOutbox(ctx)
	if err := r.server.Start(r.mux()); err != nil {
		return err
	}
	r.workerStarted = true
	<-ctx.Done()
	r.Close()
	return nil
}
func (r *Runtime) startScheduler(ctx context.Context) error {
	owner := uuid.NewString()
	ok, err := r.Cache.SetNX(ctx, r.Cache.Key("scheduler", "leader"), owner, 30*time.Second).Result()
	if err != nil {
		return err
	}
	if !ok {
		slog.Warn("scheduler leader already active; scheduling disabled on this instance")
		return nil
	}
	r.leader = owner
	interval := r.Cfg.Task.SyncInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	location, _ := time.LoadLocation(r.Cfg.DB.TimeZone)
	if location == nil {
		location = time.Local
	}
	manager, err := asynq.NewPeriodicTaskManager(asynq.PeriodicTaskManagerOpts{RedisConnOpt: asynq.RedisClientOpt{Addr: r.Cfg.Redis.Addr, Password: r.Cfg.Redis.Password, DB: r.Cfg.Redis.DB}, PeriodicTaskConfigProvider: r, SyncInterval: interval, SchedulerOpts: &asynq.SchedulerOpts{Location: location}})
	if err != nil {
		return err
	}
	r.manager = manager
	if err := manager.Start(); err != nil {
		return err
	}
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				result, err := leaderRenew.Run(ctx, r.Cache.Client, []string{r.Cache.Key("scheduler", "leader")}, owner, 30000).Int()
				if err != nil || result != 1 {
					slog.Error("scheduler leadership lost; stopping scheduler", "error", err)
					r.stopManager()
					return
				}
			}
		}
	}()
	return nil
}

var leaderRenew = redis.NewScript(`if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('PEXPIRE',KEYS[1],ARGV[2]) end return 0`)
var lockDelete = redis.NewScript(`if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`)

func (r *Runtime) stopManager() {
	r.managerStop.Do(func() {
		if r.manager != nil {
			r.manager.Shutdown()
		}
	})
}
func (r *Runtime) Close() {
	r.closeOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		r.stopManager()
		if r.workerStarted {
			r.server.Shutdown()
			r.workerStarted = false
		}
		if r.leader != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = lockDelete.Run(ctx, r.Cache.Client, []string{r.Cache.Key("scheduler", "leader")}, r.leader).Err()
			r.leader = ""
		}
		_ = r.client.Close()
	})
}
func (r *Runtime) handleJob(ctx context.Context, t *asynq.Task) (err error) {
	var p jobPayload
	if json.Unmarshal(t.Payload(), &p) != nil {
		return fmt.Errorf("invalid job payload: %w", asynq.SkipRetry)
	}
	var job Job
	if err = r.DB.WithContext(ctx).Where("job_id=? AND del_flag='0'", p.JobID).First(&job).Error; err != nil {
		return fmt.Errorf("job no longer exists: %w", asynq.SkipRetry)
	}
	if p.Target != job.InvokeTarget || (!p.Manual && job.Status != "0") {
		return nil
	}
	r.mu.RLock()
	h, ok := r.handlers[job.InvokeTarget]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unregistered handler: %w", asynq.SkipRetry)
	}
	start := time.Now()
	skipped := false
	taskID, _ := asynq.GetTaskID(ctx)
	log := JobLog{JobID: job.JobID, JobName: job.JobName, JobGroup: job.JobGroup, InvokeTarget: job.InvokeTarget, JobParams: string(p.Params), TaskID: taskID, Status: "0", CreateTime: start}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("task panic: %v", p)
		}
		log.CostTime = time.Since(start).Milliseconds()
		log.JobMessage = "执行成功"
		if err != nil {
			log.Status = "1"
			log.ExceptionInfo = err.Error()
			log.JobMessage = "执行失败"
		}
		if skipped {
			log.Status = "0"
			log.JobMessage = "因已有实例运行而跳过"
		}
		logDB := dbx.DBFrom(ctx, r.DB)
		if err != nil {
			logDB = r.DB.WithContext(context.WithoutCancel(ctx))
		}
		if e := logDB.Create(&log).Error; e != nil {
			slog.Error("job log persistence failed", "error", e)
		}
	}()
	if job.Concurrent == "1" {
		release, e := r.lockJob(ctx, job.JobID)
		if errors.Is(e, ErrSkipped) {
			skipped = true
			return nil
		}
		if e != nil {
			return e
		}
		if cleanup, ok := ctx.Value(deliveryCleanupKey{}).(*deliveryCleanup); ok {
			cleanup.callbacks = append(cleanup.callbacks, release)
		} else {
			defer release()
		}
	}
	actor := capability.SystemActor(job.JobName)
	ctx = capability.WithActor(ctx, actor)
	params := p.Params
	if len(params) == 0 {
		params = json.RawMessage(job.JobParams)
	}
	return h.Fn(ctx, params)
}
func (r *Runtime) Backlog(ctx context.Context) int64 {
	ins := asynq.NewInspector(asynq.RedisClientOpt{Addr: r.Cfg.Redis.Addr, Password: r.Cfg.Redis.Password, DB: r.Cfg.Redis.DB})
	defer ins.Close()
	var n int64
	_ = r.DB.WithContext(ctx).Model(&Outbox{}).Where("dispatch_state='pending' AND execution_state<>'done' AND queue_name IN ?", []string{queueName(r.Cfg.Redis.KeyPrefix, "critical"), queueName(r.Cfg.Redis.KeyPrefix, "default"), queueName(r.Cfg.Redis.KeyPrefix, "low")}).Count(&n).Error
	for _, q := range []string{"critical", "default", "low"} {
		i, e := ins.GetQueueInfo(queueName(r.Cfg.Redis.KeyPrefix, q))
		if e == nil {
			n += int64(i.Pending + i.Retry + i.Active + i.Scheduled)
		}
	}
	return n
}

func (r *Runtime) lockJob(ctx context.Context, id int64) (func(), error) {
	key := r.Cache.Key("job_running", fmt.Sprint(id))
	owner := uuid.NewString()
	ok, err := r.Cache.SetNX(ctx, key, owner, 31*time.Minute).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrSkipped
	}
	return func() { _ = lockDelete.Run(context.WithoutCancel(ctx), r.Cache.Client, []string{key}, owner).Err() }, nil
}

// Queue names include a stable namespace so deployments sharing Redis DBs
// cannot consume each other's jobs while keeping UI group names unchanged.
func queueName(prefix, logical string) string {
	sum := sha256.Sum256([]byte(prefix))
	return fmt.Sprintf("intent_%x_%s", sum[:6], logical)
}

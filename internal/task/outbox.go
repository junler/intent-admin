package task

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Outbox is committed with the business operation. Redis delivery only starts
// after that commit; failures stay pending and are retried by every runtime.
type Outbox struct {
	TaskID         string          `gorm:"column:task_id;primaryKey" json:"taskId"`
	TaskType       string          `json:"taskType"`
	Payload        json.RawMessage `gorm:"type:jsonb" json:"-"`
	QueueName      string          `json:"queueName"`
	MaxRetry       int             `json:"maxRetry"`
	TimeoutSeconds int64           `json:"timeoutSeconds"`
	UniqueSeconds  int64           `json:"uniqueSeconds"`
	DispatchState  string          `json:"dispatchState"`
	ExecutionState string          `json:"executionState"`
	Attempts       int             `json:"attempts"`
	LastError      string          `json:"lastError"`
	NextAttemptAt  time.Time       `json:"nextAttemptAt"`
	DeliveredAt    *time.Time      `json:"deliveredAt"`
	CompletedAt    *time.Time      `json:"completedAt"`
	CreateTime     time.Time       `json:"createTime"`
	UpdateTime     time.Time       `json:"updateTime"`
}

func (Outbox) TableName() string { return "sys_task_outbox" }
func (r *Runtime) acceptTask(ctx context.Context, kind string, payload []byte, options []asynq.Option) (string, error) {
	now := time.Now()
	record := Outbox{TaskID: uuid.NewString(), TaskType: kind, Payload: payload, QueueName: queueName(r.Cfg.Redis.KeyPrefix, "default"), TimeoutSeconds: 1800, DispatchState: "pending", ExecutionState: "pending", NextAttemptAt: now, CreateTime: now, UpdateTime: now}
	for _, opt := range options {
		switch opt.Type() {
		case asynq.QueueOpt:
			record.QueueName = opt.Value().(string)
		case asynq.MaxRetryOpt:
			record.MaxRetry = opt.Value().(int)
		case asynq.TimeoutOpt:
			record.TimeoutSeconds = int64(opt.Value().(time.Duration).Seconds())
		case asynq.UniqueOpt:
			record.UniqueSeconds = int64(opt.Value().(time.Duration).Seconds())
		}
	}
	if err := dbx.DBFrom(ctx, r.DB).Create(&record).Error; err != nil {
		return "", err
	}
	dbx.AfterCommit(ctx, func() {
		delivery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := r.dispatchOne(delivery, record.TaskID); err != nil {
			slog.Warn("task accepted durably; Redis dispatch will retry", "task_id", record.TaskID, "error", err)
		}
	})
	return record.TaskID, nil
}
func (r *Runtime) dispatchOne(ctx context.Context, id string) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record Outbox
		found := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("task_id=? AND dispatch_state='pending' AND execution_state<>'done'", id).Limit(1).Find(&record)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 0 {
			return nil
		}
		opts := []asynq.Option{asynq.TaskID(record.TaskID), asynq.Queue(record.QueueName), asynq.MaxRetry(record.MaxRetry), asynq.Timeout(time.Duration(record.TimeoutSeconds) * time.Second), asynq.Retention(7 * 24 * time.Hour)}
		if record.UniqueSeconds > 0 {
			opts = append(opts, asynq.Unique(time.Duration(record.UniqueSeconds)*time.Second))
		}
		_, enqueueErr := r.client.EnqueueContext(ctx, asynq.NewTask(record.TaskType, record.Payload), opts...)
		now := time.Now()
		patch := map[string]any{"attempts": record.Attempts + 1, "update_time": now}
		if enqueueErr == nil || errors.Is(enqueueErr, asynq.ErrTaskIDConflict) {
			patch["dispatch_state"] = "delivered"
			patch["delivered_at"] = now
			patch["last_error"] = ""
		} else {
			backoff := time.Duration(record.Attempts+1) * time.Second
			if backoff > time.Minute {
				backoff = time.Minute
			}
			patch["last_error"] = enqueueErr.Error()
			patch["next_attempt_at"] = now.Add(backoff)
		}
		if err := tx.Model(&Outbox{}).Where("task_id=?", id).Updates(patch).Error; err != nil {
			return err
		}
		return nil
	})
}
func (r *Runtime) drainOutbox(ctx context.Context) {
	var ids []string
	if err := r.DB.WithContext(ctx).Model(&Outbox{}).Where("dispatch_state='pending' AND execution_state<>'done' AND next_attempt_at<=?", time.Now()).Order("create_time ASC").Limit(100).Pluck("task_id", &ids).Error; err != nil {
		if ctx.Err() == nil {
			slog.Warn("task outbox scan failed", "error", err)
		}
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if err := r.dispatchOne(ctx, id); err != nil {
			slog.Warn("task outbox delivery retry failed", "task_id", id, "error", err)
		}
	}
}
func (r *Runtime) startOutbox(ctx context.Context) {
	r.outboxOnce.Do(func() {
		go func() {
			r.drainOutbox(ctx)
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					r.drainOutbox(ctx)
				}
			}
		}()
	})
}

type deliveryCleanupKey struct{}
type deliveryCleanup struct{ callbacks []func() }

// The completion marker commits with the Capability's business changes. If a
// process crashes after Redis accepted a task, redelivery with the same ID can
// see that marker and cannot apply the write twice.
func (r *Runtime) executeDelivery(ctx context.Context, t *asynq.Task, fn func(context.Context, *asynq.Task) error) error {
	id, _ := asynq.GetTaskID(ctx)
	if id == "" {
		return fn(ctx, t)
	}
	var count int64
	if err := r.DB.WithContext(ctx).Model(&Outbox{}).Where("task_id=?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fn(ctx, t)
	}
	cleanup := &deliveryCleanup{}
	ctx = context.WithValue(ctx, deliveryCleanupKey{}, cleanup)
	defer func() {
		for i := len(cleanup.callbacks) - 1; i >= 0; i-- {
			cleanup.callbacks[i]()
		}
	}()
	err := dbx.NewTxRunner(r.DB)(ctx, func(txCtx context.Context) error {
		q := dbx.DBFrom(txCtx, r.DB)
		var record Outbox
		if err := q.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id=?", id).First(&record).Error; err != nil {
			return err
		}
		if record.ExecutionState == "done" {
			return nil
		}
		if err := q.Model(&Outbox{}).Where("task_id=?", id).Updates(map[string]any{"execution_state": "running", "update_time": time.Now()}).Error; err != nil {
			return err
		}
		if err := fn(txCtx, t); err != nil {
			return err
		}
		now := time.Now()
		return q.Model(&Outbox{}).Where("task_id=?", id).Updates(map[string]any{"execution_state": "done", "dispatch_state": "delivered", "completed_at": now, "update_time": now, "last_error": ""}).Error
	})
	if err != nil {
		_ = r.DB.WithContext(context.WithoutCancel(ctx)).Model(&Outbox{}).Where("task_id=? AND execution_state<>'done'", id).Updates(map[string]any{"execution_state": "failed", "last_error": err.Error(), "update_time": time.Now()}).Error
	}
	return err
}
func (r *Runtime) DispatchStatus(ctx context.Context, id string) (Outbox, error) {
	var out Outbox
	if _, err := uuid.Parse(id); err != nil {
		return out, errcode.BadRequestf("任务编号无效")
	}
	err := r.DB.WithContext(ctx).Where("task_id=? AND queue_name IN ?", id, []string{queueName(r.Cfg.Redis.KeyPrefix, "critical"), queueName(r.Cfg.Redis.KeyPrefix, "default"), queueName(r.Cfg.Redis.KeyPrefix, "low")}).First(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, errcode.NotFoundf("任务投递记录不存在")
	}
	return out, err
}

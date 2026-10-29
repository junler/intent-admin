package task

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/migrations"
	"github.com/pressly/goose/v3"
)

func TestTransactionalOutboxIntegration(t *testing.T) {
	if os.Getenv("INTENT_ADMIN_TASK_INTEGRATION") != "1" {
		t.Skip("requires isolated PostgreSQL/Redis")
	}
	cfg, err := config.Load("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Name != "intent_tasks_integration" {
		t.Fatal("use disposable intent_tasks_integration database")
	}
	cfg.Redis.DB = 14
	cfg.Redis.KeyPrefix = "outbox-integration:"
	db, err := dbx.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbx.Close(db)
	sqlDB, _ := db.DB()
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("schema_migrations")
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.Up(sqlDB, "."); err != nil {
		t.Fatal(err)
	}
	c, err := cache.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := New(db, c, cfg)
	defer r.Close()
	ins := asynq.NewInspector(asynq.RedisClientOpt{Addr: cfg.Redis.Addr, DB: 14})
	defer ins.Close()
	ctx := context.Background()
	runner := dbx.NewTxRunner(db)
	queue := queueName(cfg.Redis.KeyPrefix, "default")
	var id string
	rollback := errors.New("intentional plan rollback")
	err = runner(ctx, func(txCtx context.Context) error {
		j := Job{JobName: "rolled back job", JobGroup: "DEFAULT", InvokeTarget: "demo:noParams", JobParams: "{}", CronExpression: "@hourly", MisfirePolicy: "3", Concurrent: "1", Status: "1", DelFlag: "0", CreateTime: time.Now(), UpdateTime: time.Now()}
		if err := dbx.DBFrom(txCtx, db).Create(&j).Error; err != nil {
			return err
		}
		var err error
		id, err = r.Enqueue(txCtx, j, true)
		if err != nil {
			return err
		}
		if _, err = ins.GetTaskInfo(queue, id); !(errors.Is(err, asynq.ErrTaskNotFound) || errors.Is(err, asynq.ErrQueueNotFound)) {
			t.Fatalf("job escaped uncommitted transaction: %v", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var count int64
	db.Model(&Outbox{}).Where("task_id=?", id).Count(&count)
	if count != 0 {
		t.Fatal("rolled-back outbox row persisted")
	}
	if _, err = ins.GetTaskInfo(queue, id); !(errors.Is(err, asynq.ErrTaskNotFound) || errors.Is(err, asynq.ErrQueueNotFound)) {
		t.Fatalf("rolled-back job reached Redis: %v", err)
	}
	var committed Job
	err = runner(ctx, func(txCtx context.Context) error {
		committed = Job{JobName: "committed job", JobGroup: "DEFAULT", InvokeTarget: "demo:noParams", JobParams: "{}", CronExpression: "@hourly", MisfirePolicy: "3", Concurrent: "1", Status: "1", DelFlag: "0", CreateTime: time.Now(), UpdateTime: time.Now()}
		if err := dbx.DBFrom(txCtx, db).Create(&committed).Error; err != nil {
			return err
		}
		var err error
		id, err = r.Enqueue(txCtx, committed, true)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM sys_job WHERE job_id=?", committed.JobID)
	defer db.Exec("DELETE FROM sys_task_outbox WHERE task_id=?", id)
	defer ins.DeleteTask(queue, id)
	info, err := ins.GetTaskInfo(queue, id)
	if err != nil || info.ID != id {
		t.Fatalf("committed job not delivered: %v", err)
	}
	state, err := r.DispatchStatus(ctx, id)
	if err != nil || state.DispatchState != "delivered" {
		t.Fatalf("bad dispatch state: %v %v", state, err)
	}
	// Redis downtime leaves a committed pending record, not a lost task or a
	// falsely queued result. Recovery is a retry with the same stable task ID.
	r.client.Close()
	r.client = asynq.NewClient(asynq.RedisClientOpt{Addr: "127.0.0.1:1", DB: 14, DialTimeout: 100 * time.Millisecond, ReadTimeout: 100 * time.Millisecond, WriteTimeout: 100 * time.Millisecond})
	pending, err := r.Enqueue(ctx, committed, true)
	if err != nil {
		t.Fatalf("durable acceptance should survive Redis downtime: %v", err)
	}
	defer db.Exec("DELETE FROM sys_task_outbox WHERE task_id=?", pending)
	state, err = r.DispatchStatus(ctx, pending)
	if err != nil || state.DispatchState != "pending" || state.Attempts < 1 || state.LastError == "" {
		t.Fatalf("failed delivery not retained: %+v err=%v", state, err)
	}
	r.client.Close()
	r.client = asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Redis.Addr, DB: 14})
	if err = r.dispatchOne(ctx, pending); err != nil {
		t.Fatal(err)
	}
	defer ins.DeleteTask(queue, pending)
	state, err = r.DispatchStatus(ctx, pending)
	if err != nil || state.DispatchState != "delivered" {
		t.Fatalf("recovered dispatch missing: %+v %v", state, err)
	}
	if err = r.dispatchOne(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if _, err = ins.GetTaskInfo(queue, pending); err != nil {
		t.Fatal("idempotent delivery lost task")
	}
}

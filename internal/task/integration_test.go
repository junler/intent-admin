package task_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/changeset"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/feature/demo"
	"github.com/junler/intent-admin/internal/pkg/paginate"
	"github.com/junler/intent-admin/internal/task"
	"github.com/junler/intent-admin/migrations"
	"github.com/pressly/goose/v3"
	"github.com/xuri/excelize/v2"
)

// Requires a disposable PostgreSQL database and a dedicated Redis DB. The test
// starts real Asynq scheduling/workers and checks their durable effects.
func TestQueueSchedulerAndAsyncExportIntegration(t *testing.T) {
	if os.Getenv("INTENT_ADMIN_TASK_INTEGRATION") != "1" {
		t.Skip("set INTENT_ADMIN_TASK_INTEGRATION=1 with isolated PostgreSQL/Redis")
	}
	cfg, err := config.Load("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Name != "intent_tasks_integration" {
		t.Fatal("use disposable intent_tasks_integration database")
	}
	cfg.Redis.DB = 13
	cfg.Redis.KeyPrefix = "task-integration:"
	cfg.Task.Embedded = true
	cfg.Task.Scheduler = true
	cfg.Task.Concurrency = 4
	cfg.Task.SyncInterval = 100 * time.Millisecond
	db, err := dbx.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dbx.Close(db) })
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
	t.Cleanup(c.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cs := changeset.New(db)
	capability.Install(capability.Hooks{Tx: dbx.NewTxRunner(db), ReadOnly: dbx.NewReadOnlyRunner(), PersistChanges: cs.Persist, WriteAudit: cs.WriteAudit, MaxChangesetItems: 2000})
	demo.Register(db)
	r := task.New(db, c, cfg)
	r.RegisterCapabilities()
	t.Cleanup(r.Close)
	if err = db.Exec("UPDATE demo_item SET status='0' WHERE item_id=1").Error; err != nil {
		t.Fatal(err)
	}
	job := task.Job{JobName: "integration scheduled mutation", JobGroup: "DEFAULT", InvokeTarget: "demo:withParams", JobParams: `{"itemId":1,"status":"1"}`, CronExpression: "@every 1s", MisfirePolicy: "3", Concurrent: "1", Status: "0", DelFlag: "0", CreateTime: time.Now(), UpdateTime: time.Now()}
	if err = db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM sys_job WHERE job_id=?", job.JobID) })
	if err = r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 15*time.Second, func() bool {
		var n int64
		db.Table("sys_job_log").Where("job_id=? AND status='0'", job.JobID).Count(&n)
		return n > 0
	})
	var status string
	db.Raw("SELECT status FROM demo_item WHERE item_id=1").Scan(&status)
	if status != "1" {
		t.Fatal("scheduled capability did not mutate row")
	}
	var taskChanges int64
	db.Table("sys_changeset").Where("source='task' AND actor_type='system'").Count(&taskChanges)
	if taskChanges == 0 {
		t.Fatal("scheduled write has no changeset")
	}
	if err = db.Model(&task.Job{}).Where("job_id=?", job.JobID).Update("status", "1").Error; err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	var stopped int64
	db.Table("sys_job_log").Where("job_id=?", job.JobID).Count(&stopped)
	time.Sleep(1500 * time.Millisecond)
	var after int64
	db.Table("sys_job_log").Where("job_id=?", job.JobID).Count(&after)
	if after != stopped {
		t.Fatalf("disabled task still ran %d -> %d", stopped, after)
	}
	var active, max atomic.Int32
	var completed atomic.Int32
	r.RegisterHandler(task.Handler{Name: "integration:slow", Summary: "test nonconcurrency", ParamsProto: map[string]any{}, Fn: func(ctx context.Context, _ json.RawMessage) error {
		n := active.Add(1)
		for {
			m := max.Load()
			if n <= m || max.CompareAndSwap(m, n) {
				break
			}
		}
		dbx.AfterCommit(ctx, func() { time.Sleep(100 * time.Millisecond); active.Add(-1) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
			completed.Add(1)
			return nil
		}
	}})
	slow := task.Job{JobName: "integration nonconcurrent", JobGroup: "DEFAULT", InvokeTarget: "integration:slow", JobParams: "{}", CronExpression: "@hourly", MisfirePolicy: "3", Concurrent: "1", Status: "1", DelFlag: "0", CreateTime: time.Now(), UpdateTime: time.Now()}
	if err = db.Create(&slow).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM sys_job WHERE job_id=?", slow.JobID) })
	slowTasks := []string{}
	for i := 0; i < 4; i++ {
		id, enqueueErr := r.Enqueue(ctx, slow, true)
		if enqueueErr != nil {
			err = enqueueErr
		}
		if err != nil {
			t.Fatal(err)
		}
		slowTasks = append(slowTasks, id)
	}
	waitFor(t, 10*time.Second, func() bool {
		var n int64
		db.Table("sys_job_log").Where("job_id=?", slow.JobID).Count(&n)
		return n >= 4
	})
	if max.Load() != 1 || completed.Load() < 1 {
		t.Fatalf("nonconcurrency max=%d complete=%d", max.Load(), completed.Load())
	}
	// Replaying a retained task ID after broker loss cannot repeat a completed
	// business action: the completion marker lives with the business DB write.
	dispatchRecord, er := r.DispatchStatus(ctx, slowTasks[0])
	if er != nil || dispatchRecord.ExecutionState != "done" {
		t.Fatalf("completion not durable: %+v %v", dispatchRecord, er)
	}
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: cfg.Redis.Addr, DB: 13})
	defer inspector.Close()
	waitFor(t, 5*time.Second, func() bool {
		info, err := inspector.GetTaskInfo(dispatchRecord.QueueName, dispatchRecord.TaskID)
		return err == nil && info.State == asynq.TaskStateCompleted
	})
	if er = inspector.DeleteTask(dispatchRecord.QueueName, dispatchRecord.TaskID); er != nil {
		t.Fatal(er)
	}
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Redis.Addr, DB: 13})
	defer client.Close()
	beforeReplay := completed.Load()
	if _, er = client.EnqueueContext(ctx, asynq.NewTask(dispatchRecord.TaskType, dispatchRecord.Payload), asynq.TaskID(dispatchRecord.TaskID), asynq.Queue(dispatchRecord.QueueName), asynq.Retention(time.Minute)); er != nil {
		t.Fatal(er)
	}
	waitFor(t, 5*time.Second, func() bool {
		info, er := inspector.GetTaskInfo(dispatchRecord.QueueName, dispatchRecord.TaskID)
		return er == nil && info.State == asynq.TaskStateCompleted
	})
	if completed.Load() != beforeReplay {
		t.Fatal("redelivered task repeated business side effect")
	}
	// A real queued 60k-row export paginates all rows and emits a workbook plus
	// a notification, using the original user's freshly evaluated permissions.
	if err = db.Exec("DELETE FROM sys_login_log WHERE user_name='integration_export'").Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("INSERT INTO sys_login_log (user_name,msg) SELECT 'integration_export','row '||n FROM generate_series(1,60000) n").Error; err != nil {
		t.Fatal(err)
	}
	type exportIn struct{ paginate.Query }
	type exportOut struct {
		Rows  []map[string]any `json:"rows"`
		Total int64            `json:"total"`
	}
	capability.Register(capability.Capability[exportIn, exportOut]{Meta: capability.Meta{Name: "integration.logs.list", Summary: "导出测试", Perm: "integration:export:list", Risk: capability.RiskLow, Export: &capability.ExportSpec{SheetName: "日志", FileName: "日志_{date}", Perm: "integration:export:export", Columns: []capability.ExportColumn{{Field: "infoId", Label: "编号"}, {Field: "msg", Label: "消息"}}}}, Fn: func(ctx context.Context, in exportIn) (exportOut, error) {
		q := dbx.DBFrom(ctx, db).Table("sys_login_log").Where("user_name='integration_export'")
		var total int64
		if err := q.Count(&total).Error; err != nil {
			return exportOut{}, err
		}
		rows := []map[string]any{}
		err := q.Select("info_id AS \"infoId\",msg").Order("info_id").Scopes(in.Scope()).Find(&rows).Error
		return exportOut{rows, total}, err
	}})
	delivered := make(chan int, 1)
	r.FileWriter = func(ctx context.Context, name string, reader io.Reader, mime, biz string) (map[string]any, error) {
		if mime != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" || biz != "export" {
			return nil, fmt.Errorf("invalid generated file metadata")
		}
		f, err := excelize.OpenReader(reader)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		rows, err := f.Rows("日志")
		if err != nil {
			return nil, err
		}
		count := 0
		for rows.Next() {
			count++
		}
		if err = rows.Error(); err != nil {
			return nil, err
		}
		_ = rows.Close()
		delivered <- count
		return map[string]any{"uploadId": "integration-export", "fileName": name}, nil
	}
	entry, _ := capability.Lookup("integration.logs.list")
	actor := &capability.Actor{Type: capability.ActorHuman, Source: capability.SourceHTTP, UserID: 1, UserName: "admin", Perms: []string{capability.SuperPerm}, MaxRisk: capability.RiskCritical}
	if _, err = r.EnqueueExport(capability.WithActor(ctx, actor), entry, exportIn{}); err != nil {
		t.Fatal(err)
	}
	select {
	case count := <-delivered:
		if count != 60001 {
			t.Fatalf("export workbook rows=%d, expected60001", count)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("queued export timed out")
	}
	waitFor(t, 5*time.Second, func() bool {
		var n int64
		db.Table("sys_notice").Where("notice_title LIKE ?", "导出完成：%").Count(&n)
		return n > 0
	})
}
func waitFor(t *testing.T, timeout time.Duration, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("timed out waiting for queued work")
}

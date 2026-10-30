package task

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

func TestQueueBoundaryAndRedaction(t *testing.T) {
	r := testRuntime(t)
	for _, q := range []string{"default", "critical", "low"} {
		if _, err := r.physicalQueue(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"DEFAULT", "default:other", "intent_foreign_default", "../default", ""} {
		if _, err := r.physicalQueue(q); err == nil {
			t.Fatalf("untrusted queue %q accepted", q)
		}
	}
	task := &asynq.TaskInfo{ID: "id", Type: "intent-admin:export", Payload: []byte(`{"userId":1,"input":{"secret":"private"}}`), Result: []byte(`{"url":"private"}`), State: asynq.TaskStateRetry, LastErr: "password=private"}
	out := queueTaskDTO("default", task, true)
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "private") || strings.Contains(string(b), "userId") {
		t.Fatalf("internal identity escaped: %s", b)
	}
	task.Type = "intent-admin:job"
	task.Payload = []byte(`{"jobId":7,"params":{"pwd":"secret","token":"secret","userId":9,"nested":{"api_key":"secret"},"text":"secret","days":3}}`)
	out = queueTaskDTO("default", task, true)
	b, _ = json.Marshal(out)
	if strings.Contains(string(b), `"secret"`) || strings.Contains(string(b), `"userId":9`) {
		t.Fatalf("job credentials escaped: %s", b)
	}
	if out.CanCancel || !out.CanRetry || !out.CanArchive || !out.CanDelete {
		t.Fatal("incorrect retry action matrix")
	}
	task.State = asynq.TaskStateCompleted
	out = queueTaskDTO("default", task, false)
	if out.CanRetry || out.CanCancel || out.CanArchive || !out.CanDelete {
		t.Fatal("completed task can execute again")
	}
}

// Uses fresh, UUID-scoped queues in Redis. It deliberately creates the SAME
// task ID in two deployments, proving cancellation does not use Asynq's global
// cancellation channel. No FLUSHDB or foreign queue deletion is used.
func TestQueueControlRedisIntegration(t *testing.T) {
	if os.Getenv("INTENT_ADMIN_TASK_INTEGRATION") != "1" {
		t.Skip("requires real Redis")
	}
	cfg, err := config.Load("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Redis.DB = 12
	cfg.Redis.KeyPrefix = "queue-control:" + uuid.NewString() + ":"
	newRuntime := func(cfg *config.Config) *Runtime {
		c, e := cache.Open(cfg)
		if e != nil {
			t.Fatal(e)
		}
		r := New(nil, c, cfg)
		t.Cleanup(func() { r.Close(); c.Close() })
		return r
	}
	r := newRuntime(cfg)
	foreignCfg := *cfg
	foreignCfg.Redis.KeyPrefix = "queue-foreign:" + uuid.NewString() + ":"
	foreign := newRuntime(&foreignCfg)
	i := r.inspector()
	defer i.Close()
	ownQ, _ := r.physicalQueue("default")
	foreignQ, _ := foreign.physicalQueue("default")
	t.Cleanup(func() {
		for _, q := range []string{ownQ, foreignQ} {
			_ = i.DeleteQueue(q, true)
		}
	})
	ctx := context.Background()
	info, err := r.client.Enqueue(asynq.NewTask("intent-admin:job", []byte(`{"jobId":7,"params":{"secret":"hidden"}}`)), asynq.Queue(ownQ), asynq.ProcessIn(time.Hour), asynq.Retention(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	ref := QueueTaskRef{Queue: "default", TaskID: info.ID, ExpectedState: "scheduled"}
	if _, err = r.QueueAction(ctx, "archive", ref); err != nil {
		t.Fatal(err)
	}
	if _, err = r.QueueAction(ctx, "retry", ref); errcode.From(err).Code != errcode.StaleData {
		t.Fatalf("stale state accepted: %v", err)
	}
	ref.ExpectedState = "archived"
	if _, err = r.QueueAction(ctx, "retry", ref); err != nil {
		t.Fatal(err)
	}
	page, err := r.QueueTasks(ctx, QueueTaskQuery{Queue: "default", State: "pending"})
	if err != nil || page.Total != 1 || len(page.Rows) != 1 {
		t.Fatalf("pending page: %+v %v", page, err)
	}
	ref.ExpectedState = "pending"
	if _, err = r.QueueAction(ctx, "delete", ref); err != nil {
		t.Fatal(err)
	}
	if _, err = r.QueueTask(ctx, ref); errcode.From(err).Code != errcode.NotFound {
		t.Fatalf("deleted task remains: %v", err)
	}
	if err = r.SetQueuePaused(QueueRef{Queue: "default"}, true); err != nil {
		t.Fatal(err)
	}
	summary, err := r.QueueSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range summary.Queues {
		if q.Queue == "default" && !q.Paused {
			t.Fatal("pause not reflected")
		}
	}
	if err = r.SetQueuePaused(QueueRef{Queue: "default"}, false); err != nil {
		t.Fatal(err)
	}
	// Cross-prefix physical names and foreign task IDs cannot be queried/actioned.
	foreignInfo, err := foreign.client.Enqueue(asynq.NewTask("intent-admin:export", []byte(`{"userId":1}`)), asynq.Queue(foreignQ), asynq.ProcessIn(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.QueueTask(ctx, QueueTaskRef{Queue: "default", TaskID: foreignInfo.ID}); errcode.From(err).Code != errcode.NotFound {
		t.Fatal("foreign task was visible")
	}
	if _, err = r.QueueTasks(ctx, QueueTaskQuery{Queue: foreignQ, State: "scheduled"}); errcode.From(err).Code != errcode.Forbidden {
		t.Fatal("foreign physical queue accepted")
	}
	summary, err = r.QueueSummary(ctx)
	if err != nil || summary.Scheduled != 0 {
		t.Fatalf("foreign queue leaked into summary %+v %v", summary, err)
	}
	if err = i.DeleteTask(foreignQ, foreignInfo.ID); err != nil {
		t.Fatal(err)
	}

	id := uuid.NewString()
	ownStarted := make(chan struct{}, 1)
	foreignStarted := make(chan struct{}, 1)
	ownCanceled := make(chan struct{}, 1)
	foreignCanceled := make(chan struct{}, 1)
	finish := make(chan struct{})
	start := func(runtime *Runtime, q string, started, canceled chan struct{}) *asynq.Server {
		srv := asynq.NewServer(asynq.RedisClientOpt{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: 12}, asynq.Config{Concurrency: 1, Queues: map[string]int{q: 1}, TaskCheckInterval: 50 * time.Millisecond, ShutdownTimeout: time.Second})
		mux := asynq.NewServeMux()
		mux.HandleFunc("intent-admin:job", runtime.cancellable(func(ctx context.Context, _ *asynq.Task) error {
			started <- struct{}{}
			select {
			case <-ctx.Done():
				canceled <- struct{}{}
				return ctx.Err()
			case <-finish:
				return nil
			}
		}))
		if err := srv.Start(mux); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(srv.Shutdown)
		return srv
	}
	start(r, ownQ, ownStarted, ownCanceled)
	start(foreign, foreignQ, foreignStarted, foreignCanceled)
	for _, q := range []string{ownQ, foreignQ} {
		if _, err = r.client.Enqueue(asynq.NewTask("intent-admin:job", []byte(`{}`)), asynq.TaskID(id), asynq.Queue(q), asynq.MaxRetry(3), asynq.Retention(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	for _, ch := range []chan struct{}{ownStarted, foreignStarted} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not start")
		}
	}
	ref = QueueTaskRef{Queue: "default", TaskID: id, ExpectedState: "active"}
	result, err := r.QueueAction(ctx, "cancel", ref)
	if err != nil || result["status"] != "cancellation_requested" {
		t.Fatalf("cancel: %v %v", result, err)
	}
	select {
	case <-ownCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("worker context did not honor cancel")
	}
	select {
	case <-foreignCanceled:
		t.Fatal("same task ID canceled another deployment")
	case <-time.After(350 * time.Millisecond):
	}
	close(finish)
	deadline := time.Now().Add(5 * time.Second)
	for {
		a, e := i.GetTaskInfo(ownQ, id)
		b, f := i.GetTaskInfo(foreignQ, id)
		if e == nil && f == nil && a.State == asynq.TaskStateArchived && b.State == asynq.TaskStateCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancel should archive without retry; foreign should complete")
		}
		time.Sleep(50 * time.Millisecond)
	}
	ref.ExpectedState = "archived"
	if _, err = r.QueueAction(ctx, "retry", ref); err != nil {
		t.Fatal(err)
	}
	if n, e := r.Cache.Exists(ctx, r.queueCancelKey(ownQ, id)).Result(); e != nil || n != 0 {
		t.Fatal("explicit retry did not clear only its cancel marker")
	}
	// Prevent stale cancellation from being retained by the scratch fixture.
	_ = r.Cache.Del(ctx, r.queueCancelKey(ownQ, id)).Err()
}

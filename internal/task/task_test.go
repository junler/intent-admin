package task

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

func TestCronDialect(t *testing.T) {
	for _, s := range []string{"0 2 * * *", "*/5 * * * *", "@every 30s", "@daily", "@hourly"} {
		schedule, err := ValidateCron(s)
		if err != nil {
			t.Fatalf("valid %q: %v", s, err)
		}
		if !schedule.Next(time.Now()).After(time.Now()) {
			t.Fatal("next execution not in future")
		}
	}
	for _, s := range []string{"0 0 2 * * ?", "0 0 0 * * ? 2027", "* * * ? *", "@every 0s", "@every -1s", "invalid"} {
		if _, err := ValidateCron(s); err == nil {
			t.Fatalf("invalid dialect accepted: %q", s)
		}
	}
}
func testRuntime(t *testing.T) *Runtime {
	t.Helper()
	mr := miniredis.RunT(t)
	cfg := &config.Config{Redis: config.Redis{Addr: mr.Addr(), KeyPrefix: "tasks-test:"}, Task: config.Task{Concurrency: 1}}
	c, err := cache.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := New(nil, c, cfg)
	t.Cleanup(func() { r.Close(); c.Close() })
	return r
}
func TestJobAllowlistAndParameters(t *testing.T) {
	r := testRuntime(t)
	// Own the fixtures rather than borrowing a feature package's handlers: the
	// allowlist is what is under test, not any particular module.
	r.RegisterHandler(Handler{Name: "sample:noParams", Summary: "无参数", ParamsProto: map[string]any{},
		Fn: func(context.Context, json.RawMessage) error { return nil }})
	r.RegisterHandler(Handler{Name: "sample:withParams", Summary: "带参数", ParamsProto: map[string]any{"itemId": 0, "status": "1"},
		Fn: func(context.Context, json.RawMessage) error { return nil }})

	j := Job{JobName: "sample", InvokeTarget: "sample:noParams", CronExpression: "@every 30s"}
	if err := r.ValidateJob(&j); err != nil {
		t.Fatal(err)
	}
	if j.Status != "1" || j.Concurrent != "1" {
		t.Fatal("new tasks must default disabled and nonconcurrent")
	}
	j.InvokeTarget = "os:exec"
	if r.ValidateJob(&j) == nil {
		t.Fatal("unregistered invocation accepted")
	}
	j.InvokeTarget = "sample:withParams"
	j.JobParams = `{"shell":"rm -rf /"}`
	if r.ValidateJob(&j) == nil {
		t.Fatal("unknown handler argument accepted")
	}
	j.JobParams = `{"itemId":1,"status":"1"}`
	if err := r.ValidateJob(&j); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentExecutionLock(t *testing.T) {
	r := testRuntime(t)
	ctx := context.Background()
	release, err := r.lockJob(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock, err := r.lockJob(ctx, 42)
			if err == nil {
				admitted.Add(1)
				unlock()
			} else if !errors.Is(err, ErrSkipped) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 0 {
		t.Fatal("overlapping task admitted")
	}
	release()
	release2, err := r.lockJob(ctx, 42)
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	release2()
}

func TestCronCannotImpersonateExportUser(t *testing.T) {
	r := testRuntime(t)
	// This is a real export-capable target. The rejection must be about the
	// unsafe cron origin, not an unknown capability or malformed parameters.
	capability.Register(capability.Capability[capability.Empty, capability.Empty]{Meta: capability.Meta{Name: "integration.exportOriginTarget", Summary: "Export origin fixture", Perm: "integration:private:list", Risk: capability.RiskLow, Export: &capability.ExportSpec{Perm: "integration:private:export", Columns: []capability.ExportColumn{{Field: "id", Label: "ID"}}}}, Fn: func(context.Context, capability.Empty) (capability.Empty, error) { return capability.Empty{}, nil }})
	forged := Job{JobName: "impersonated export", InvokeTarget: "system:exportAsync", JobParams: `{"capability":"integration.exportOriginTarget","filters":{},"userId":1}`, CronExpression: "@hourly"}
	if err := r.ValidateJob(&forged); errcode.From(err).Code != errcode.Forbidden {
		t.Fatalf("cron accepted another user's export grants: %v", err)
	}
	if _, err := r.Enqueue(context.Background(), forged, true); errcode.From(err).Code != errcode.Forbidden {
		t.Fatalf("manual cron run bypassed export-origin boundary: %v", err)
	}
	for _, h := range r.Handlers() {
		if h.Name == "system:exportAsync" {
			t.Fatal("internal export leaked into selectable cron handlers")
		}
	}
}

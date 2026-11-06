package idempotency

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/testutil"
)

func TestConcurrentReceiptsAndRollback(t *testing.T) {
	db := testutil.Postgres(t)
	if err := db.Exec("CREATE TABLE retry_test(id bigserial PRIMARY KEY, value text)").Error; err != nil {
		t.Fatal(err)
	}
	s := New(db, "test-hmac-key")
	capability.Install(capability.Hooks{Tx: dbx.NewTxRunner(db), Deduplicate: s.Run})
	defer capability.Install(capability.Hooks{})
	type input struct {
		Value string `json:"value"`
		Fail  bool   `json:"fail"`
	}
	capability.Register(capability.Capability[input, map[string]any]{Meta: capability.Meta{Name: "test.retry.create", Summary: "测试请求去重", Perm: "test:write", Risk: capability.RiskMedium, Mutating: true, Deduplicate: true}, DryRun: func(context.Context, input) (*capability.Preview, error) {
		return &capability.Preview{Summary: "test", Affected: 1}, nil
	}, Fn: func(ctx context.Context, in input) (map[string]any, error) {
		var id int64
		if err := dbx.DBFrom(ctx, db).Raw("INSERT INTO retry_test(value) VALUES(?) RETURNING id", in.Value).Scan(&id).Error; err != nil {
			return nil, err
		}
		if in.Fail {
			return nil, errcode.BadRequestf("failed after insert")
		}
		return map[string]any{"id": id}, nil
	}})
	actor := &capability.Actor{UserID: 1, Source: capability.SourceHTTP, Perms: []string{"test:write"}, MaxRisk: capability.RiskHigh}
	ctx := capability.WithRequestKey(capability.WithActor(context.Background(), actor), "same-key-123456")
	call := func(ctx context.Context, raw string) (any, error) {
		return capability.InvokeJSON(ctx, "test.retry.create", json.RawMessage(raw))
	}
	var wg sync.WaitGroup
	results := make(chan string, 12)
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := call(ctx, `{"value":"payload"}`)
			if err != nil {
				errs <- err
				return
			}
			raw, _ := json.Marshal(out)
			results <- string(raw)
		}()
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Fatal(err)
	}
	first := ""
	for out := range results {
		if first == "" {
			first = out
		}
		if out != first {
			t.Fatalf("different replay results: %s / %s", first, out)
		}
	}
	var count int64
	db.Table("retry_test").Count(&count)
	if count != 1 {
		t.Fatalf("concurrent retry wrote %d rows", count)
	}
	if _, err := call(ctx, `{"value":"different"}`); errcode.From(err).Code != errcode.Conflict {
		t.Fatalf("parameter mismatch accepted: %v", err)
	}
	denied := capability.WithActor(ctx, &capability.Actor{UserID: 1, Source: capability.SourceHTTP, MaxRisk: capability.RiskHigh})
	if _, err := call(denied, `{"value":"payload"}`); errcode.From(err).Code != errcode.Forbidden {
		t.Fatalf("replay bypassed permissions: %v", err)
	}
	failed := capability.WithRequestKey(ctx, "rollback-123456")
	if _, err := call(failed, `{"value":"rolled back","fail":true}`); err == nil {
		t.Fatal("expected failure")
	}
	db.Table("retry_test").Count(&count)
	if count != 1 {
		t.Fatal("failed request committed business write")
	}
	if _, err := call(failed, `{"value":"retry after rollback"}`); err != nil {
		t.Fatal(err)
	}
	// Identity and capability boundaries are part of the request key.
	other := *actor
	other.UserID = 2
	if _, err := call(capability.WithActor(ctx, &other), `{"value":"payload"}`); err != nil {
		t.Fatal(err)
	}
	db.Table("retry_test").Count(&count)
	if count != 3 {
		t.Fatalf("identity boundary: %d", count)
	}
	if err := db.Model(&receipt{}).Where("1=1").Update("expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := call(ctx, `{"value":"payload"}`); err != nil {
		t.Fatal(err)
	}
	db.Table("retry_test").Count(&count)
	if count != 4 {
		t.Fatal("expired request was not executed")
	}
	var rows []receipt
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if len(row.RequestID) != 64 || len(row.InputDigest) != 64 {
			t.Fatal(fmt.Sprint(row))
		}
	}
}

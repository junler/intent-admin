package service

import (
	"context"
	"testing"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/testutil"
)

func TestDemoVersionsAndLatestRows(t *testing.T) {
	db := testutil.Postgres(t)
	if err := db.Exec("UPDATE demo_item SET create_time='2026-01-01T00:00:00Z'").Error; err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.Raw("INSERT INTO demo_item(name,create_time) VALUES('Latest','2026-02-01T00:00:00Z') RETURNING item_id").Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	s := NewDemoService(db)
	ctx := capability.WithRecorder(capability.WithActor(context.Background(), &capability.Actor{UserName: "test"}), capability.NewRecorder())
	rows, err := s.List(ctx, DemoListIn{})
	if err != nil || len(rows.Rows) == 0 || rows.Rows[0].ItemID != id {
		t.Fatalf("newest row hidden: %v / %v", rows, err)
	}
	rows, err = s.List(ctx, DemoListIn{BeginTime: "2026-01-15T00:00:00Z", EndTime: "2026-02-15T00:00:00Z"})
	if err != nil || rows.Total != 1 {
		t.Fatalf("date range: %v / %v", rows, err)
	}
	snapshot, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	name := "Fresh"
	if _, err := s.Update(ctx, DemoUpdateIn{ItemID: id, Version: &snapshot.Version, Name: &name}); err != nil {
		t.Fatal(err)
	}
	name = "Stale"
	if _, err := s.Update(ctx, DemoUpdateIn{ItemID: id, Version: &snapshot.Version, Name: &name}); errcode.From(err).Code != errcode.Conflict {
		t.Fatalf("stale update accepted: %v", err)
	}
	current, err := s.Get(ctx, id)
	if err != nil || current.Name != "Fresh" || current.Version != snapshot.Version+1 {
		t.Fatalf("version guard lost data: %v / %v", current, err)
	}
}

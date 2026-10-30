package notification

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/paginate"
	"github.com/junler/intent-admin/migrations"
)

func TestPrivateNotificationsIntegration(t *testing.T) {
	dsn := os.Getenv("INTENT_ADMIN_NOTIFICATION_DSN")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL integration database")
	}
	if !strings.Contains(dsn, "_integration") {
		t.Fatal("use a disposable _integration database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("schema_migrations")
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.Up(sqlDB, migrations.Dir); err != nil {
		t.Fatal(err)
	}
	var userIDs []int64
	for i := 0; i < 2; i++ {
		var id int64
		if err = db.Raw("INSERT INTO sys_user(user_name,nick_name,password) VALUES (?,?,?) RETURNING user_id", "notification-"+uuid.NewString()[:8], "Notification fixture", "unused-fixture").Scan(&id).Error; err != nil {
			t.Fatal(err)
		}
		userIDs = append(userIDs, id)
	}
	defer db.Exec("DELETE FROM sys_user WHERE user_id IN ?", userIDs)
	defer db.Exec("DELETE FROM sys_notification WHERE user_id IN ?", userIDs)
	svc := New(db)
	ctxFor := func(id int64) context.Context {
		return capability.WithActor(context.Background(), &capability.Actor{UserID: id, Source: capability.SourceHTTP})
	}
	ctx := ctxFor(userIDs[0])
	if err = svc.Publish(ctx, userIDs[0], "收到计划审批", "Review the plan", "/ai/approvals"); err != nil {
		t.Fatal(err)
	}
	if err = svc.Publish(ctx, userIDs[1], "收到计划审批", "Private other user", "/ai/approvals"); err != nil {
		t.Fatal(err)
	}
	rows, total, err := svc.List(ctx, paginate.Query{})
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("recipient isolation: %d %v %v", total, rows, err)
	}
	if err = svc.Read(ctxFor(userIDs[1]), []string{rows[0].ID}); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = svc.List(ctx, paginate.Query{})
	if rows[0].ReadAt != nil {
		t.Fatal("another user marked private notification read")
	}
	if err = svc.Read(ctx, []string{rows[0].ID}); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = svc.List(ctx, paginate.Query{})
	if rows[0].ReadAt == nil {
		t.Fatal("recipient could not mark read")
	}
	rollback := errors.New("workflow rollback")
	err = dbx.NewTxRunner(db)(ctx, func(tx context.Context) error {
		if err := svc.Publish(tx, userIDs[0], "Rollback fixture", "Must disappear", "/ai/approvals"); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	_, total, err = svc.List(ctx, paginate.Query{})
	if err != nil || total != 1 {
		t.Fatalf("rolled-back workflow delivered notification: %d %v", total, err)
	}
	if err = svc.Publish(ctx, userIDs[0], "Invalid link", "", "//evil.example"); err == nil {
		t.Fatal("accepted external notification link")
	}
}

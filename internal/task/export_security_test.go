package task

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
	"github.com/junler/intent-admin/migrations"
	"github.com/pressly/goose/v3"
)

func TestExportAuthorizationRevokedBeforeDelivery(t *testing.T) {
	if os.Getenv("INTENT_ADMIN_TASK_INTEGRATION") != "1" {
		t.Skip("requires isolated PostgreSQL and Redis")
	}
	cfg, err := config.Load("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Name != "intent_tasks_integration" {
		t.Fatal("use disposable intent_tasks_integration database")
	}
	cfg.Redis.DB = 14
	db, err := dbx.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbx.Close(db)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("schema_migrations")
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.Up(sqlDB, migrations.Dir); err != nil {
		t.Fatal(err)
	}
	c, err := cache.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := New(db, c, cfg)
	defer r.Close()
	ctx := context.Background()
	user := model.User{UserName: fmt.Sprintf("export_%d", time.Now().UnixNano()), NickName: "Export fixture", Password: "unused-in-this-fixture", Status: "0", DelFlag: "0", IdentityAudit: model.IdentityAudit{CreateTime: time.Now(), UpdateTime: time.Now()}}
	if err = db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM sys_user WHERE user_id=?", user.UserID)
	for _, perm := range []string{"integration:security:list", "integration:security:export"} {
		if err = db.Exec("INSERT INTO sys_user_perm (user_id,perm) VALUES (?,?)", user.UserID, perm).Error; err != nil {
			t.Fatal(err)
		}
	}
	type result struct {
		Rows  []map[string]any `json:"rows"`
		Total int64            `json:"total"`
	}
	queried := false
	capability.Register(capability.Capability[paginate.Query, result]{Meta: capability.Meta{Name: "integration.exportSecurity", Summary: "导出授权回归测试", Perm: "integration:security:list", Risk: capability.RiskLow, Export: &capability.ExportSpec{SheetName: "数据", FileName: "fixture", Perm: "integration:security:export", Columns: []capability.ExportColumn{{Field: "value", Label: "值"}}}}, Fn: func(context.Context, paginate.Query) (result, error) {
		queried = true
		err := db.WithContext(context.Background()).Exec("DELETE FROM sys_user_perm WHERE user_id=? AND perm='integration:security:export'", user.UserID).Error
		return result{Rows: []map[string]any{{"value": "private row"}}, Total: 1}, err
	}})
	entry, _ := capability.Lookup("integration.exportSecurity")
	if _, err = r.authorizeExport(ctx, user.UserID, entry); err != nil {
		t.Fatalf("valid current grants rejected: %v", err)
	}
	delivered := false
	r.FileWriter = func(context.Context, string, io.Reader, string, string) (map[string]any, error) {
		delivered = true
		return map[string]any{"uploadId": "should-never-be-created"}, nil
	}
	err = r.export(capability.WithActor(ctx, capability.SystemActor("queued with old grant")), exportPayload{Capability: entry.Name, UserID: user.UserID})
	if errcode.From(err).Code != errcode.Forbidden || !queried || delivered {
		t.Fatalf("revoked export: queried=%v delivered=%v err=%v", queried, delivered, err)
	}
	if _, err = r.authorizeExport(ctx, user.UserID, entry); errcode.From(err).Code != errcode.Forbidden {
		t.Fatalf("revoked pending actor retained permission: %v", err)
	}
	if err = db.Model(&model.User{}).Where("user_id=?", user.UserID).Update("status", "1").Error; err != nil {
		t.Fatal(err)
	}
	if _, err = r.authorizeExport(ctx, user.UserID, entry); errcode.From(err).Code != errcode.Forbidden {
		t.Fatalf("disabled user authorized: %v", err)
	}
}

package automation

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/knowledge"
	"github.com/junler/intent-admin/internal/notification"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSQLIntegration(t *testing.T) {
	dsn := os.Getenv("INTENT_ADMIN_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL integration database")
	}
	if !strings.Contains(dsn, "_integration") {
		t.Fatal("use a disposable _integration database")
	}
	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	rootPool, _ := root.DB()
	defer rootPool.Close()
	schema := "ai_sql_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := root.Exec("CREATE SCHEMA " + quote(schema)).Error; err != nil {
		t.Fatal(err)
	}
	defer root.Exec("DROP SCHEMA " + quote(schema) + " CASCADE")
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeParams["search_path"] = schema
	cfg.RuntimeParams["timezone"] = "Asia/Shanghai"
	pool := stdlib.OpenDB(*cfg)
	defer pool.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{"CREATE TABLE sys_user (user_id bigint PRIMARY KEY, user_name text, password text, create_time timestamptz, del_flag text)", "INSERT INTO sys_user VALUES (1,'a','do-not-expose',now(),'0'),(2,'b','another-secret',now(),'0'),(3,'deleted','deleted-secret',now(),'2')"} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	knowledge.CapabilityEntities["fixture.sqlUser"] = "sys_user"
	capability.Register(capability.Capability[capability.Empty, capability.Empty]{Meta: capability.Meta{Name: "fixture.sqlUser.list", Summary: "用户查询", Perm: "fixture:sql", Risk: capability.RiskLow, AIEnabled: true, MCPEnabled: true}, Fn: func(context.Context, capability.Empty) (capability.Empty, error) { return capability.Empty{}, nil }})
	ctx := capability.WithActor(context.Background(), &capability.Actor{UserID: 7, Source: capability.SourceAI, MaxRisk: capability.RiskHigh, Perms: []string{"fixture:sql"}})
	s := &Service{DB: db}
	out, err := s.Query(dbx.WithReadOnly(ctx), SQLInput{SQL: "SELECT count(*) AS count FROM sys_user WHERE create_time >= $1", Params: []any{"2000-01-01"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 1 || out.Rows[0]["count"] != int64(2) {
		t.Fatalf("bad count: %+v", out)
	}
	out, err = s.Query(ctx, SQLInput{SQL: "SELECT * FROM sys_user ORDER BY user_id", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Truncated || len(out.Rows) != 1 {
		t.Fatalf("limit failed: %+v", out)
	}
	if _, ok := out.Rows[0]["password"]; ok {
		t.Fatal("credential included by star")
	}
	if _, err := s.Query(ctx, SQLInput{SQL: "SELECT password AS harmless FROM sys_user"}); err == nil {
		t.Fatal("aliased credential was exposed")
	}
	// SQL queries must join ambient transactions without GORM write-guard
	// failures or leaving search_path changed for the following business write.
	if err := db.Transaction(func(tx *gorm.DB) error {
		txctx := dbx.WithTx(dbx.WithReadOnly(ctx), tx)
		if _, err := s.Query(txctx, SQLInput{SQL: "SELECT user_name FROM sys_user"}); err != nil {
			return err
		}
		return tx.Exec("INSERT INTO sys_user VALUES (4,'ambient','secret',now(),'0')").Error
	}); err != nil {
		t.Fatal(err)
	}
	denied := capability.WithActor(context.Background(), &capability.Actor{Source: capability.SourceAI, MaxRisk: capability.RiskLow})
	if _, err := s.Query(denied, SQLInput{SQL: "SELECT * FROM sys_user"}); err == nil {
		t.Fatal("unpermitted user queried table")
	}
	if err := db.Exec("CREATE TABLE sys_notification (notification_id uuid PRIMARY KEY,user_id bigint,title text,content text,link text,read_at timestamptz,create_time timestamptz)").Error; err != nil {
		t.Fatal(err)
	}
	Register(db, "Asia/Shanghai")
	notification.New(db).RegisterCapabilities()
	capability.Install(capability.Hooks{Tx: dbx.NewTxRunner(db), ReadOnly: dbx.NewReadOnlyRunner()})
	flow := Flow{Steps: []Step{
		{Name: "stats", Capability: "automation.sql.query", Args: map[string]any{"sql": "SELECT count(*) AS count FROM sys_user WHERE create_time >= $1", "params": []any{"${today}"}}},
		{Name: "notify", Capability: "system.notification.send", Args: map[string]any{"title": "统计", "content": "今日新增 ${stats.rows.0.count} 人"}},
	}}
	raw, _ := json.Marshal(flow)
	preview, err := capability.RunPreview(ctx, "automation.flow.run", raw)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Affected != 1 || len(preview.Changes) != 1 {
		t.Fatalf("missing child preview: %+v", preview)
	}
	var count int64
	db.Table("sys_notification").Count(&count)
	if count != 0 {
		t.Fatal("flow preview delivered notification")
	}
	if _, err := capability.InvokeJSON(ctx, "automation.flow.run", raw); err != nil {
		t.Fatal(err)
	}
	var message notification.Message
	if err := db.First(&message).Error; err != nil {
		t.Fatal(err)
	}
	if message.UserID != 7 || message.Content != "今日新增 3 人" {
		t.Fatalf("incorrect dynamic delivery: %+v", message)
	}
	flow.Steps = append(flow.Steps, Step{Name: "fail", Capability: "system.notification.send", Args: map[string]any{"title": "无正文"}})
	raw, _ = json.Marshal(flow)
	if _, err := capability.InvokeJSON(ctx, "automation.flow.run", raw); err == nil {
		t.Fatal("invalid later step succeeded")
	}
	db.Table("sys_notification").Count(&count)
	if count != 1 {
		t.Fatalf("workflow failure did not roll back prior write: %d", count)
	}
}

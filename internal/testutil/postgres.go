// Package testutil provides isolated, opt-in database fixtures.
package testutil

import (
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/junler/intent-admin/migrations"
)

func Postgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("INTENT_ADMIN_TEST_DSN")
	if dsn == "" {
		t.Skip("requires INTENT_ADMIN_TEST_DSN in a disposable _integration database")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cfg.Database, "_integration") {
		t.Fatal("disposable _integration database required")
	}
	admin := stdlib.OpenDB(*cfg)
	schema := "foundation_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE"); _ = admin.Close() })
	cfg.RuntimeParams["search_path"] = schema
	pool := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("schema_migrations")
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(pool, migrations.Dir); err != nil {
		t.Fatal(err)
	}
	return db
}

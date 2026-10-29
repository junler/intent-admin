package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Seed fills in missing initial records without replacing existing settings,
// menu definitions or credentials. The same SQL also runs in migration 00003.
// Call after migrating up; the transaction makes an explicit rerun atomic.
func Seed(ctx context.Context, db *sql.DB) error {
	raw, err := FS.ReadFile("00003_seed.sql")
	if err != nil {
		return fmt.Errorf("读取种子数据失败: %w", err)
	}
	up, _, ok := strings.Cut(string(raw), "-- +goose Down")
	if !ok {
		return fmt.Errorf("种子迁移缺少 Down 标记")
	}
	up = strings.TrimPrefix(up, "-- +goose Up")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始种子事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, up); err != nil {
		return fmt.Errorf("写入种子数据失败（请先运行 migrate up）: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交种子数据失败: %w", err)
	}
	return nil
}

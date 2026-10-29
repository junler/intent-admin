// Package dbx owns the database handle, the ambient-transaction context and
// the dry-run write guard.
//
// It sits below service so that service code can join the executor's
// transaction without importing bootstrap — which would create an import
// cycle (bootstrap -> feature -> service -> bootstrap).
package dbx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/junler/intent-admin/internal/config"
)

// readOnlyKey marks a context as being inside a dry run.
type readOnlyKey struct{}

// ErrReadOnly is returned when a write is attempted during a dry run.
var ErrReadOnly = errors.New("当前处于预演（dry-run）上下文，禁止写入数据库；DryRun 必须是只读的")

// WithReadOnly marks ctx as read-only. Every GORM write issued under it is
// rejected by the guard callbacks.
func WithReadOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, readOnlyKey{}, true)
}

// IsReadOnly reports whether ctx is a dry-run context.
func IsReadOnly(ctx context.Context) bool {
	v, ok := ctx.Value(readOnlyKey{}).(bool)
	return ok && v
}

// OpenDB connects to PostgreSQL and installs the dry-run write guard.
func Open(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DB.DSN()), &gorm.Config{
		Logger:                 logger.Default.LogMode(gormLogLevel(cfg.DB.LogLevel)),
		SkipDefaultTransaction: true,
		// The project uses explicit column names everywhere (plan §9), so the
		// default pluralising namer is kept but table names are declared on
		// the models themselves.
		NowFunc: func() time.Time { return time.Now().Local() },
	})
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接池失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.DB.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.DB.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("数据库不可用: %w", err)
	}

	if err := installReadOnlyGuard(db); err != nil {
		return nil, err
	}
	return db, nil
}

// installReadOnlyGuard rejects writes issued from a dry-run context.
//
// The contract "DryRun must not mutate anything" cannot be enforced by review
// alone — a helper that accidentally calls Save would silently corrupt
// production data while reporting a tidy preview. This turns that class of
// mistake into a loud, immediate failure.
func installReadOnlyGuard(db *gorm.DB) error {
	reject := func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Context == nil {
			return
		}
		if IsReadOnly(tx.Statement.Context) {
			_ = tx.AddError(ErrReadOnly)
		}
	}
	type registration struct {
		name     string
		register func(fn func(*gorm.DB)) error
	}
	regs := []registration{
		{"intent_admin:readonly_create", func(fn func(*gorm.DB)) error {
			return db.Callback().Create().Before("gorm:create").Register("intent_admin:readonly_create", fn)
		}},
		{"intent_admin:readonly_update", func(fn func(*gorm.DB)) error {
			return db.Callback().Update().Before("gorm:update").Register("intent_admin:readonly_update", fn)
		}},
		{"intent_admin:readonly_delete", func(fn func(*gorm.DB)) error {
			return db.Callback().Delete().Before("gorm:delete").Register("intent_admin:readonly_delete", fn)
		}},
		{"intent_admin:readonly_raw", func(fn func(*gorm.DB)) error {
			return db.Callback().Raw().Before("gorm:raw").Register("intent_admin:readonly_raw", fn)
		}},
	}
	for _, r := range regs {
		if err := r.register(reject); err != nil {
			return fmt.Errorf("注册只读护栏 %s 失败: %w", r.name, err)
		}
	}
	return nil
}

// txKey carries the ambient transaction so service code can pick it up from
// the context instead of threading *gorm.DB through every signature.
type txKey struct{}

// WithTx returns a context carrying tx.
func WithTx(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// DBFrom returns the ambient transaction if one is active, otherwise base.
// Service code always calls this, so the same function works inside and
// outside a transaction.
func DBFrom(ctx context.Context, base *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok && tx != nil {
		return tx.WithContext(ctx)
	}
	return base.WithContext(ctx)
}

// NewTxRunner returns the executor's Tx hook: run fn inside a transaction,
// with the transaction published on the context.
//
// The business write and its changeset row commit together. An audit trail
// that can diverge from the data it describes is worse than no audit trail,
// because it invites false confidence.
type afterCommitKey struct{}
type commitQueue struct {
	callbacks []func() error
	rollback  []func(error)
}

// AfterCommit defers cache invalidation and external side effects until the
// outer transaction commits. Outside a managed transaction it runs immediately.
func AfterCommit(ctx context.Context, fn func()) {
	_ = AfterCommitE(ctx, func() error { fn(); return nil })
}

// AfterCommitE defers a fallible side effect until commit, or executes it
// immediately outside a managed transaction. A queued callback's error is
// returned by the outer NewTxRunner after all committed callbacks are drained.
func AfterCommitE(ctx context.Context, fn func() error) error {
	if q, ok := ctx.Value(afterCommitKey{}).(*commitQueue); ok {
		q.callbacks = append(q.callbacks, fn)
		return nil
	}
	return fn()
}

// AfterRollback registers a notification for a managed transaction that is
// rolled back, including a later rollback of its outer transaction.
func AfterRollback(ctx context.Context, fn func(error)) {
	if q, ok := ctx.Value(afterCommitKey{}).(*commitQueue); ok {
		q.rollback = append(q.rollback, fn)
	}
}

func NewTxRunner(db *gorm.DB) func(context.Context, func(context.Context) error) error {
	return func(ctx context.Context, fn func(context.Context) error) error {
		parent, nested := ctx.Value(afterCommitKey{}).(*commitQueue)
		queue := &commitQueue{}
		committed, notified := false, false
		defer func() {
			if recovered := recover(); recovered != nil {
				if !committed && !notified {
					notified = true
					cause := fmt.Errorf("事务因异常回滚")
					for _, callback := range queue.rollback {
						callback(cause)
					}
				}
				panic(recovered)
			}
		}()
		err := DBFrom(ctx, db).Transaction(func(tx *gorm.DB) error {
			txCtx := context.WithValue(WithTx(ctx, tx), afterCommitKey{}, queue)
			return fn(txCtx)
		})
		if err != nil {
			notified = true
			for _, callback := range queue.rollback {
				callback(err)
			}
			return err
		}
		committed = true
		if nested {
			parent.callbacks = append(parent.callbacks, queue.callbacks...)
			parent.rollback = append(parent.rollback, queue.rollback...)
		} else {
			var committedError error
			for _, callback := range queue.callbacks {
				if callbackError := callback(); callbackError != nil && committedError == nil {
					committedError = callbackError
				}
			}
			if committedError != nil {
				return fmt.Errorf("数据库事务已提交，但提交后处理失败: %w", committedError)
			}
		}
		return nil
	}
}

// NewReadOnlyRunner returns the executor's ReadOnly hook.
func NewReadOnlyRunner() func(context.Context, func(context.Context) error) error {
	return func(ctx context.Context, fn func(context.Context) error) error {
		return fn(WithReadOnly(ctx))
	}
}

func gormLogLevel(level string) logger.LogLevel {
	switch level {
	case "silent":
		return logger.Silent
	case "error":
		return logger.Error
	case "info":
		return logger.Info
	default:
		return logger.Warn
	}
}

// CloseDB releases the connection pool.
func Close(db *gorm.DB) {
	if db == nil {
		return
	}
	sqlDB, err := db.DB()
	if err != nil {
		return
	}
	if err := sqlDB.Close(); err != nil {
		slog.Warn("关闭数据库连接失败", "err", err)
	}
}

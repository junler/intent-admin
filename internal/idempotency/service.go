// Package idempotency commits successful request receipts with business writes.
package idempotency

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type receipt struct {
	RequestID   string `gorm:"primaryKey"`
	InputDigest string
	Result      json.RawMessage `gorm:"type:jsonb"`
	ExpiresAt   time.Time
}

func (receipt) TableName() string { return "sys_request_receipt" }

type Service struct {
	db     *gorm.DB
	secret []byte
}

func New(db *gorm.DB, secret string) *Service { return &Service{db, []byte(secret)} }
func (s *Service) digest(value []byte) string {
	h := hmac.New(sha256.New, s.secret)
	_, _ = h.Write(value)
	return hex.EncodeToString(h.Sum(nil))
}

// Run must run inside the executor transaction. The transaction-scoped lock
// serializes concurrent retries across replicas; rollback also removes receipts.
// Only explicitly opted-in capabilities with credential-free outputs use it.
func (s *Service) Run(ctx context.Context, e *capability.Entry, a *capability.Actor, in any, next func(context.Context) (any, error)) (any, error) {
	key := capability.RequestKey(ctx)
	if len(key) < 8 || len(key) > 128 {
		return nil, errcode.BadRequestf("Idempotency-Key 需要 8–128 个字符")
	}
	db := dbx.DBFrom(ctx, s.db)
	if _, ok := db.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return nil, errcode.Internalf("请求去重必须在业务事务内执行")
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	id := s.digest([]byte(fmt.Sprintf("%d/%s/%d/%s/%s", a.UserID, a.Source, a.TokenID, e.Name, key)))
	digest := s.digest(raw)
	if err := db.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", id).Error; err != nil {
		return nil, err
	}
	var old receipt
	err = db.Where("request_id = ?", id).Take(&old).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err == nil && old.ExpiresAt.After(time.Now()) {
		if old.InputDigest != digest {
			return nil, errcode.Conflictf("同一个请求编号不能用于不同的参数")
		}
		return old.Result, nil
	}
	out, err := next(capability.WithRequestKey(ctx, ""))
	if err != nil {
		return nil, err
	}
	result, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if len(result) > 1024*1024 {
		return nil, errcode.BadRequestf("请求结果过大，无法保存重试凭据")
	}
	row := receipt{RequestID: id, InputDigest: digest, Result: result, ExpiresAt: time.Now().Add(24 * time.Hour)}
	if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
		return nil, err
	}
	// Bound expiry cleanup so receipts do not grow indefinitely on active systems.
	if err := db.Exec("DELETE FROM sys_request_receipt WHERE request_id IN (SELECT request_id FROM sys_request_receipt WHERE expires_at < now() ORDER BY expires_at LIMIT 100 FOR UPDATE SKIP LOCKED)").Error; err != nil {
		return nil, err
	}
	return out, nil
}

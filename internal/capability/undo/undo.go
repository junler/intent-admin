// Package undo provides generic compensation for ordinary CRUD, the mirror of
// the dryrun package.
//
// The changeset stores, per row, the fields that changed and their previous
// values. Undo replays those values back. Because every write — human or AI —
// goes through the same executor, this makes "undo" work for a mis-click in
// the UI just as well as for a bad AI plan.
package undo

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

// Payload is the compensation data stored on each changeset item. It is built
// by the changeset layer from the Recorder output, so capabilities using the
// generic helpers never construct it by hand.
type Payload struct {
	Entity string         `json:"entity"`
	ID     string         `json:"id"`
	Op     capability.Op  `json:"op"`
	Before map[string]any `json:"before,omitempty"`
	After  map[string]any `json:"after,omitempty"`
}

// Table resolves the physical table for a payload, guarding against a payload
// pointing at a table this capability has no business touching.
type Table struct {
	Name    string
	IDField string
}

func (t Table) idField() string {
	if t.IDField != "" {
		return t.IDField
	}
	return "id"
}

// RestoreFieldsFn returns an Undo function that writes the recorded `before`
// values back onto the row. Suitable for update-shaped capabilities.
func RestoreFieldsFn(db *gorm.DB, table Table) func(context.Context, json.RawMessage) error {
	return func(ctx context.Context, raw json.RawMessage) error {
		p, err := decode(raw)
		if err != nil {
			return err
		}
		if err := p.expect(table.Name, capability.OpUpdate); err != nil {
			return err
		}
		if len(p.Before) == 0 {
			return errcode.Internalf("回滚数据缺少原值，无法恢复 %s#%s", p.Entity, p.ID)
		}
		res := db.WithContext(ctx).Table(table.Name).
			Where(table.idField()+" = ?", p.ID).
			Updates(p.Before)
		if res.Error != nil {
			return errcode.Internalf("恢复 %s#%s 失败", p.Entity, p.ID).Wrap(res.Error)
		}
		if res.RowsAffected == 0 {
			// The row is gone. Report it rather than claiming success: a
			// partially restored changeset must be visible to the operator.
			return errcode.Conflictf("%s#%s 已不存在，无法恢复", p.Entity, p.ID)
		}
		return nil
	}
}

// RestoreSoftDeletedFn returns an Undo function that clears the soft-delete
// flag, for capabilities that delete by setting del_flag.
func RestoreSoftDeletedFn(db *gorm.DB, table Table, flagField, aliveValue string) func(context.Context, json.RawMessage) error {
	return func(ctx context.Context, raw json.RawMessage) error {
		p, err := decode(raw)
		if err != nil {
			return err
		}
		if err := p.expect(table.Name, capability.OpDelete); err != nil {
			return err
		}
		res := db.WithContext(ctx).Table(table.Name).
			Where(table.idField()+" = ?", p.ID).
			Update(flagField, aliveValue)
		if res.Error != nil {
			return errcode.Internalf("恢复 %s#%s 失败", p.Entity, p.ID).Wrap(res.Error)
		}
		if res.RowsAffected == 0 {
			return errcode.Conflictf("%s#%s 已被物理删除，无法恢复", p.Entity, p.ID)
		}
		return nil
	}
}

// DeleteCreatedFn returns an Undo function that removes a row created by the
// capability. It hard-deletes, because the row did not exist before.
func DeleteCreatedFn(db *gorm.DB, table Table) func(context.Context, json.RawMessage) error {
	return func(ctx context.Context, raw json.RawMessage) error {
		p, err := decode(raw)
		if err != nil {
			return err
		}
		if err := p.expect(table.Name, capability.OpCreate); err != nil {
			return err
		}
		if err := db.WithContext(ctx).Table(table.Name).
			Where(table.idField()+" = ?", p.ID).
			Delete(nil).Error; err != nil {
			return errcode.Internalf("撤销新增 %s#%s 失败", p.Entity, p.ID).Wrap(err)
		}
		return nil
	}
}

// Auto dispatches on the recorded operation, covering capabilities that mix
// creates, updates and soft deletes in one call.
func Auto(db *gorm.DB, table Table, flagField, aliveValue string) func(context.Context, json.RawMessage) error {
	restore := RestoreFieldsFn(db, table)
	undelete := RestoreSoftDeletedFn(db, table, flagField, aliveValue)
	remove := DeleteCreatedFn(db, table)
	return func(ctx context.Context, raw json.RawMessage) error {
		p, err := decode(raw)
		if err != nil {
			return err
		}
		switch p.Op {
		case capability.OpUpdate:
			return restore(ctx, raw)
		case capability.OpDelete:
			return undelete(ctx, raw)
		case capability.OpCreate:
			return remove(ctx, raw)
		case capability.OpSkip:
			return nil
		default:
			return errcode.Internalf("未知的变更类型 %q，无法回滚", p.Op)
		}
	}
}

func decode(raw json.RawMessage) (*Payload, error) {
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errcode.Internalf("回滚数据格式错误").Wrap(err)
	}
	if p.ID == "" {
		return nil, errcode.Internalf("回滚数据缺少记录 ID")
	}
	return &p, nil
}

// expect guards against a changeset item being replayed against the wrong
// table or with the wrong operation — a corrupted or hand-edited changeset
// must not become a way to write arbitrary rows.
func (p *Payload) expect(table string, op capability.Op) error {
	if p.Entity != "" && p.Entity != table {
		return errcode.Internalf("回滚数据属于 %s，不能用于 %s", p.Entity, table)
	}
	if p.Op != op {
		return errcode.Internalf("回滚数据的操作类型是 %s，期望 %s", p.Op, op)
	}
	return nil
}

// Describe renders a payload for logs and error messages.
func (p *Payload) Describe() string {
	return fmt.Sprintf("%s#%s(%s)", p.Entity, p.ID, p.Op)
}

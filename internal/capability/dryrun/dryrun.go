// Package dryrun provides generic preview builders for ordinary CRUD.
//
// Writing a DryRun by hand for every mutating capability is the main
// adoption risk of the preview/rollback design: if it is tedious, developers
// will skip it or get it wrong, and the guarantee the whole system rests on
// quietly erodes. These helpers bring the typical case down to three or four
// lines, so the correct path is also the easy one.
//
// All helpers are read-only. They run inside the executor's write-rejecting
// session, so a mistake here fails loudly rather than mutating data.
package dryrun

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

// LabelOpt controls how rows are described to the user and how values are
// rendered in the diff.
type LabelOpt struct {
	// Entity is the table name recorded on each change; defaults to the
	// model's GORM table name.
	Entity string
	// IDField is the primary key column name. Defaults to "id".
	IDField string
	// LabelField names the column used as the human-readable identifier, e.g.
	// "user_name". A diff that says "#3" is much less reviewable than one that
	// says "test01（张三）".
	LabelField string
	// LabelFunc overrides LabelField with arbitrary formatting.
	LabelFunc func(row map[string]any) string
	// FieldLabels maps column names to Chinese labels shown in the diff.
	FieldLabels map[string]string
	// DictFields maps column names to dictionary types, so the UI can render
	// "正常 → 停用" instead of "0 → 1".
	DictFields map[string]string
	// Route and Filters build the "go and check" evidence link.
	Route   string
	Filters map[string]any
	// SkipFunc marks rows the operation will deliberately leave alone,
	// returning a reason. Superadmin protection is expressed here.
	SkipFunc func(row map[string]any) (skip bool, reason string)
}

func (o LabelOpt) idField() string {
	if o.IDField != "" {
		return o.IDField
	}
	return "id"
}

func (o LabelOpt) label(row map[string]any) string {
	if o.LabelFunc != nil {
		return o.LabelFunc(row)
	}
	if o.LabelField != "" {
		if v, ok := row[o.LabelField]; ok && v != nil {
			return fmt.Sprint(v)
		}
	}
	return ""
}

func (o LabelOpt) evidence() *capability.Evidence {
	if o.Route == "" {
		return nil
	}
	return &capability.Evidence{Route: o.Route, Filters: o.Filters}
}

// fetch loads the affected rows as maps so the helpers do not need to know the
// model's field layout.
func fetch(ctx context.Context, db *gorm.DB, model any, ids []any, opt LabelOpt) ([]map[string]any, string, error) {
	tx := db.WithContext(ctx).Model(model)
	entity := opt.Entity
	if entity == "" {
		stmt := tx.Statement
		if err := stmt.Parse(model); err == nil {
			entity = stmt.Table
		}
	}
	var rows []map[string]any
	if err := tx.Where(opt.idField()+" IN ?", ids).Find(&rows).Error; err != nil {
		return nil, entity, errcode.Internalf("预演查询失败").Wrap(err)
	}
	return rows, entity, nil
}

// ForUpdate previews applying patch to the rows identified by ids.
func ForUpdate(ctx context.Context, db *gorm.DB, model any, ids []any, patch map[string]any, opt LabelOpt) (*capability.Preview, error) {
	if len(ids) == 0 {
		return nil, errcode.BadRequestf("未指定要修改的记录")
	}
	rows, entity, err := fetch(ctx, db, model, ids, opt)
	if err != nil {
		return nil, err
	}

	p := &capability.Preview{Evidence: opt.evidence()}
	found := make(map[string]bool, len(rows))

	for _, row := range rows {
		id := capability.ID(row[opt.idField()])
		found[id] = true
		change := capability.Change{Entity: entity, ID: id, Label: opt.label(row), Op: capability.OpUpdate}

		if opt.SkipFunc != nil {
			if skip, reason := opt.SkipFunc(row); skip {
				change.Op = capability.OpSkip
				change.Reason = reason
				p.AddChange(change)
				p.Warn(reason)
				continue
			}
		}

		for field, after := range patch {
			before := row[field]
			if equal(before, after) {
				continue
			}
			change.Fields = append(change.Fields, diff(field, before, after, opt))
		}
		// A row whose values already match is reported as a skip rather than
		// silently counted as affected: "4 changed" should mean four rows
		// actually change.
		if len(change.Fields) == 0 {
			change.Op = capability.OpSkip
			change.Reason = "值未发生变化"
		}
		p.AddChange(change)
	}

	for _, id := range ids {
		if !found[capability.ID(id)] {
			p.AddChange(capability.Change{
				Entity: entity, ID: capability.ID(id), Op: capability.OpSkip,
				Reason: "记录不存在或已删除",
			})
			p.Warn("部分记录不存在或已删除")
		}
	}

	p.Summary = fmt.Sprintf("将修改 %d 条记录", p.Affected)
	if p.Skipped > 0 {
		p.Summary += fmt.Sprintf("，跳过 %d 条", p.Skipped)
	}
	return p, nil
}

// ForDelete previews deleting the rows identified by ids.
func ForDelete(ctx context.Context, db *gorm.DB, model any, ids []any, opt LabelOpt) (*capability.Preview, error) {
	if len(ids) == 0 {
		return nil, errcode.BadRequestf("未指定要删除的记录")
	}
	rows, entity, err := fetch(ctx, db, model, ids, opt)
	if err != nil {
		return nil, err
	}

	p := &capability.Preview{Evidence: opt.evidence()}
	found := make(map[string]bool, len(rows))

	for _, row := range rows {
		id := capability.ID(row[opt.idField()])
		found[id] = true
		change := capability.Change{Entity: entity, ID: id, Label: opt.label(row), Op: capability.OpDelete}
		if opt.SkipFunc != nil {
			if skip, reason := opt.SkipFunc(row); skip {
				change.Op = capability.OpSkip
				change.Reason = reason
				p.AddChange(change)
				p.Warn(reason)
				continue
			}
		}
		// Show the identifying fields so the user can tell what disappears,
		// not just how many rows.
		for field, label := range opt.FieldLabels {
			if v, ok := row[field]; ok {
				change.Fields = append(change.Fields, capability.FieldDiff{
					Field: field, Label: label, Before: v, After: nil,
					BeforeText: dictText(field, v, opt),
				})
			}
		}
		p.AddChange(change)
	}

	for _, id := range ids {
		if !found[capability.ID(id)] {
			p.AddChange(capability.Change{
				Entity: entity, ID: capability.ID(id), Op: capability.OpSkip,
				Reason: "记录不存在或已删除",
			})
			p.Warn("部分记录不存在或已删除")
		}
	}

	p.Summary = fmt.Sprintf("将删除 %d 条记录", p.Affected)
	if p.Skipped > 0 {
		p.Summary += fmt.Sprintf("，跳过 %d 条", p.Skipped)
	}
	return p, nil
}

// ForCreate previews inserting one record described by fields.
func ForCreate(entity string, fields map[string]any, opt LabelOpt) *capability.Preview {
	change := capability.Change{Entity: entity, Op: capability.OpCreate, Label: opt.label(fields)}
	for field, after := range fields {
		if capability.IsSensitive(field) {
			continue
		}
		change.Fields = append(change.Fields, diff(field, nil, after, opt))
	}
	p := &capability.Preview{Summary: "将新增 1 条记录", Evidence: opt.evidence()}
	p.AddChange(change)
	return p
}

// ForCount previews an operation whose effect is only a row count, such as
// purging logs older than N days. There is nothing per-row worth showing, but
// the magnitude matters a great deal.
func ForCount(entity string, count int64, summary string, reversible bool) *capability.Preview {
	p := &capability.Preview{
		Summary:  summary,
		Affected: int(count),
	}
	if !reversible {
		p.Warn("此操作不可回滚，请确认后再执行")
	}
	if count == 0 {
		p.Summary = summary + "（没有符合条件的记录）"
	}
	_ = entity
	return p
}

func diff(field string, before, after any, opt LabelOpt) capability.FieldDiff {
	return capability.FieldDiff{
		Field:      field,
		Label:      opt.FieldLabels[field],
		Before:     before,
		After:      after,
		BeforeText: dictText(field, before, opt),
		AfterText:  dictText(field, after, opt),
	}
}

// dictText renders the dictionary type alongside the raw value. The actual
// label lookup happens in the frontend, which already caches dictionaries;
// shipping the dictionary type keeps the backend from duplicating that cache.
func dictText(field string, v any, opt LabelOpt) string {
	dict, ok := opt.DictFields[field]
	if !ok || v == nil {
		return ""
	}
	return dict + ":" + fmt.Sprint(v)
}

// equal compares a database value with a patch value. Numeric types coming out
// of a map scan rarely match the Go literal type in the patch, so comparison
// goes through the string form.
func equal(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

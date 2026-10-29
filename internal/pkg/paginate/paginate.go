// Package paginate holds the shared pagination/sorting input and the GORM
// scopes that apply it. Sort columns must be whitelisted per capability: the
// column name reaches SQL, so an unchecked value is an injection vector.
package paginate

import (
	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/pkg/errcode"
)

const (
	DefaultPageNum  = 1
	DefaultPageSize = 10
	MaxPageSize     = 100
	// ExportPageSize is substituted for PageSize when a list capability is
	// invoked through the generated /export endpoint.
	ExportPageSize = 50000
)

// Query is embedded into list capability inputs.
type Query struct {
	PageNum       int    `json:"pageNum"       form:"pageNum"       jsonschema:"页码，从 1 开始，默认 1"`
	PageSize      int    `json:"pageSize"      form:"pageSize"      jsonschema:"每页条数，默认 10，上限 100"`
	OrderByColumn string `json:"orderByColumn" form:"orderByColumn" jsonschema:"排序字段，必须是该功能允许的字段之一"`
	IsAsc         string `json:"isAsc"         form:"isAsc"         jsonschema:"排序方向：asc 或 desc"`

	// Unbounded raises the page-size ceiling to ExportPageSize. It is set only
	// by the export adapter and is excluded from JSON, form and schema
	// binding, so a client cannot ask for an unbounded query and turn any list
	// endpoint into a full-table dump.
	Unbounded bool `json:"-" form:"-" jsonschema:"-"`
}

// Normalize clamps the pagination values into range and returns offset/limit.
func (q *Query) Normalize() (offset, limit int) {
	if q.PageNum < 1 {
		q.PageNum = DefaultPageNum
	}
	ceiling := MaxPageSize
	if q.Unbounded {
		ceiling = ExportPageSize
	}
	switch {
	case q.PageSize < 1:
		q.PageSize = DefaultPageSize
	case q.PageSize > ceiling:
		q.PageSize = ceiling
	}
	return (q.PageNum - 1) * q.PageSize, q.PageSize
}

// Scope returns a GORM scope applying LIMIT/OFFSET.
func (q *Query) Scope() func(*gorm.DB) *gorm.DB {
	offset, limit := q.Normalize()
	return func(db *gorm.DB) *gorm.DB { return db.Offset(offset).Limit(limit) }
}

// OrderScope returns a GORM scope applying ORDER BY, validated against allowed.
// allowed maps the API-facing column name to the physical column name, so the
// public contract does not leak the schema. An unknown column is rejected
// rather than silently ignored — a typo in a sort field should be visible.
func (q *Query) OrderScope(allowed map[string]string, fallback string) (func(*gorm.DB) *gorm.DB, error) {
	column := fallback
	if q.OrderByColumn != "" {
		mapped, ok := allowed[q.OrderByColumn]
		if !ok {
			return nil, errcode.BadRequestf("不支持按 %s 排序", q.OrderByColumn)
		}
		column = mapped
	}
	if column == "" {
		return func(db *gorm.DB) *gorm.DB { return db }, nil
	}
	direction := "DESC"
	if q.IsAsc == "asc" || q.IsAsc == "ASC" {
		direction = "ASC"
	}
	order := column + " " + direction
	return func(db *gorm.DB) *gorm.DB { return db.Order(order) }, nil
}

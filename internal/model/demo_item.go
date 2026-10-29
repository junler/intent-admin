package model

import "time"

// Soft-delete flag values used across all tables.
const (
	DelFlagAlive   = "0"
	DelFlagDeleted = "2"
)

// Status values used across all tables.
const (
	StatusNormal   = "0"
	StatusDisabled = "1"
)

// DemoItem backs the demo capabilities that validate the Capability layer end
// to end. It is intentionally boring: one of every field shape the generic
// dry-run and undo helpers must cope with.
//
// Delete together with internal/feature/demo once the real modules land.
type DemoItem struct {
	ItemID   int64  `gorm:"column:item_id;primaryKey;autoIncrement"`
	Name     string `gorm:"column:name"`
	Category string `gorm:"column:category"`
	// Amount is integer cents. Money never becomes a float anywhere in the
	// backend; only the presentation layer divides by 100.
	Amount int64  `gorm:"column:amount"`
	Status string `gorm:"column:status"`
	// DelFlag drives soft deletion. A custom flag is used rather than GORM's
	// DeletedAt because the whole schema shares this convention and audit
	// queries need a plain comparable column.
	DelFlag    string    `gorm:"column:del_flag"`
	Remark     *string   `gorm:"column:remark"`
	CreateBy   string    `gorm:"column:create_by"`
	CreateTime time.Time `gorm:"column:create_time"`
	UpdateBy   string    `gorm:"column:update_by"`
	UpdateTime time.Time `gorm:"column:update_time"`
}

// TableName pins the physical table name; the default pluraliser would guess
// "demo_items".
func (DemoItem) TableName() string { return "demo_item" }

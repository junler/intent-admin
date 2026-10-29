package demo

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/capability/undo"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

// CreateInput is shared by manual and spreadsheet creation. Amount is cents.
type CreateInput struct {
	Name     string `json:"name" validate:"required,max=100"`
	Category string `json:"category" validate:"max=32"`
	Amount   int64  `json:"amount" validate:"gte=0"`
	Status   string `json:"status" validate:"omitempty,oneof=0 1"`
	Remark   string `json:"remark" validate:"max=500"`
}

func registerCreate(db *gorm.DB) {
	plan := func(ctx context.Context, in CreateInput) (*capability.Preview, error) {
		if strings.TrimSpace(in.Name) == "" || len([]rune(in.Name)) > 100 || len([]rune(in.Category)) > 32 || len([]rune(in.Remark)) > 500 || in.Amount < 0 || (in.Status != "" && in.Status != "0" && in.Status != "1") {
			return nil, errcode.BadRequestf("名称、分类、金额、状态或备注格式不正确")
		}
		var count int64
		if err := dbx.DBFrom(ctx, db).Model(&model.DemoItem{}).Where("name=? AND category=? AND del_flag='0'", in.Name, in.Category).Count(&count).Error; err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, errcode.Conflictf("同分类下名称已存在")
		}
		if in.Status == "" {
			in.Status = "0"
		}
		p := &capability.Preview{Summary: "新增示例条目", Evidence: &capability.Evidence{Route: route}}
		c := capability.Change{Entity: table, Op: capability.OpCreate, Label: in.Name}
		for _, f := range []struct {
			key   string
			value any
		}{{"name", in.Name}, {"category", in.Category}, {"amount", in.Amount}, {"status", in.Status}, {"remark", in.Remark}} {
			label := fieldLabels[f.key]
			if f.key == "amount" {
				label = "金额（分）"
			}
			c.Fields = append(c.Fields, capability.FieldDiff{Field: f.key, Label: label, After: f.value})
		}
		p.AddChange(c)
		return p, nil
	}
	capability.Register(capability.Capability[CreateInput, map[string]any]{
		Meta:   capability.Meta{Name: "demo.item.create", Summary: "新增示例条目", Tags: []string{"示例"}, Perm: "demo:item:edit", Risk: capability.RiskMedium, Mutating: true, Reversible: true, HTTP: &capability.HTTPBinding{Method: "POST", Path: route}, AIEnabled: true, MCPEnabled: true, BizType: capability.BizCreate, LogTitle: "示例条目", Examples: []capability.Example{{Utterance: "新增一个示例条目", Args: `{"name":"示例","category":"alpha","amount":100}`}}},
		DryRun: plan,
		Fn: func(ctx context.Context, in CreateInput) (map[string]any, error) {
			if _, err := plan(ctx, in); err != nil {
				return nil, err
			}
			if in.Status == "" {
				in.Status = "0"
			}
			a, _ := capability.ActorFrom(ctx)
			now := time.Now()
			row := model.DemoItem{Name: in.Name, Category: in.Category, Amount: in.Amount, Status: in.Status, DelFlag: "0", Remark: &in.Remark, CreateBy: a.UserName, UpdateBy: a.UserName, CreateTime: now, UpdateTime: now}
			if err := dbx.DBFrom(ctx, db).Create(&row).Error; err != nil {
				return nil, err
			}
			capability.RecorderFrom(ctx).Create(table, capability.ID(row.ItemID), in.Name, map[string]any{"name": in.Name, "category": in.Category, "amount": in.Amount, "status": in.Status, "remark": in.Remark})
			return map[string]any{"itemId": row.ItemID}, nil
		},
		Undo: undo.Auto(db, undo.Table{Name: table, IDField: "item_id"}, "del_flag", "0"),
	})
}

// Package demo registers the capabilities that validate the Capability layer
// end to end: list with export, partial update with preview and undo, soft
// delete with preview and undo.
//
// It is also the worked example referenced by CONTRIBUTING.md — adding a real
// module looks exactly like this. Delete the package once the system modules
// (plan Phase 3) are in place.
package demo

import (
	"context"

	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/capability/dryrun"
	"github.com/junler/intent-admin/internal/capability/undo"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/service"
)

const (
	table = "demo_item"
	route = "/demo/item"
)

var fieldLabels = map[string]string{
	"name":     "名称",
	"category": "分类",
	"amount":   "金额",
	"status":   "状态",
	"remark":   "备注",
}

var dictFields = map[string]string{
	"status": "sys_normal_disable",
}

// Register wires the demo capabilities. Called from bootstrap.
func Register(db *gorm.DB) {
	registerCreate(db)
	svc := service.NewDemoService(db)
	tbl := undo.Table{Name: table, IDField: "item_id"}

	capability.Register(capability.Capability[service.DemoListIn, service.DemoListOut]{
		Meta: capability.Meta{
			Name:       "demo.item.list",
			Summary:    "查询示例条目",
			Tags:       []string{"示例"},
			Perm:       "demo:item:list",
			Risk:       capability.RiskLow,
			HTTP:       &capability.HTTPBinding{Method: "GET", Path: route + "/list"},
			AIEnabled:  true,
			MCPEnabled: true,
			LogTitle:   "示例条目",
			BizType:    capability.BizOther,
			Examples: []capability.Example{
				{Utterance: "有哪些示例条目", Args: `{}`},
				{Utterance: "查一下 beta 分类下停用的条目", Args: `{"category":"beta","status":"1"}`},
			},
			Export: &capability.ExportSpec{
				SheetName: "示例条目",
				FileName:  "示例条目_{date}",
				Perm:      "demo:item:export",
				Columns: []capability.ExportColumn{
					{Field: "itemId", Label: "编号", Width: 10},
					{Field: "name", Label: "名称", Width: 24},
					{Field: "category", Label: "分类", Width: 14},
					{Field: "amount", Label: "金额", Width: 14, Format: capability.FormatMoney},
					{Field: "status", Label: "状态", Width: 10, Dict: "sys_normal_disable"},
					{Field: "remark", Label: "备注", Width: 30},
					{Field: "createTime", Label: "创建时间", Width: 20, Format: capability.FormatDateTime},
				},
			},
		},
		Fn: svc.List,
	})

	capability.Register(capability.Capability[service.DemoUpdateIn, capability.Empty]{
		Meta: capability.Meta{
			Name:       "demo.item.update",
			Summary:    "修改示例条目",
			Tags:       []string{"示例"},
			Perm:       "demo:item:edit",
			Risk:       capability.RiskMedium,
			Mutating:   true,
			Reversible: true,
			Idempotent: true,
			HTTP:       &capability.HTTPBinding{Method: "PUT", Path: route + "/update/:itemId"},
			AIEnabled:  true,
			MCPEnabled: true,
			LogTitle:   "示例条目",
			BizType:    capability.BizUpdate,
			Examples: []capability.Example{
				{Utterance: "把 3 号条目停用", Args: `{"itemId":3,"status":"1"}`},
				{Utterance: "把示例条目 A 的金额改成 25 元", Args: `{"itemId":1,"amount":2500}`},
			},
		},
		Fn: svc.Update,
		// Three lines, because the generic helper does the work. If this were
		// tedious, developers would skip it and the preview guarantee would
		// quietly erode — see plan §4.3.
		DryRun: func(ctx context.Context, in service.DemoUpdateIn) (*capability.Preview, error) {
			var current model.DemoItem
			if err := dbx.DBFrom(ctx, db).
				Where("item_id = ? AND del_flag = ?", in.ItemID, model.DelFlagAlive).
				First(&current).Error; err != nil {
				return nil, errcode.NotFoundf("条目 %d 不存在", in.ItemID)
			}
			return dryrun.ForUpdate(ctx, db, &model.DemoItem{},
				[]any{in.ItemID},
				service.DemoPatchFor(in, current),
				labelOpt())
		},
		Undo: undo.RestoreFieldsFn(db, tbl),
	})

	capability.Register(capability.Capability[service.DemoRemoveIn, capability.Empty]{
		Meta: capability.Meta{
			Name:    "demo.item.remove",
			Summary: "删除示例条目",
			Tags:    []string{"示例"},
			Perm:    "demo:item:remove",
			// Deletes are high risk even when soft and reversible: the AI must
			// show the diff and get an explicit confirmation.
			Risk:       capability.RiskHigh,
			Mutating:   true,
			Reversible: true,
			HTTP:       &capability.HTTPBinding{Method: "DELETE", Path: route + "/remove/:itemIds"},
			AIEnabled:  true,
			MCPEnabled: true,
			LogTitle:   "示例条目",
			BizType:    capability.BizDelete,
			Examples: []capability.Example{
				{Utterance: "删除 4 号条目", Args: `{"itemIds":[4]}`},
				{Utterance: "把 2 号和 5 号条目删掉", Args: `{"itemIds":[2,5]}`},
			},
		},
		Fn: svc.Remove,
		DryRun: func(ctx context.Context, in service.DemoRemoveIn) (*capability.Preview, error) {
			ids := make([]any, 0, len(in.ItemIDs))
			for _, id := range in.ItemIDs {
				ids = append(ids, id)
			}
			return dryrun.ForDelete(ctx, db, &model.DemoItem{}, ids, labelOpt())
		},
		Undo: undo.RestoreSoftDeletedFn(db, tbl, "del_flag", model.DelFlagAlive),
	})
}

func labelOpt() dryrun.LabelOpt {
	return dryrun.LabelOpt{
		Entity:      table,
		IDField:     "item_id",
		LabelFunc:   service.DemoLabel,
		FieldLabels: fieldLabels,
		DictFields:  dictFields,
		Route:       route,
	}
}

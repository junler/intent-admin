package service

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
)

// DemoItemView is the row shape returned to clients.
type DemoItemView struct {
	ItemID     int64     `json:"itemId"`
	Name       string    `json:"name"`
	Category   string    `json:"category"`
	Amount     int64     `json:"amount"   jsonschema:"金额，整数分"`
	Status     string    `json:"status"   jsonschema:"状态：0 正常，1 停用"`
	Remark     string    `json:"remark"`
	CreateTime time.Time `json:"createTime"`
	UpdateTime time.Time `json:"updateTime"`
}

// DemoListIn is the filter for the demo list.
type DemoListIn struct {
	paginate.Query
	Name     string `json:"name"     form:"name"     jsonschema:"名称，模糊匹配"`
	Category string `json:"category" form:"category" jsonschema:"分类，精确匹配"`
	Status   string `json:"status"   form:"status"   validate:"omitempty,oneof=0 1" jsonschema:"状态：0 正常，1 停用"`
}

// DemoListOut is the paginated result. The field name `rows` is what
// ExportSpec.RowsField points at.
type DemoListOut struct {
	Rows     []DemoItemView `json:"rows"`
	Total    int64          `json:"total"`
	PageNum  int            `json:"pageNum"`
	PageSize int            `json:"pageSize"`
}

// DemoUpdateIn updates one item. Pointer fields distinguish "not supplied"
// from "set to zero", which matters for partial updates: without it there is
// no way to clear a remark or set an amount to 0.
type DemoUpdateIn struct {
	ItemID   int64   `json:"itemId"   uri:"itemId" validate:"required,gt=0" jsonschema:"条目 ID"`
	Name     *string `json:"name"     validate:"omitempty,min=1,max=100" jsonschema:"名称"`
	Category *string `json:"category" validate:"omitempty,max=32"        jsonschema:"分类"`
	Amount   *int64  `json:"amount"   validate:"omitempty,gte=0"         jsonschema:"金额，整数分"`
	Status   *string `json:"status"   validate:"omitempty,oneof=0 1"     jsonschema:"状态：0 正常，1 停用"`
	Remark   *string `json:"remark"   validate:"omitempty,max=500"       jsonschema:"备注"`
}

// DemoRemoveIn soft-deletes items.
type DemoRemoveIn struct {
	ItemIDs []int64 `json:"itemIds" uri:"itemIds" collection_format:"csv" validate:"required,min=1,max=200,dive,gt=0" jsonschema:"要删除的条目 ID 列表"`
}

// DemoService implements the demo capabilities' behaviour.
type DemoService struct {
	db *gorm.DB
}

// NewDemoService builds the service.
func NewDemoService(db *gorm.DB) *DemoService { return &DemoService{db: db} }

// sortable whitelists the columns the client may sort by. The value reaches
// SQL, so an unchecked column name is an injection vector.
var demoSortable = map[string]string{
	"itemId":     "item_id",
	"name":       "name",
	"amount":     "amount",
	"createTime": "create_time",
	"updateTime": "update_time",
}

// List returns a page of items.
func (s *DemoService) List(ctx context.Context, in DemoListIn) (DemoListOut, error) {
	db := dbx.DBFrom(ctx, s.db).Model(&model.DemoItem{}).
		Where("del_flag = ?", model.DelFlagAlive)

	if in.Name != "" {
		db = db.Where("name LIKE ?", "%"+in.Name+"%")
	}
	if in.Category != "" {
		db = db.Where("category = ?", in.Category)
	}
	if in.Status != "" {
		db = db.Where("status = ?", in.Status)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return DemoListOut{}, errcode.Internalf("查询失败").Wrap(err)
	}

	order, err := in.OrderScope(demoSortable, "create_time")
	if err != nil {
		return DemoListOut{}, err
	}

	var items []model.DemoItem
	if err := db.Scopes(order, in.Scope()).Order("item_id ASC").Find(&items).Error; err != nil {
		return DemoListOut{}, errcode.Internalf("查询失败").Wrap(err)
	}

	rows := make([]DemoItemView, 0, len(items))
	for _, it := range items {
		rows = append(rows, toDemoView(it))
	}
	return DemoListOut{Rows: rows, Total: total, PageNum: in.PageNum, PageSize: in.PageSize}, nil
}

// Update applies a partial update and records the change for rollback.
func (s *DemoService) Update(ctx context.Context, in DemoUpdateIn) (capability.Empty, error) {
	db := dbx.DBFrom(ctx, s.db)

	var current model.DemoItem
	if err := db.Where("item_id = ? AND del_flag = ?", in.ItemID, model.DelFlagAlive).
		First(&current).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return capability.Empty{}, errcode.NotFoundf("条目 %d 不存在", in.ItemID)
		}
		return capability.Empty{}, errcode.Internalf("查询失败").Wrap(err)
	}

	patch, before := demoPatch(in, current)
	if len(patch) == 0 {
		// Nothing to do. Returning success with an empty changeset keeps the
		// operation idempotent without writing a misleading audit row.
		return capability.Empty{}, nil
	}
	patch["update_time"] = time.Now()
	if actor, ok := capability.ActorFrom(ctx); ok {
		patch["update_by"] = actor.UserName
	}

	if err := db.Model(&model.DemoItem{}).
		Where("item_id = ?", in.ItemID).
		Updates(patch).Error; err != nil {
		return capability.Empty{}, errcode.Internalf("更新失败").Wrap(err)
	}

	// Reporting the change is what makes this operation reversible and what
	// puts a readable diff in the changeset. One line per write.
	capability.RecorderFrom(ctx).Update(
		model.DemoItem{}.TableName(),
		capability.ID(in.ItemID),
		current.Name,
		before,
		stripAudit(patch),
	)
	return capability.Empty{}, nil
}

// Remove soft-deletes items, skipping ones that are already gone.
func (s *DemoService) Remove(ctx context.Context, in DemoRemoveIn) (capability.Empty, error) {
	db := dbx.DBFrom(ctx, s.db)

	var items []model.DemoItem
	if err := db.Where("item_id IN ? AND del_flag = ?", in.ItemIDs, model.DelFlagAlive).
		Find(&items).Error; err != nil {
		return capability.Empty{}, errcode.Internalf("查询失败").Wrap(err)
	}
	if len(items) == 0 {
		return capability.Empty{}, errcode.NotFoundf("没有可删除的条目")
	}

	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ItemID)
	}
	if err := db.Model(&model.DemoItem{}).
		Where("item_id IN ?", ids).
		Updates(map[string]any{"del_flag": model.DelFlagDeleted, "update_time": time.Now()}).
		Error; err != nil {
		return capability.Empty{}, errcode.Internalf("删除失败").Wrap(err)
	}

	rec := capability.RecorderFrom(ctx)
	table := model.DemoItem{}.TableName()
	for _, it := range items {
		rec.Delete(table, capability.ID(it.ItemID), it.Name, map[string]any{
			"name":     it.Name,
			"category": it.Category,
			"amount":   it.Amount,
			"status":   it.Status,
		})
	}
	return capability.Empty{}, nil
}

// CountForRemove is used by the dry run to describe the delete.
func (s *DemoService) DB() *gorm.DB { return s.db }

// demoPatch computes the column patch and the previous values for the fields
// actually supplied.
func demoPatch(in DemoUpdateIn, current model.DemoItem) (patch, before map[string]any) {
	patch = map[string]any{}
	before = map[string]any{}
	if in.Name != nil && *in.Name != current.Name {
		patch["name"] = *in.Name
		before["name"] = current.Name
	}
	if in.Category != nil && *in.Category != current.Category {
		patch["category"] = *in.Category
		before["category"] = current.Category
	}
	if in.Amount != nil && *in.Amount != current.Amount {
		patch["amount"] = *in.Amount
		before["amount"] = current.Amount
	}
	if in.Status != nil && *in.Status != current.Status {
		patch["status"] = *in.Status
		before["status"] = current.Status
	}
	if in.Remark != nil && *in.Remark != derefString(current.Remark) {
		patch["remark"] = *in.Remark
		before["remark"] = derefString(current.Remark)
	}
	return patch, before
}

// DemoPatchFor exposes the patch computation to the capability's dry run, so
// preview and execution derive the field set from the same code. Two
// implementations would eventually disagree, and the diff the user approved
// would stop matching what runs.
func DemoPatchFor(in DemoUpdateIn, current model.DemoItem) map[string]any {
	patch, _ := demoPatch(in, current)
	return patch
}

// stripAudit removes bookkeeping columns from the recorded `after` map so the
// diff shown to a user contains only fields they can reason about.
func stripAudit(patch map[string]any) map[string]any {
	out := make(map[string]any, len(patch))
	for k, v := range patch {
		switch k {
		case "update_time", "update_by", "create_time", "create_by":
			continue
		}
		out[k] = v
	}
	return out
}

func toDemoView(it model.DemoItem) DemoItemView {
	return DemoItemView{
		ItemID:     it.ItemID,
		Name:       it.Name,
		Category:   it.Category,
		Amount:     it.Amount,
		Status:     it.Status,
		Remark:     derefString(it.Remark),
		CreateTime: it.CreateTime,
		UpdateTime: it.UpdateTime,
	}
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// DemoLabel renders an item for diffs and audit messages.
func DemoLabel(row map[string]any) string {
	name, _ := row["name"].(string)
	if name == "" {
		return fmt.Sprint(row["item_id"])
	}
	return name
}

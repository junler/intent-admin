package demo

import (
	"context"
	"strconv"

	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

// AttachmentAccess demonstrates a module-owned attachment authorization policy.
func AttachmentAccess(db *gorm.DB) func(context.Context, string, bool) error {
	return func(ctx context.Context, id string, write bool) error {
		a, ok := capability.ActorFrom(ctx)
		if !ok {
			return errcode.Unauthorizedf("请先登录")
		}
		perm := "demo:item:list"
		if write {
			perm = "demo:item:edit"
		}
		if !a.Can(perm) {
			return errcode.Forbiddenf("没有业务记录访问权限")
		}
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 {
			return errcode.BadRequestf("记录编号无效")
		}
		var count int64
		if err := dbx.DBFrom(ctx, db).Model(&model.DemoItem{}).Where("item_id=? AND del_flag='0'", n).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return errcode.NotFoundf("业务记录不存在")
		}
		return nil
	}
}

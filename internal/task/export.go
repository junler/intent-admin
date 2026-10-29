package task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/xuri/excelize/v2"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/exportfmt"
	"github.com/junler/intent-admin/internal/service"
)

type exportPayload struct {
	Capability string          `json:"capability"`
	Filters    json.RawMessage `json:"filters"`
	UserID     int64           `json:"userId"`
}

// EnqueueExport is wired to the HTTP adapter's async fallback. No caller can
// choose another user's identity; it is copied from the authenticated actor.
func (r *Runtime) EnqueueExport(ctx context.Context, e *capability.Entry, in any) (any, error) {
	a, ok := capability.ActorFrom(ctx)
	if !ok {
		return nil, errcode.Unauthorizedf("请先登录")
	}
	if a.Source != capability.SourceHTTP || e.Export == nil || !a.Can(e.ExportPerm()) || !a.Can(e.Perm) {
		return nil, errcode.Forbiddenf("没有权限导出该功能")
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	p, _ := json.Marshal(exportPayload{e.Name, raw, a.UserID})
	id, err := r.acceptTask(ctx, "intent-admin:export", p, []asynq.Option{asynq.Queue(queueName(r.Cfg.Redis.KeyPrefix, "low")), asynq.MaxRetry(2), asynq.Timeout(30 * time.Minute)})
	if err != nil {
		return nil, err
	}
	return map[string]any{"taskId": id, "status": "accepted", "message": "后台导出已受理，系统将自动投递并重试，完成后可在文件管理页下载"}, nil
}
func (r *Runtime) handleExport(ctx context.Context, t *asynq.Task) error {
	var p exportPayload
	if json.Unmarshal(t.Payload(), &p) != nil {
		return fmt.Errorf("invalid export payload: %w", asynq.SkipRetry)
	}
	ctx = capability.WithActor(ctx, capability.SystemActor("异步导出"))
	_, err := capability.InvokeJSON(ctx, "task.system.exportAsync", t.Payload())
	return err
}
func (r *Runtime) export(ctx context.Context, p exportPayload) (err error) {
	e, ok := capability.Lookup(p.Capability)
	if !ok || e.Export == nil {
		return errcode.NotFoundf("导出功能不存在")
	}
	actor, err := r.authorizeExport(ctx, p.UserID, e)
	if err != nil {
		return err
	}
	ctx = capability.WithActor(ctx, actor)
	if r.FileWriter == nil {
		return errcode.Internalf("文件存储服务不可用")
	}
	var filters map[string]any
	if len(p.Filters) == 0 {
		p.Filters = json.RawMessage(`{}`)
	}
	if json.Unmarshal(p.Filters, &filters) != nil {
		return errcode.BadRequestf("筛选条件格式错误")
	}
	if filters == nil {
		filters = map[string]any{}
	}
	f := excelize.NewFile()
	defer f.Close()
	sheet := e.Export.SheetName
	if sheet == "" {
		sheet = "数据"
	}
	_ = f.SetSheetName("Sheet1", sheet)
	stream, err := f.NewStreamWriter(sheet)
	if err != nil {
		return err
	}
	headers := make([]any, len(e.Export.Columns))
	for i, c := range e.Export.Columns {
		headers[i] = c.Label
	}
	if err = stream.SetRow("A1", headers); err != nil {
		return err
	}
	dict := map[string]map[string]string{}
	for _, c := range e.Export.Columns {
		if c.Dict != "" {
			var rows []struct {
				Value string `gorm:"column:dict_value"`
				Label string `gorm:"column:dict_label"`
			}
			if er := r.DB.WithContext(ctx).Table("sys_dict_data").Select("dict_value,dict_label").Where("dict_type=? AND status='0' AND del_flag='0'", c.Dict).Scan(&rows).Error; er != nil {
				return er
			}
			values := map[string]string{}
			for _, v := range rows {
				values[v.Value] = v.Label
			}
			dict[c.Dict] = values
		}
	}
	rowNumber := 2
	total := int64(-1)
	for page := 1; ; page++ {
		filters["pageNum"] = page
		filters["pageSize"] = 100
		raw, _ := json.Marshal(filters)
		out, er := capability.InvokeJSON(ctx, e.Name, raw)
		if er != nil {
			return er
		}
		encoded, er := json.Marshal(out)
		if er != nil {
			return er
		}
		var result struct {
			Rows  []map[string]any `json:"rows"`
			Total int64            `json:"total"`
		}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.UseNumber()
		if er = decoder.Decode(&result); er != nil {
			return er
		}
		if total < 0 {
			total = result.Total
		}
		for _, row := range result.Rows {
			vals := make([]any, len(e.Export.Columns))
			for i, c := range e.Export.Columns {
				v := row[c.Field]
				if c.Dict != "" {
					if label, ok := dict[c.Dict][fmt.Sprint(v)]; ok {
						v = label
					}
				}
				if c.Format == capability.FormatMoney {
					v = exportfmt.Money(v)
				}
				if c.Format == capability.FormatDateTime {
					if s, ok := v.(string); ok {
						if t, er := time.Parse(time.RFC3339Nano, s); er == nil {
							v = t.Format("2006-01-02 15:04:05")
						}
					}
				}
				vals[i] = v
			}
			cell, _ := excelize.CoordinatesToCellName(1, rowNumber)
			if er := stream.SetRow(cell, vals); er != nil {
				return er
			}
			rowNumber++
		}
		if len(result.Rows) == 0 || int64(rowNumber-2) >= result.Total {
			break
		}
		if page >= 100000 {
			return errcode.BadRequestf("导出超过最大工作表行数，请缩小范围")
		}
	}
	if err = stream.Flush(); err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "intent-admin-export-*.xlsx")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err = f.Write(tmp); err != nil {
		return err
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	name := strings.ReplaceAll(e.Export.FileName, "{date}", time.Now().Format("20060102150405"))
	if name == "" {
		name = e.Name
	}
	if !strings.HasSuffix(name, ".xlsx") {
		name += ".xlsx"
	}
	// Long-running exports check authorization again at the delivery boundary.
	// A role revoked while rows were being streamed must not produce a file.
	actor, err = r.authorizeExport(ctx, p.UserID, e)
	if err != nil {
		return err
	}
	ctx = capability.WithActor(ctx, actor)
	file, err := r.FileWriter(ctx, name, tmp, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "export")
	if err != nil {
		return err
	}
	id := fmt.Sprint(file["uploadId"])
	capability.RecorderFrom(ctx).Create("sys_upload", id, name, map[string]any{"file_name": name, "biz_type": "export", "size": file["size"]})
	notice := map[string]any{"notice_title": "导出完成：" + e.Summary, "notice_type": "1", "notice_content": fmt.Sprintf("%s 的导出已完成（%d 行）。文件编号 %s，请前往文件管理下载。", actor.UserName, rowNumber-2, id), "status": "0", "del_flag": "0", "create_by": actor.UserName, "update_by": actor.UserName}
	return dbx.DBFrom(ctx, r.DB).Table("sys_notice").Create(notice).Error
}

func (r *Runtime) authorizeExport(ctx context.Context, userID int64, e *capability.Entry) (*capability.Actor, error) {
	identity := service.NewIdentity(r.DB)
	u, err := identity.User(ctx, userID)
	if err != nil || u.Status != model.StatusNormal {
		return nil, errcode.Forbiddenf("导出账号已停用或不存在")
	}
	_, perms, err := identity.RolesAndPermissions(ctx, u.UserID)
	if err != nil {
		return nil, err
	}
	a := &capability.Actor{Type: capability.ActorHuman, Source: capability.SourceHTTP, UserID: u.UserID, UserName: u.UserName, Perms: perms, MaxRisk: capability.RiskCritical, ApprovalMode: "manual"}
	if !a.Can(e.Perm) || !a.Can(e.ExportPerm()) {
		return nil, errcode.Forbiddenf("导出权限已被撤销")
	}
	return a, nil
}

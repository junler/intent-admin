package task

import (
	"context"
	"encoding/json"
	"github.com/junler/intent-admin/internal/automation"
	"strings"
	"unicode/utf8"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

const AnnouncementHandler = "system:createNotice"

type AnnouncementParams struct {
	Title   string `json:"noticeTitle"`
	Content string `json:"noticeContent"`
	Type    string `json:"noticeType"`
	Status  string `json:"status"`
	Remark  string `json:"remark,omitempty"`
}

func announcementParams(raw json.RawMessage) (AnnouncementParams, error) {
	var p AnnouncementParams
	if json.Unmarshal(raw, &p) != nil || strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Content) == "" {
		return p, errcode.BadRequestf("通知公告需要标题 noticeTitle 和正文 noticeContent")
	}
	if utf8.RuneCountInString(p.Title) > 100 || utf8.RuneCountInString(p.Content) > 100000 || utf8.RuneCountInString(p.Remark) > 500 {
		return p, errcode.BadRequestf("公告标题最多 100 字、正文最多 100000 字、备注最多 500 字")
	}
	if p.Type == "" {
		p.Type = "2"
	}
	if p.Status == "" {
		p.Status = "0"
	}
	if (p.Type != "1" && p.Type != "2") || (p.Status != "0" && p.Status != "1") {
		return p, errcode.BadRequestf("通知类型必须为 1（通知）或 2（公告），状态必须为 0（正常）或 1（停用）")
	}
	return p, nil
}

// AuthorizeJob checks delegated business permissions before a human creates,
// edits, enables or manually runs a scheduled operation.
func (r *Runtime) AuthorizeJob(ctx context.Context, j Job) error {
	if j.InvokeTarget == WorkflowHandler {
		p, err := workflowParams(j.JobParams)
		if err != nil {
			return err
		}
		a, ok := capability.ActorFrom(ctx)
		if !ok || a.ReadOnly || !capability.RiskHigh.AtMost(a.MaxRisk) {
			return errcode.Forbiddenf("通用定时流程需要 high 风险上限和写入授权")
		}
		return automation.Validate(ctx, p.Flow, true)
	}
	r.mu.RLock()
	h := r.handlers[j.InvokeTarget]
	r.mu.RUnlock()
	if h.Permission == "" {
		return nil
	}
	a, ok := capability.ActorFrom(ctx)
	if !ok || !a.Can(h.Permission) {
		return errcode.Forbiddenf("使用此处理器需要权限 %s", h.Permission)
	}
	return nil
}

func (r *Runtime) registerAnnouncementHandler() {
	r.RegisterHandler(Handler{
		Name: AnnouncementHandler, Summary: "创建公开通知公告，每次执行新增一条；noticeType 为 1（通知）或 2（公告），status 为 0（正常）或 1（停用）", Permission: "system:notice:add",
		ParamsProto: map[string]any{"noticeTitle": "定时公告", "noticeContent": "这是一条定时发布的公告", "noticeType": "2", "status": "0", "remark": ""},
		Fn: func(ctx context.Context, raw json.RawMessage) error {
			params, err := announcementParams(raw)
			if err != nil {
				return err
			}
			args, _ := json.Marshal(params)
			// Reuse the ordinary notice capability for validation, audit,
			// changesets and transaction participation.
			_, err = capability.InvokeJSON(ctx, "system.notice.create", args)
			return err
		},
	})
}

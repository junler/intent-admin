package notification

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type SendInput struct {
	Title   string `json:"title" jsonschema:"个人系统通知标题，最多 200 字"`
	Content string `json:"content" jsonschema:"个人系统通知正文，最多 16000 字节"`
}

func validateSend(ctx context.Context, in SendInput) (int64, error) {
	a, ok := capability.ActorFrom(ctx)
	if !ok || a.UserID <= 0 {
		return 0, errcode.Unauthorizedf("请先登录")
	}
	if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Content) == "" || utf8.RuneCountInString(in.Title) > 200 || len(in.Content) > 16000 {
		return 0, errcode.BadRequestf("通知需要标题（最多 200 字）和内容（最多 16000 字节）")
	}
	return a.UserID, nil
}

// RegisterCapabilities exposes personal delivery to every ordinary execution
// path. The recipient is always the authenticated principal, never an input.
func (s *Service) RegisterCapabilities() {
	capability.Register(capability.Capability[SendInput, capability.Empty]{Meta: capability.Meta{Name: "system.notification.send", Summary: "给当前用户发送个人系统通知（铃铛），适用于即时请求、通用流程和定时任务", Risk: capability.RiskMedium, Mutating: true, AIEnabled: true, MCPEnabled: true, HTTP: &capability.HTTPBinding{Method: "POST", Path: "/system/notification/send"}, LogTitle: "个人通知", BizType: capability.BizCreate, Examples: []capability.Example{{Utterance: "通知我统计结果", Args: `{"title":"统计结果","content":"今日新增用户 12 人"}`}}}, Fn: func(ctx context.Context, in SendInput) (capability.Empty, error) {
		id, err := validateSend(ctx, in)
		if err != nil {
			return capability.Empty{}, err
		}
		m, err := s.PublishMessage(ctx, id, in.Title, in.Content, "/")
		if err == nil {
			capability.RecorderFrom(ctx).Create("sys_notification", m.ID, m.Title, map[string]any{"user_id": id, "title": m.Title, "content": m.Content, "link": m.Link})
		}
		return capability.Empty{}, err
	}, DryRun: func(ctx context.Context, in SendInput) (*capability.Preview, error) {
		id, err := validateSend(ctx, in)
		if err != nil {
			return nil, err
		}
		p := &capability.Preview{Summary: "向当前用户发送个人系统通知：" + in.Title}
		p.AddChange(capability.Change{Entity: "sys_notification", Op: capability.OpCreate, Label: in.Title, Fields: []capability.FieldDiff{{Field: "user_id", After: id}, {Field: "title", After: in.Title}, {Field: "content", After: in.Content}}})
		return p, nil
	}})
}

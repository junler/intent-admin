package task

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/notification"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

const NotificationHandler = "system:notifyUser"

type NotificationParams struct {
	RecipientID int64  `json:"recipientId"`
	Title       string `json:"title"`
	Content     string `json:"content"`
}

func notificationParams(raw json.RawMessage) (NotificationParams, error) {
	var p NotificationParams
	if json.Unmarshal(raw, &p) != nil || p.RecipientID <= 0 || strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Content) == "" || utf8.RuneCountInString(p.Title) > 200 || len(p.Content) > 16000 {
		return p, errcode.BadRequestf("通知需要有效接收人、标题（最多 200 字）和内容（最多 16000 字节）")
	}
	return p, nil
}

// PrepareJob binds private notification recipients on the authenticated write
// path. An edit preserves the original recipient, including edits by admins.
func (r *Runtime) PrepareJob(ctx context.Context, j *Job, previous *Job) error {
	if err := r.AuthorizeJob(ctx, *j); err != nil {
		return err
	}
	expression, err := NormalizeCron(j.CronExpression)
	if err != nil {
		return err
	}
	j.CronExpression = expression
	if j.InvokeTarget == WorkflowHandler {
		if err := r.prepareWorkflow(ctx, j, previous); err != nil {
			return err
		}
	}
	if j.InvokeTarget == NotificationHandler {
		a, ok := capability.ActorFrom(ctx)
		if !ok || a.UserID <= 0 {
			return errcode.Unauthorizedf("请先登录")
		}
		recipient := a.UserID
		if previous != nil && previous.InvokeTarget == NotificationHandler {
			old, err := notificationParams(json.RawMessage(previous.JobParams))
			if err != nil {
				return err
			}
			recipient = old.RecipientID
		}
		var values map[string]json.RawMessage
		if json.Unmarshal([]byte(j.JobParams), &values) != nil || values == nil {
			return errcode.BadRequestf("任务参数必须为 JSON 对象")
		}
		if raw, exists := values["recipientId"]; exists {
			var supplied int64
			if json.Unmarshal(raw, &supplied) != nil || (supplied != 0 && supplied != recipient) {
				return errcode.Forbiddenf("通知接收人由服务端绑定，不能指定其他用户")
			}
		}
		values["recipientId"], _ = json.Marshal(recipient)
		raw, err := json.Marshal(values)
		if err != nil {
			return err
		}
		j.JobParams = string(raw)
	}
	if err := r.ValidateJob(j); err != nil {
		return err
	}
	if j.InvokeTarget == AnnouncementHandler {
		params, err := announcementParams(json.RawMessage(j.JobParams))
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(params)
		j.JobParams = string(raw)
	}
	return nil
}

func (r *Runtime) registerNotificationCapability() {
	notification.New(r.DB).RegisterCapabilities()
	capability.Register(capability.Capability[NotificationParams, capability.Empty]{
		Meta: capability.Meta{Name: "task.system.notifyUser", Summary: "投递定时个人系统通知", Perm: "monitor:job:edit", Risk: capability.RiskMedium, Mutating: true, AIEnabled: false, MCPEnabled: false, LogTitle: "定时通知", BizType: capability.BizCreate},
		Fn: func(ctx context.Context, in NotificationParams) (capability.Empty, error) {
			a, ok := capability.ActorFrom(ctx)
			if !ok || a.Source != capability.SourceTask || a.Type != capability.ActorSystem {
				return capability.Empty{}, errcode.Forbiddenf("定时通知只能由任务处理器投递")
			}
			raw, _ := json.Marshal(in)
			if _, err := notificationParams(raw); err != nil {
				return capability.Empty{}, err
			}
			actor := *a
			actor.UserID = in.RecipientID
			args, _ := json.Marshal(notification.SendInput{Title: in.Title, Content: in.Content})
			_, err := capability.InvokeJSON(capability.WithActor(ctx, &actor), "system.notification.send", args)
			return capability.Empty{}, err
		},
		DryRun: func(ctx context.Context, in NotificationParams) (*capability.Preview, error) {
			raw, _ := json.Marshal(in)
			if _, err := notificationParams(raw); err != nil {
				return nil, err
			}
			return &capability.Preview{Summary: "向任务创建者发送个人系统通知：" + in.Title, Affected: 1}, nil
		},
	})
}

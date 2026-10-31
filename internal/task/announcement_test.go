package task

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

func TestAnnouncementJobPermissionsAndDefaults(t *testing.T) {
	r := testRuntime(t)
	job := func() Job {
		return Job{JobName: "每分钟创建公告", InvokeTarget: AnnouncementHandler, JobParams: `{"noticeTitle":"定时公告","noticeContent":"公告正文"}`, CronExpression: "* * * * *", Status: "0"}
	}
	withoutNotice := capability.WithActor(context.Background(), &capability.Actor{UserID: 7, Source: capability.SourceAI, Perms: []string{"monitor:job:add", "monitor:job:run"}})
	j := job()
	if err := r.PrepareJob(withoutNotice, &j, nil); errcode.From(err).Code != errcode.Forbidden {
		t.Fatalf("scheduled public notice bypassed notice permission: %v", err)
	}
	if _, err := r.Enqueue(withoutNotice, j, true); errcode.From(err).Code != errcode.Forbidden {
		t.Fatalf("manual run bypassed notice permission: %v", err)
	}
	ctx := capability.WithActor(context.Background(), &capability.Actor{UserID: 7, Source: capability.SourceAI, Perms: []string{"monitor:job:add", "system:notice:add"}})
	if err := r.PrepareJob(ctx, &j, nil); err != nil {
		t.Fatal(err)
	}
	var params AnnouncementParams
	if err := json.Unmarshal([]byte(j.JobParams), &params); err != nil {
		t.Fatal(err)
	}
	if params.Type != "2" || params.Status != "0" {
		t.Fatalf("expected defaults to be visible in stored job: %+v", params)
	}
	if err := r.AuthorizeJob(context.Background(), j); err == nil {
		t.Fatal("unauthenticated caller could enable public announcement task")
	}
	previous := j
	if err := r.PrepareJob(withoutNotice, &j, &previous); err == nil {
		t.Fatal("editing existing announcement job bypassed notice permission")
	}
}

func TestAnnouncementJobParameterValidation(t *testing.T) {
	r := testRuntime(t)
	for _, params := range []string{
		`{}`, `null`, `[]`,
		`{"noticeTitle":"标题"}`,
		`{"noticeTitle":" ","noticeContent":"正文"}`,
		`{"noticeTitle":"标题","noticeContent":"正文","noticeType":"3"}`,
		`{"noticeTitle":"标题","noticeContent":"正文","status":"2"}`,
		`{"noticeTitle":"标题","noticeContent":"正文","recipientId":7}`,
		`{"noticeTitle":"` + strings.Repeat("字", 101) + `","noticeContent":"正文"}`,
	} {
		j := Job{JobName: "公告", InvokeTarget: AnnouncementHandler, JobParams: params, CronExpression: "* * * * *"}
		if err := r.ValidateJob(&j); err == nil {
			t.Errorf("invalid announcement params accepted: %s", params)
		}
	}
	for _, noticeType := range []string{"1", "2"} {
		raw, _ := json.Marshal(AnnouncementParams{Title: "标题", Content: "正文", Type: noticeType, Status: "0"})
		j := Job{JobName: "通知公告", InvokeTarget: AnnouncementHandler, JobParams: string(raw), CronExpression: "* * * * *"}
		if err := r.ValidateJob(&j); err != nil {
			t.Errorf("valid notice type %s rejected: %v", noticeType, err)
		}
	}
}

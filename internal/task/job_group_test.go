package task

import (
	"context"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/junler/intent-admin/internal/capability"
)

func TestAnnouncementJobGroupAndWorkerQueue(t *testing.T) {
	r := testRuntime(t)
	ctx := capability.WithActor(context.Background(), &capability.Actor{UserID: 7, Source: capability.SourceAI, Perms: []string{"system:notice:add"}})
	for _, tc := range []struct{ input, stored, queue string }{
		{"", "DEFAULT", "default"},
		{"DEFAULT", "DEFAULT", "default"},
		{" default ", "DEFAULT", "default"},
		{"SYSTEM", "DEFAULT", "default"},
		{" system ", "DEFAULT", "default"},
		{"CRITICAL", "critical", "critical"},
		{"low", "low", "low"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			j := Job{JobName: "每分钟发布公共公告", JobGroup: tc.input, InvokeTarget: AnnouncementHandler, JobParams: `{"noticeTitle":"公告","noticeContent":"正文"}`, CronExpression: "* * * * *", Status: "0"}
			if err := r.PrepareJob(ctx, &j, nil); err != nil {
				t.Fatal(err)
			}
			if j.JobGroup != tc.stored {
				t.Fatalf("stored group %q, want %q", j.JobGroup, tc.stored)
			}
			found := false
			for _, option := range r.options(j, false) {
				if option.Type() == asynq.QueueOpt {
					found = true
					if option.Value() != queueName(r.Cfg.Redis.KeyPrefix, tc.queue) {
						t.Fatalf("task routed to unserved queue %v", option.Value())
					}
				}
			}
			if !found {
				t.Fatal("task missing worker queue option")
			}
		})
	}
	for _, input := range []string{"PUBLIC", "公共", "unknown"} {
		j := Job{JobName: "公告", JobGroup: input, InvokeTarget: AnnouncementHandler, JobParams: `{"noticeTitle":"公告","noticeContent":"正文"}`, CronExpression: "* * * * *"}
		if err := r.PrepareJob(ctx, &j, nil); err == nil {
			t.Errorf("invented queue accepted: %s", input)
		}
	}
}

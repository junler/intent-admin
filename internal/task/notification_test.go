package task

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/junler/intent-admin/internal/capability"
)

func TestNormalizeCron(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"0 * * * * ?", "* * * * *"},
		{"0 */5 * * * ?", "*/5 * * * *"},
		{"0 0 9 ? * MON-FRI", "0 9 * * MON-FRI"},
		{"0 0 9 1 * ?", "0 9 1 * *"},
		{"  * * * * *  ", "* * * * *"},
		{"@every 1m", "@every 1m"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := NormalizeCron(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, input := range []string{"0 0 9 ? * 1", "15 * * * * ?", "0 0 0 * * ? 2027", "* * * ? *", "@every 0s"} {
		if _, err := NormalizeCron(input); err == nil {
			t.Errorf("accepted ambiguous or unsupported cron %q", input)
		}
	}
	expression, _ := NormalizeCron("0 * * * * ?")
	schedule, _ := ValidateCron(expression)
	now := time.Date(2026, 10, 31, 12, 34, 45, 0, time.UTC)
	if next := schedule.Next(now); !next.Equal(time.Date(2026, 10, 31, 12, 35, 0, 0, time.UTC)) {
		t.Fatalf("per-minute schedule fired at %v", next)
	}
}

func TestNotificationJobRecipientBinding(t *testing.T) {
	r := testRuntime(t)
	ctx := capability.WithActor(context.Background(), &capability.Actor{UserID: 7, Source: capability.SourceAI})
	job := func(params string) Job {
		return Job{JobName: "每分钟提醒", InvokeTarget: NotificationHandler, JobParams: params, CronExpression: "0 * * * * ?", Status: "0"}
	}
	j := job(`{"title":"每分钟提醒","content":"定时通知"}`)
	if err := r.PrepareJob(ctx, &j, nil); err != nil {
		t.Fatal(err)
	}
	var params NotificationParams
	_ = json.Unmarshal([]byte(j.JobParams), &params)
	if params.RecipientID != 7 || j.CronExpression != "* * * * *" || j.Status != "0" {
		t.Fatalf("incorrect prepared job: %+v %+v", j, params)
	}
	previous := j
	other := capability.WithActor(context.Background(), &capability.Actor{UserID: 99, Source: capability.SourceHTTP})
	updated := job(`{"title":"更新提醒","content":"新内容"}`)
	if err := r.PrepareJob(other, &updated, &previous); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(updated.JobParams), &params)
	if params.RecipientID != 7 {
		t.Fatalf("editing redirected a private notification to %d", params.RecipientID)
	}
	for _, raw := range []string{
		`{"title":"通知","content":"内容","recipientId":99}`,
		`{"title":"通知","content":"内容","recipientId":"7"}`,
		`{"title":"通知","content":"内容","userId":99}`,
		`{"title":"通知"}`,
		`{"title":"通知","content":" "}`,
		`[]`,
		`null`,
	} {
		j := job(raw)
		if err := r.PrepareJob(ctx, &j, nil); err == nil {
			t.Errorf("accepted invalid or forged notification params %s", raw)
		}
	}
	forgedEdit := job(`{"title":"通知","content":"内容","recipientId":99}`)
	if err := r.PrepareJob(other, &forgedEdit, &previous); err == nil {
		t.Fatal("editor changed original recipient")
	}
	unauthenticated := job(`{"title":"通知","content":"内容"}`)
	if err := r.PrepareJob(context.Background(), &unauthenticated, nil); err == nil {
		t.Fatal("created a private reminder without a principal")
	}
}

func TestNotificationParameterBounds(t *testing.T) {
	for _, p := range []NotificationParams{
		{RecipientID: 0, Title: "标题", Content: "内容"},
		{RecipientID: 7, Title: strings.Repeat("字", 201), Content: "内容"},
		{RecipientID: 7, Title: "标题", Content: strings.Repeat("x", 16001)},
	} {
		raw, _ := json.Marshal(p)
		if _, err := notificationParams(raw); err == nil {
			t.Error("invalid notification parameters accepted")
		}
	}
}

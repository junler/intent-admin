package task

import (
	"github.com/junler/intent-admin/internal/config"
	"testing"
	"time"
)

func TestSchedulePreview(t *testing.T) {
	r := &Runtime{Cfg: &config.Config{DB: config.DB{TimeZone: "Asia/Shanghai"}}}
	base := time.Date(2026, 1, 30, 16, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ name, expression, first string }{
		{"month end", "0 9 31 * *", "2026-01-31T09:00:00+08:00"},
		{"weekdays", "0 9 * * MON-FRI", "2026-02-02T09:00:00+08:00"},
		{"normalization", "0 0 9 ? * MON-FRI", "2026-02-02T09:00:00+08:00"},
		{"interval", "@every 5m", "2026-01-31T00:05:00+08:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.PreviewSchedule(tc.expression, base)
			if err != nil {
				t.Fatal(err)
			}
			if got.TimeZone != "Asia/Shanghai" || len(got.NextExecutions) != 5 || got.NextExecutions[0].Format(time.RFC3339) != tc.first {
				t.Fatalf("unexpected preview: %+v", got)
			}
			prev := base
			for _, next := range got.NextExecutions {
				if !next.After(prev) {
					t.Fatal("times not increasing")
				}
				prev = next
			}
			j := Job{CronExpression: tc.expression}
			r.SetNextExecution(&j, base)
			if !j.NextExecution.Equal(got.NextExecutions[0]) {
				t.Fatal("list diverges from preview")
			}
			if got.Estimated != (tc.name == "interval") {
				t.Fatal("wrong interval estimate flag")
			}
		})
	}
	for _, expr := range []string{"99 9 * * *", "@every 0s", "invalid", "CRON_TZ=UTC", "TZ=UTC", "TZ=UTC @every 0s"} {
		if _, err := r.PreviewSchedule(expr, base); err == nil {
			t.Fatalf("accepted invalid %q", expr)
		}
	}
	got, err := r.PreviewSchedule("0 0 31 2 *", base)
	if err != nil || len(got.NextExecutions) != 0 {
		t.Fatalf("impossible date: %+v %v", got, err)
	}
}

func TestSchedulePreviewDSTAndFallback(t *testing.T) {
	r := &Runtime{Cfg: &config.Config{DB: config.DB{TimeZone: "America/New_York"}}}
	for _, tc := range []struct{ base, expr, first, second string }{
		{"2026-03-08T00:00:00-05:00", "30 2 * * *", "2026-03-09T02:30:00-04:00", "2026-03-10T02:30:00-04:00"},
		{"2026-11-01T00:00:00-04:00", "30 1 * * *", "2026-11-01T01:30:00-04:00", "2026-11-01T01:30:00-05:00"},
	} {
		base, _ := time.Parse(time.RFC3339, tc.base)
		got, err := r.PreviewSchedule(tc.expr, base)
		if err != nil {
			t.Fatal(err)
		}
		if got.NextExecutions[0].Format(time.RFC3339) != tc.first || got.NextExecutions[1].Format(time.RFC3339) != tc.second {
			t.Fatalf("DST mismatch: %+v", got)
		}
	}
	interval, err := r.PreviewSchedule("TZ=UTC @every 5m", time.Now())
	if err != nil || !interval.Estimated || len(interval.NextExecutions) != 5 {
		t.Fatalf("zoned interval: %+v %v", interval, err)
	}
	r.Cfg.DB.TimeZone = "Not/AZone"
	if r.ScheduleLocation() != time.Local {
		t.Fatal("fallback differs from scheduler")
	}
	got, err := r.PreviewSchedule("CRON_TZ=UTC 0 9 * * *", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || got.TimeZone != "UTC" {
		t.Fatalf("legacy timezone: %+v %v", got, err)
	}
}

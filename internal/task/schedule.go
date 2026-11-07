package task

import (
	"time"

	"github.com/robfig/cron/v3"
)

// SchedulePreview is a read-only estimate, not a view of queued executions.
type SchedulePreview struct {
	CronExpression string      `json:"cronExpression"`
	TimeZone       string      `json:"timeZone"`
	BaseTime       time.Time   `json:"baseTime"`
	NextExecutions []time.Time `json:"nextExecutions"`
	Estimated      bool        `json:"estimated"`
}

// ScheduleLocation uses exactly the scheduler's configured fallback behavior.
func (r *Runtime) ScheduleLocation() *time.Location {
	if r != nil && r.Cfg != nil {
		if location, err := time.LoadLocation(r.Cfg.DB.TimeZone); err == nil {
			return location
		}
	}
	return time.Local
}

func (r *Runtime) PreviewSchedule(expression string, base time.Time) (SchedulePreview, error) {
	normalized, err := NormalizeCron(expression)
	if err != nil {
		return SchedulePreview{}, err
	}
	schedule, err := ValidateCron(normalized)
	if err != nil {
		return SchedulePreview{}, err
	}
	location := r.ScheduleLocation()
	// Preserve explicitly zoned legacy expressions accepted by the scheduler.
	if spec, ok := schedule.(*cron.SpecSchedule); ok && spec.Location != time.Local {
		location = spec.Location
	}
	base = base.In(location)
	_, estimated := schedule.(cron.ConstantDelaySchedule)
	out := SchedulePreview{CronExpression: normalized, TimeZone: location.String(), BaseTime: base, NextExecutions: []time.Time{}, Estimated: estimated}
	next := base
	for range 5 {
		next = schedule.Next(next)
		if next.IsZero() {
			break
		}
		out.NextExecutions = append(out.NextExecutions, next.In(location))
	}
	return out, nil
}

// SetNextExecution shares the preview's clock and timezone semantics. It does
// not claim that paused jobs or interval estimates are queued for execution.
func (r *Runtime) SetNextExecution(j *Job, base time.Time) {
	j.NextExecution = time.Time{}
	if preview, err := r.PreviewSchedule(j.CronExpression, base); err == nil && len(preview.NextExecutions) > 0 {
		j.NextExecution = preview.NextExecutions[0]
	}
}

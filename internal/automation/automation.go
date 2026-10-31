// Package automation provides reusable AI operations independent of scheduling.
package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"gorm.io/gorm"
)

type Step struct {
	Name       string         `json:"name" jsonschema:"步骤结果名称，使用英文字母、数字和下划线，不能重复"`
	Capability string         `json:"capability" jsonschema:"从功能目录选择的准确名称"`
	When       string         `json:"when,omitempty" jsonschema:"可选布尔结果引用，例如 ${stats.rows.0.alert}；为 false 时跳过本步骤"`
	Args       map[string]any `json:"args" jsonschema:"参数中可用 ${步骤名.rows.0.count} 引用前面只读步骤的结果；独立引用保留原类型，嵌入文本则转为字符串"`
}
type Flow struct {
	Steps    []Step `json:"steps"`
	TimeZone string `json:"timeZone,omitempty" jsonschema:"时间变量所属时区，默认系统业务时区，例如 Asia/Shanghai"`
}
type Results struct {
	Results map[string]any `json:"results"`
}
type Service struct{ DB *gorm.DB }

var namePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)
var referencePattern = regexp.MustCompile(`\$\{([^{}]+)\}`)

func Register(db *gorm.DB, timeZone string) {
	s := &Service{DB: db}
	s.registerSQL()
	capability.Register(capability.Capability[Flow, Results]{Meta: capability.Meta{Name: "automation.flow.query", Summary: "组合多个只读查询并引用结果，支持 SQL、业务查询和模板计算", Risk: capability.RiskLow, AIEnabled: true, MCPEnabled: true, HTTP: &capability.HTTPBinding{Method: "POST", Path: "/automation/flow/query"}}, Fn: func(ctx context.Context, in Flow) (Results, error) {
		if in.TimeZone == "" {
			in.TimeZone = timeZone
		}
		return Run(ctx, in, false, false)
	}})
	capability.Register(capability.Capability[Flow, Results]{Meta: capability.Meta{Name: "automation.flow.run", Summary: "执行通用流程：查询、引用结果并调用业务操作；修改前必须预演确认，可用于即时请求和定时任务", Risk: capability.RiskHigh, Mutating: true, AIEnabled: true, MCPEnabled: true, HTTP: &capability.HTTPBinding{Method: "POST", Path: "/automation/flow/run"}, LogTitle: "通用流程", Examples: []capability.Example{{Utterance: "统计今天新增用户并通知我", Args: `{"steps":[{"name":"stats","capability":"automation.sql.query","args":{"sql":"SELECT count(*) AS count FROM sys_user WHERE create_time >= $1","params":["${today}"]}},{"name":"notify","capability":"system.notification.send","args":{"title":"新增用户统计","content":"今日新增 ${stats.rows.0.count} 人"}}]}`}}}, Fn: func(ctx context.Context, in Flow) (Results, error) {
		if in.TimeZone == "" {
			in.TimeZone = timeZone
		}
		return Run(ctx, in, true, false)
	}, DryRun: func(ctx context.Context, in Flow) (*capability.Preview, error) {
		if in.TimeZone == "" {
			in.TimeZone = timeZone
		}
		result, err := Run(ctx, in, true, true)
		if err != nil {
			return nil, err
		}
		p := &capability.Preview{Summary: "执行通用流程"}
		for _, step := range in.Steps {
			if child, ok := result.Results[step.Name].(*capability.Preview); ok {
				p.Affected += child.Affected
				p.Skipped += child.Skipped
				p.Changes = append(p.Changes, child.Changes...)
				p.Warnings = append(p.Warnings, child.Warnings...)
				p.Warnings = append(p.Warnings, child.Summary)
				p.Truncated = p.Truncated || child.Truncated
			}
		}
		if len(p.Changes) > capability.MaxPreviewChanges {
			p.Changes = p.Changes[:capability.MaxPreviewChanges]
			p.Truncated = true
		}
		return p, nil
	}})
}

// Validate keeps composition inside the same entry-point and risk boundary.
func Validate(ctx context.Context, in Flow, write bool) error {
	a, ok := capability.ActorFrom(ctx)
	if !ok {
		return errcode.Unauthorizedf("请先登录")
	}
	if in.TimeZone != "" {
		if _, err := time.LoadLocation(in.TimeZone); err != nil {
			return errcode.BadRequestf("流程时区无效")
		}
	}
	if len(in.Steps) == 0 || len(in.Steps) > 25 {
		return errcode.BadRequestf("流程需要 1 到 25 个步骤")
	}
	names := map[string]bool{}
	for _, step := range in.Steps {
		if !namePattern.MatchString(step.Name) || names[step.Name] || step.Name == "today" || step.Name == "now" || step.Name == "yesterday" || step.Name == "tomorrow" {
			return errcode.BadRequestf("流程步骤名称无效或重复")
		}
		names[step.Name] = true
		e, ok := capability.Lookup(step.Capability)
		if !ok || strings.HasPrefix(step.Capability, "automation.flow.") || strings.HasPrefix(step.Capability, "monitor.job.") {
			return errcode.BadRequestf("流程步骤不能嵌套流程或管理定时任务")
		}
		// Both entry points must expose an operation before it is composable. This
		// prevents wrappers from opening configuration and credential operations.
		if !e.AIEnabled || !e.MCPEnabled || !a.Can(e.Perm) || !e.Risk.AtMost(a.MaxRisk) || e.Risk == capability.RiskCritical {
			return errcode.Forbiddenf("无权在流程中执行 %s", step.Capability)
		}
		if e.Mutating && strings.HasPrefix(e.Name, "system.config.") {
			return errcode.Forbiddenf("系统参数修改请单独提交计划，不能通过通用流程执行")
		}
		if e.Mutating && (!write || a.ReadOnly) {
			return errcode.Forbiddenf("只读流程不能包含修改")
		}
		if !e.Mutating && e.Risk != capability.RiskLow {
			return errcode.BadRequestf("流程查询必须为低风险只读功能")
		}
	}
	return nil
}

func Run(ctx context.Context, in Flow, write, preview bool) (Results, error) {
	out := Results{Results: map[string]any{}}
	if err := Validate(ctx, in, write); err != nil {
		return out, err
	}
	now := time.Now()
	if in.TimeZone != "" {
		loc, _ := time.LoadLocation(in.TimeZone)
		now = now.In(loc)
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	values := map[string]any{"now": now.Format(time.RFC3339), "today": today.Format(time.RFC3339), "yesterday": today.AddDate(0, 0, -1).Format(time.RFC3339), "tomorrow": today.AddDate(0, 0, 1).Format(time.RFC3339)}
	for _, step := range in.Steps {
		if step.When != "" {
			condition, err := Resolve(step.When, values)
			if err != nil {
				return out, err
			}
			enabled, ok := condition.(bool)
			if !ok {
				return out, errcode.BadRequestf("流程条件必须引用布尔查询结果")
			}
			if !enabled {
				out.Results[step.Name] = map[string]any{"skipped": true}
				continue
			}
		}
		args, err := Resolve(step.Args, values)
		if err != nil {
			return out, err
		}
		raw, err := json.Marshal(args)
		if err != nil {
			return out, err
		}
		e, _ := capability.Lookup(step.Capability)
		var result any
		if e.Mutating && preview {
			result, err = capability.RunPreview(ctx, e.Name, raw)
		} else if !e.Mutating {
			result, err = capability.InvokeJSON(dbx.WithReadOnly(ctx), e.Name, raw)
		} else {
			result, err = capability.InvokeJSON(ctx, e.Name, raw)
		}
		if err != nil {
			return out, errcode.BadRequestf("流程步骤 %s 失败：%s", step.Name, errcode.From(err).Msg)
		}
		if preview && e.Mutating {
			out.Results[step.Name] = result
		} else {
			out.Results[step.Name] = capability.SanitizeValue(result)
		}
		// Only read results are referenceable, so every write can be fully previewed.
		if !e.Mutating {
			clean := capability.SanitizeValue(result)
			if m, ok := clean.(map[string]any); ok && write && m["truncated"] == true {
				return out, errcode.BadRequestf("查询结果被截断，请缩小范围后再执行流程")
			}
			values[step.Name] = clean
		}
	}
	return out, nil
}

// Resolve supports typed references and string interpolation, never evaluation.
func Resolve(value any, values map[string]any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, item := range v {
			next, err := Resolve(item, values)
			if err != nil {
				return nil, err
			}
			out[k] = next
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			next, err := Resolve(item, values)
			if err != nil {
				return nil, err
			}
			out[i] = next
		}
		return out, nil
	case string:
		matches := referencePattern.FindAllStringSubmatchIndex(v, -1)
		if len(matches) == 0 {
			return v, nil
		}
		var b strings.Builder
		start := 0
		for _, m := range matches {
			path := v[m[2]:m[3]]
			next, err := lookup(values, path)
			if err != nil {
				return nil, err
			}
			if m[0] == 0 && m[1] == len(v) {
				return next, nil
			}
			b.WriteString(v[start:m[0]])
			switch n := next.(type) {
			case string:
				b.WriteString(n)
			default:
				raw, _ := json.Marshal(n)
				b.Write(raw)
			}
			start = m[1]
		}
		b.WriteString(v[start:])
		return b.String(), nil
	default:
		return value, nil
	}
}
func lookup(values map[string]any, path string) (any, error) {
	return lookupPath(values, strings.Split(path, "."), path)
}
func lookupPath(current any, parts []string, path string) (any, error) {
	if len(parts) == 0 {
		return current, nil
	}
	part := parts[0]
	switch v := current.(type) {
	case map[string]any:
		next, ok := v[part]
		if !ok {
			return nil, errcode.BadRequestf("引用 %s 不存在或依赖尚未执行的写步骤", path)
		}
		return lookupPath(next, parts[1:], path)
	case []any:
		if part == "*" {
			out := make([]any, len(v))
			for i, item := range v {
				next, err := lookupPath(item, parts[1:], path)
				if err != nil {
					return nil, err
				}
				out[i] = next
			}
			return out, nil
		}
		i, err := strconv.Atoi(part)
		if err != nil || i < 0 || i >= len(v) {
			return nil, errcode.BadRequestf("引用 %s 的数组下标无效", path)
		}
		return lookupPath(v[i], parts[1:], path)
	default:
		return nil, fmt.Errorf("引用 %s 不是对象或数组", path)
	}
}

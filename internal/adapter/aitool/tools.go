// Package aitool gives the model narrow tools. Only the plan engine can
// perform writes; no tool in this package directly executes a mutating entry.
package aitool

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/knowledge"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/plan"
)

type PlanEngine interface {
	Create(context.Context, plan.CreateIn) (*plan.Plan, error)
}
type Knowledge interface {
	Get(context.Context, string) (knowledge.Knowledge, error)
}
type Emit func(kind string, data any)

type Tools struct {
	Navigation   Navigation
	Plans        PlanEngine
	Knowledge    Knowledge
	SessionID    string
	PageContext  map[string]any
	Intent       string
	MaxCalls     int
	Emit         Emit
	calls        atomic.Int64
	Proposed     atomic.Bool
	Navigated    atomic.Bool
	FilledForm   atomic.Bool
	FormPassword string
	FormSecrets  []string
	Evidence     []capability.Evidence
}

type keywordInput struct {
	Keyword string `json:"keyword,omitempty" jsonschema:"description=名称或关键词，留空列出可用功能"`
}
type nameInput struct {
	Name string `json:"name" jsonschema:"description=从功能目录中选择的准确 capability 名称"`
}
type topicInput struct {
	Topic string `json:"topic" jsonschema:"description=overview、rules、表名或字典类型"`
}
type queryInput struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}
type proposalStep struct {
	Capability string         `json:"capability"`
	Args       map[string]any `json:"args"`
	Note       string         `json:"note,omitempty"`
}
type proposalInput struct {
	Intent  string         `json:"intent"`
	Steps   []proposalStep `json:"steps"`
	OnError string         `json:"onError,omitempty" jsonschema:"enum=stop,enum=continue,default=stop,description=计划步骤失败策略。默认 stop 表示失败后停止；仅在用户明确要求失败后继续时填写 continue。不是任务处理器或定时任务参数"`
}

// Model-generated control values fail closed: only an explicit continue may
// relax the default stop-on-error policy. Core plan validation stays strict.
func proposalErrorPolicy(value string) string {
	if strings.ToLower(strings.TrimSpace(value)) == "continue" {
		return "continue"
	}
	return "stop"
}

func (t *Tools) before(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	max := t.MaxCalls
	if max <= 0 {
		max = 25
	}
	if t.calls.Add(1) > int64(max) {
		return errcode.BadRequestf("本轮工具调用超过上限 %d", max)
	}
	if t.FilledForm.Load() {
		return errcode.BadRequestf("表单已填写，请等待用户确认提交")
	}
	if t.Navigated.Load() {
		return errcode.BadRequestf("本轮页面已打开")
	}
	if t.Proposed.Load() {
		return errcode.BadRequestf("本轮计划已提交，请等待用户确认")
	}
	if t.Emit != nil {
		t.Emit("tool_call", map[string]any{"name": name, "summary": name})
	}
	return nil
}
func (t *Tools) after(name string, out any, err error) {
	if t.Emit == nil {
		return
	}
	brief := "操作完成"
	if err != nil {
		brief = errcode.From(err).Msg
	}
	t.Emit("tool_result", map[string]any{"name": name, "ok": err == nil, "brief": brief})
}

func actor(ctx context.Context) (*capability.Actor, error) {
	a, ok := capability.ActorFrom(ctx)
	if !ok || a.Source != capability.SourceAI {
		return nil, errcode.Unauthorizedf("缺少 AI 主体")
	}
	return a, nil
}

// Query is deliberately available separately for regression testing the
// read/write boundary, independently of any model vendor.
func (t *Tools) Query(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	e, ok := capability.Lookup(name)
	if !ok || !e.VisibleTo(capability.SourceAI, a.Perms, a.MaxRisk) {
		return nil, errcode.NotFoundf("未找到可用功能 %s", name)
	}
	if e.Mutating || e.Risk != capability.RiskLow {
		return nil, errcode.Forbiddenf("query 仅允许低风险只读功能，修改必须提交计划")
	}
	out, err := capability.InvokeJSON(dbx.WithReadOnly(ctx), name, raw)
	if err != nil {
		return nil, err
	}
	if route := Route(e); route != "" {
		var filters map[string]any
		_ = json.Unmarshal(raw, &filters)
		t.Evidence = append(t.Evidence, capability.Evidence{Route: route, Filters: filters})
	}
	clean := capability.SanitizeValue(out)
	encoded, err := json.Marshal(clean)
	if err != nil {
		return nil, err
	}
	if len(encoded) > 16<<10 {
		return map[string]any{"truncated": true, "content": truncateUTF8(string(encoded), 16<<10), "hint": "结果过大，请缩小筛选范围或在管理页面查看"}, nil
	}
	return clean, nil
}

func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && (s[max]&0xc0) == 0x80 {
		max--
	}
	return s[:max]
}

func (t *Tools) Build() ([]tool.BaseTool, error) {
	list, err := utils.InferTool("list_capabilities", "搜索当前用户与当前风险上限允许 AI 使用的功能。先查目录，再选择准确名称。", func(ctx context.Context, in keywordInput) (out any, err error) {
		if err = t.before(ctx, "list_capabilities"); err != nil {
			return nil, err
		}
		defer func() { t.after("list_capabilities", out, err) }()
		a, err := actor(ctx)
		if err != nil {
			return nil, err
		}
		entries := capability.Search(capability.SourceAI, a.Perms, a.MaxRisk, in.Keyword, 30)
		rows := make([]capability.Descriptor, 0, len(entries))
		for _, e := range entries {
			rows = append(rows, e.Brief())
		}
		return rows, nil
	})
	if err != nil {
		return nil, err
	}
	describe, err := utils.InferTool("describe_capability", "读取功能入参 JSON Schema、示例和风险。禁止臆造参数。", func(ctx context.Context, in nameInput) (out any, err error) {
		if err = t.before(ctx, "describe_capability"); err != nil {
			return nil, err
		}
		defer func() { t.after("describe_capability", out, err) }()
		a, err := actor(ctx)
		if err != nil {
			return nil, err
		}
		e, ok := capability.Lookup(in.Name)
		if !ok || !e.VisibleTo(capability.SourceAI, a.Perms, a.MaxRisk) {
			return nil, errcode.NotFoundf("未找到可用功能")
		}
		return e.Describe(), nil
	})
	if err != nil {
		return nil, err
	}
	know, err := utils.InferTool("get_knowledge", "查询系统字典、实体关系和硬约束。字段含义不确定时先查知识库。", func(ctx context.Context, in topicInput) (out any, err error) {
		if err = t.before(ctx, "get_knowledge"); err != nil {
			return nil, err
		}
		defer func() { t.after("get_knowledge", out, err) }()
		if t.Knowledge == nil {
			return nil, errcode.Internalf("知识库不可用")
		}
		return t.Knowledge.Get(ctx, in.Topic)
	})
	if err != nil {
		return nil, err
	}
	query, err := utils.InferTool("query", "执行低风险只读查询；不能修改数据。结果最多 16KB，需准确 capability 名与参数。", func(ctx context.Context, in queryInput) (out any, err error) {
		if err = t.before(ctx, "query"); err != nil {
			return nil, err
		}
		defer func() { t.after("query", out, err) }()
		raw, err := json.Marshal(in.Args)
		if err != nil {
			return nil, err
		}
		return t.Query(ctx, in.Name, raw)
	})
	if err != nil {
		return nil, err
	}
	propose, err := utils.InferTool("propose_plan", "所有修改必须提交计划。提交后只预览，不执行，等待用户在页面明确确认。列表导航也可提交一个只读步骤。", func(ctx context.Context, in proposalInput) (out any, err error) {
		if err = t.before(ctx, "propose_plan"); err != nil {
			return nil, err
		}
		defer func() { t.after("propose_plan", out, err) }()
		if t.Plans == nil {
			return nil, errcode.Internalf("计划引擎不可用")
		}
		intent := strings.TrimSpace(in.Intent)
		if intent == "" {
			intent = t.Intent
		}
		steps := make([]plan.Step, 0, len(in.Steps))
		for i, step := range in.Steps {
			raw, e := json.Marshal(step.Args)
			if e != nil {
				return nil, e
			}
			steps = append(steps, plan.Step{Seq: i + 1, Capability: step.Capability, Args: raw, Note: step.Note})
		}
		p, err := t.Plans.Create(ctx, plan.CreateIn{Intent: intent, SessionID: t.SessionID, Steps: steps, PageContext: t.PageContext, OnError: proposalErrorPolicy(in.OnError)})
		if err != nil {
			return nil, err
		}
		t.Proposed.Store(true)
		mutating := false
		for _, step := range p.Steps {
			if e, ok := capability.Lookup(step.Capability); ok && e.Mutating {
				mutating = true
			}
		}
		if t.Emit != nil {
			if mutating {
				t.Emit("plan", p)
			} else if len(p.Steps) == 1 {
				step := p.Steps[0]
				e, _ := capability.Lookup(step.Capability)
				if route := Route(e); route != "" && e != nil && (e.Export != nil || strings.HasSuffix(e.Name, ".list")) {
					var filters map[string]any
					_ = json.Unmarshal(step.Args, &filters)
					t.Emit("navigate", map[string]any{"route": route, "filters": filters, "note": "已按查询条件打开页面"})
				} else {
					t.Emit("answer", map[string]any{"text": formatResults(p), "evidence": []any{}})
				}
			} else {
				t.Emit("answer", map[string]any{"text": formatResults(p), "evidence": []any{}})
			}
		}
		return map[string]any{"planId": p.ID, "status": p.Status, "message": "计划已提交；写操作尚未执行，需用户确认"}, nil
	})
	if err != nil {
		return nil, err
	}
	navigation, err := t.navigationTools()
	if err != nil {
		return nil, err
	}
	form, err := t.formTool()
	if err != nil {
		return nil, err
	}
	forms, err := t.genericFormTools()
	if err != nil {
		return nil, err
	}
	return append(append(append([]tool.BaseTool{list, describe, know, query, propose}, navigation...), form), forms...), nil
}

func formatResults(p *plan.Plan) string {
	b, _ := json.MarshalIndent(capability.SanitizeValue(p.Steps), "", "  ")
	return "查询结果：\n```json\n" + truncateUTF8(string(b), 16<<10) + "\n```"
}

// consoleRoutes are the pages whose read capability is not named ".list",
// because they are consoles rather than list pages.
var consoleRoutes = []string{"/monitor/server", "/monitor/cache", "/monitor/queue"}

var (
	pageMu    sync.Mutex
	pageCount int
	pageCache []string
)

// pageBases reports the frontend routes implied by the registry.
//
// The convention is that a capability named "<ns>.list" bound to "<base>/list"
// declares <base> as a browsable page. Every module registered the normal way
// satisfies it, including the generated system resources and log pages, so
// adding a business module needs no edit here.
//
// This used to be a hand-maintained list of every route in the application.
// It had already drifted: the queue console was missing, so the AI could not
// link to or navigate there.
func pageBases() []string {
	pageMu.Lock()
	defer pageMu.Unlock()
	// The registry only grows, so its size identifies the generation.
	if n := capability.Count(); pageCache == nil || n != pageCount {
		bases := append([]string(nil), consoleRoutes...)
		for _, e := range capability.All() {
			if e.HTTP == nil || !strings.HasSuffix(e.Name, ".list") {
				continue
			}
			if base, ok := strings.CutSuffix(e.HTTP.Path, "/list"); ok && base != "" {
				bases = append(bases, base)
			}
		}
		// Longest first, so /system/dict/type wins over a shorter neighbour.
		sort.Slice(bases, func(i, j int) bool { return len(bases[i]) > len(bases[j]) })
		pageCache, pageCount = bases, n
	}
	return pageCache
}

// Route maps a capability's declared endpoint to a bundled application page.
func Route(e *capability.Entry) string {
	if e == nil || e.HTTP == nil {
		return ""
	}
	path := e.HTTP.Path
	// These three pages deliberately do not live at their capability's path.
	if strings.HasPrefix(path, "/system/changeset") {
		return "/changesets"
	}
	if strings.HasPrefix(path, "/system/audit") {
		return "/audit"
	}
	if strings.HasPrefix(path, "/dashboard/") {
		return "/"
	}
	for _, base := range pageBases() {
		if path == base || strings.HasPrefix(path, base+"/") {
			return base
		}
	}
	return ""
}

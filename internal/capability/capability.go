// Package capability is the project's backbone.
//
// Every business function is registered here once as a strongly typed
// Capability. The registry then drives all three entry points — the HTTP API,
// the in-app AI agent and the MCP server — and the executor applies
// permissions, the risk ceiling, validation, auditing and changeset recording
// in exactly one place. That is what makes "the AI cannot bypass permissions"
// a structural property rather than a promise.
//
// To add a feature: define the input/output structs, write the service
// function, call Register. HTTP route, OpenAPI entry, AI tool, MCP tool, diff
// preview and Excel export all follow automatically.
package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// HTTPBinding describes how a capability is exposed over HTTP. Nil means it is
// not reachable over HTTP at all (AI/MCP only).
type HTTPBinding struct {
	Method string // GET | POST | PUT | DELETE
	Path   string // "/system/user/:userId"
}

// Example maps a natural-language request to the arguments it should produce.
// These are the single most effective lever on small-model accuracy, so they
// are part of the capability contract rather than documentation.
type Example struct {
	Utterance string `json:"utterance"`
	Args      string `json:"args"`
}

// Business type codes recorded on the audit row.
const (
	BizOther     = 0
	BizCreate    = 1
	BizUpdate    = 2
	BizDelete    = 3
	BizGrant     = 4
	BizExport    = 5
	BizImport    = 6
	BizForceOut  = 7
	BizTruncate  = 8
)

// Meta is everything about a capability other than its behaviour.
type Meta struct {
	// Name is the globally unique dotted identifier, e.g. system.user.list.
	// It is also the tool name seen by the AI and by MCP clients.
	Name string
	// Summary is the Chinese description used in OpenAPI, the AI tool
	// catalogue and the MCP tool list.
	Summary string
	Tags    []string
	// Perm is the permission string required, e.g. system:user:list. Empty
	// means any authenticated caller.
	Perm string

	// Risk gates AI/MCP execution. Required — Register treats the zero value
	// as RiskCritical and logs a warning.
	Risk Risk
	// Mutating marks writes and side-effecting reads (refresh, clear, force
	// logout). It drives auditing, changeset recording and the need to confirm.
	Mutating bool
	// Idempotent means repeating the call with the same arguments has no
	// further effect. Surfaced to MCP clients as idempotentHint.
	Idempotent bool
	// Reversible means Undo is provided. When false the UI must say so in red:
	// an irreversible action is the one case where "are you sure" is warranted.
	Reversible bool

	HTTP       *HTTPBinding
	AIEnabled  bool
	MCPEnabled bool

	Examples []Example
	LogTitle string
	BizType  int

	// Export, when set, generates {path}/export and {path}/template endpoints
	// that reuse this capability's filter input and query function.
	Export *ExportSpec
}

// Capability binds behaviour to Meta.
type Capability[I, O any] struct {
	Meta
	// Fn performs the work. For Mutating capabilities it runs inside the
	// executor's transaction and should report changes via RecorderFrom(ctx).
	Fn func(ctx context.Context, in I) (O, error)
	// DryRun predicts the effect without causing it. Required when Mutating.
	// It runs with a database session that rejects writes, so a mistake here
	// fails loudly instead of corrupting data.
	DryRun func(ctx context.Context, in I) (*Preview, error)
	// Undo compensates an applied change. Required when Reversible.
	Undo func(ctx context.Context, undo json.RawMessage) error
}

var nameRe = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*(\.[a-z][a-zA-Z0-9]*)+$`)

// Register validates a capability and adds it to the global registry.
//
// It panics on contract violations. These are programming errors that must
// surface at startup, not at the first request: a Mutating capability without
// a DryRun would otherwise silently lose the preview guarantee the whole
// design rests on.
func Register[I, O any](c Capability[I, O]) {
	m := c.Meta

	switch {
	case m.Name == "":
		panic("capability: Name 不能为空")
	case !nameRe.MatchString(m.Name):
		panic(fmt.Sprintf("capability %q: Name 必须是点分小驼峰，如 system.user.changeStatus", m.Name))
	case m.Summary == "":
		panic(fmt.Sprintf("capability %q: Summary 不能为空（它会出现在 OpenAPI、AI 与 MCP 的功能目录里）", m.Name))
	case c.Fn == nil:
		panic(fmt.Sprintf("capability %q: Fn 不能为空", m.Name))
	}

	if !m.Risk.Valid() {
		// Fail closed: an undeclared risk is the most dangerous one.
		warnf("capability %q 未声明 Risk，已按 critical 处理（AI 与 MCP 都无法执行）", m.Name)
		m.Risk = RiskCritical
	}
	if m.Mutating && c.DryRun == nil {
		panic(fmt.Sprintf("capability %q: Mutating 为 true 时必须提供 DryRun（见 dryrun 包里的通用助手）", m.Name))
	}
	if m.Reversible && c.Undo == nil {
		panic(fmt.Sprintf("capability %q: Reversible 为 true 时必须提供 Undo（见 undo 包里的通用助手）", m.Name))
	}
	if !m.Mutating && m.Reversible {
		panic(fmt.Sprintf("capability %q: 只读操作不应标记 Reversible", m.Name))
	}
	if m.HTTP != nil {
		if !strings.HasPrefix(m.HTTP.Path, "/") {
			panic(fmt.Sprintf("capability %q: HTTP.Path 必须以 / 开头", m.Name))
		}
		switch m.HTTP.Method {
		case "GET", "POST", "PUT", "DELETE":
		default:
			panic(fmt.Sprintf("capability %q: 不支持的 HTTP 方法 %q", m.Name, m.HTTP.Method))
		}
	}
	if m.Export != nil && m.Export.normalize(m.Name) != nil {
		panic(fmt.Sprintf("capability %q: Export 配置无效", m.Name))
	}

	inputSchema, err := schemaFor[I]()
	if err != nil {
		panic(fmt.Sprintf("capability %q: 推导入参 schema 失败: %v", m.Name, err))
	}
	outputSchema, err := schemaFor[O]()
	if err != nil {
		panic(fmt.Sprintf("capability %q: 推导出参 schema 失败: %v", m.Name, err))
	}

	e := &Entry{
		Meta:         m,
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		newInput:     func() any { return new(I) },
		invoke: func(ctx context.Context, in any) (any, error) {
			typed, ok := in.(*I)
			if !ok {
				return nil, fmt.Errorf("capability %s: 入参类型不匹配 (%T)", m.Name, in)
			}
			return c.Fn(ctx, *typed)
		},
	}
	if c.DryRun != nil {
		e.dryRun = func(ctx context.Context, in any) (*Preview, error) {
			typed, ok := in.(*I)
			if !ok {
				return nil, fmt.Errorf("capability %s: 入参类型不匹配 (%T)", m.Name, in)
			}
			return c.DryRun(ctx, *typed)
		}
	}
	if c.Undo != nil {
		e.undo = c.Undo
	}
	register(e)
}

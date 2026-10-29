package capability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/junler/intent-admin/internal/pkg/errcode"
)

// Audit column widths, matching the schema in plan §9.6.
const (
	maxAuditParam  = 2000
	maxAuditResult = 2000
)

// AuditRecord is one row of the behaviour trail. It records not just what was
// called but why (Intent), how it was authorised (ApprovalMode) and what it
// produced (ChangesetID) — which is what makes an AI-driven system auditable.
type AuditRecord struct {
	Capability   string
	Title        string
	BizType      int
	Risk         Risk
	ActorType    string
	Source       Source
	UserID       int64
	UserName     string
	IP           string
	UserAgent    string
	TokenID      int64
	Intent       string
	PlanID       string
	ChangesetID  string
	ApprovalMode string
	ApprovedBy   string
	Param        string
	Result       string
	Success      bool
	ErrorMsg     string
	CostMS       int64
	OccurredAt   time.Time
}

// Hooks are the infrastructure dependencies of the executor, injected by
// bootstrap. They are interfaces rather than imports so this package stays
// free of GORM and of the changeset/audit packages, which depend on it.
type Hooks struct {
	// MaxAIRisk and MaxMCPRisk are deployment ceilings. They apply in the
	// executor as well as adapters so no alternate entry point can raise them.
	MaxAIRisk  Risk
	MaxMCPRisk Risk
	// Tx runs fn inside a database transaction. Required for Mutating
	// capabilities so the business write and its changeset commit together —
	// an audit trail that can diverge from the data is worse than none.
	Tx func(ctx context.Context, fn func(ctx context.Context) error) error
	// ReadOnly runs fn with a session that rejects writes. Used for DryRun.
	ReadOnly func(ctx context.Context, fn func(ctx context.Context) error) error
	// PersistChanges stores the recorded items and returns the changeset id.
	PersistChanges func(ctx context.Context, e *Entry, actor *Actor, items []RecordedItem, undo json.RawMessage) (string, error)
	// WriteAudit stores one audit row. Called asynchronously; must not block.
	WriteAudit func(ctx context.Context, rec AuditRecord)
	// Validate applies struct-tag validation to a bound input.
	Validate func(in any) error
	// MaxChangesetItems caps one changeset; 0 disables the check.
	MaxChangesetItems int
}

var hooks Hooks

// Install wires the executor's dependencies. Called once from bootstrap.
func Install(h Hooks) { hooks = h }

// RunPreview runs a capability's dry run with JSON arguments.
//
// Preview itself is always treated as low risk: it is read-only and the whole
// point is to let a caller see the consequences of something it may not be
// allowed to execute.
func RunPreview(ctx context.Context, name string, raw json.RawMessage) (*Preview, error) {
	e, actor, err := resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	if !e.HasDryRun() {
		return nil, errcode.BadRequestf("功能 %s 不支持预览", name)
	}
	in, err := decode(e, raw)
	if err != nil {
		return nil, err
	}
	return doPreview(ctx, e, actor, in)
}

// RunPreviewTyped runs a dry run with an already-bound input, for the HTTP path
// that has no JSON body to re-decode.
func RunPreviewTyped(ctx context.Context, name string, in any) (*Preview, error) {
	e, actor, err := resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	if !e.HasDryRun() {
		return nil, errcode.BadRequestf("功能 %s 不支持预览", name)
	}
	if err := validate(e, in); err != nil {
		return nil, err
	}
	return doPreview(ctx, e, actor, in)
}

func doPreview(ctx context.Context, e *Entry, actor *Actor, in any) (p *Preview, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("capability dry-run panic", "capability", e.Name, "panic", r)
			err = errcode.Internalf("预览 %s 时发生内部错误", e.Name)
		}
	}()
	if err := checkPermission(e, actor); err != nil {
		return nil, err
	}

	run := func(ctx context.Context) error {
		out, rerr := e.dryRun(ctx, in)
		p = out
		return rerr
	}
	if hooks.ReadOnly != nil {
		// A DryRun that writes is a contract violation that must fail loudly
		// rather than quietly mutate production data.
		err = hooks.ReadOnly(ctx, run)
	} else {
		err = run(ctx)
	}
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errcode.Internalf("功能 %s 的 DryRun 返回了空预览", e.Name)
	}
	p.Capability = e.Name
	p.Risk = e.Risk
	p.Reversible = e.Reversible
	if p.Summary == "" {
		p.Summary = fmt.Sprintf("%s：影响 %d 条", e.Summary, p.Affected)
	}
	return p, nil
}

// InvokeJSON executes a capability with JSON arguments. Used by the AI and MCP
// adapters.
func InvokeJSON(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	e, actor, err := resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	in, err := decode(e, raw)
	if err != nil {
		return nil, err
	}
	return run(ctx, e, actor, in)
}

// InvokeTyped executes a capability with an already-bound input. Used by the
// HTTP adapter, which has bound uri/query/body into the input struct and would
// otherwise pay for a pointless re-serialisation.
func InvokeTyped(ctx context.Context, name string, in any) (any, error) {
	e, actor, err := resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := validate(e, in); err != nil {
		return nil, err
	}
	return run(ctx, e, actor, in)
}

// resolve looks up the capability and checks that this entry point may see it.
func resolve(ctx context.Context, name string) (*Entry, *Actor, error) {
	actor, ok := ActorFrom(ctx)
	if !ok {
		return nil, nil, errcode.Unauthorizedf("请先登录")
	}
	e, ok := Lookup(name)
	if !ok {
		return nil, nil, errcode.NotFoundf("功能 %s 不存在", name)
	}
	// Entry-point visibility is reported as 404, not 403: telling an AI or an
	// external client that a capability exists but is off-limits to it leaks
	// the catalogue and invites probing.
	switch actor.Source {
	case SourceAI:
		if !e.AIEnabled {
			return nil, nil, errcode.NotFoundf("功能 %s 不存在", name)
		}
	case SourceMCP:
		if !e.MCPEnabled {
			return nil, nil, errcode.NotFoundf("功能 %s 不存在", name)
		}
	}
	return e, actor, nil
}

// checkPermission enforces the permission string only. The risk ceiling is a
// separate gate (checkRisk) because previews deliberately skip it.
func checkPermission(e *Entry, actor *Actor) error {
	if !actor.Can(e.Perm) {
		return errcode.Forbiddenf("没有权限执行 %s", e.Summary)
	}
	return nil
}

// checkRisk enforces the risk ceiling and, when exceeded, returns a payload
// the UI and the agent can act on: which route the human should use instead.
func checkRisk(e *Entry, actor *Actor) error {
	allowed := actor.MaxRisk
	if !allowed.Valid() {
		allowed = RiskLow
	}
	if actor.Source == SourceAI || actor.Source == SourceMCP {
		// Critical operations always require the human interface, even when
		// a deployment or an adapter accidentally supplies a critical ceiling.
		if allowed == RiskCritical {
			allowed = RiskHigh
		}
		if hooks.MaxAIRisk.Valid() && hooks.MaxAIRisk.Level() < allowed.Level() {
			allowed = hooks.MaxAIRisk
		}
	}
	if actor.Source == SourceMCP && hooks.MaxMCPRisk.Valid() && hooks.MaxMCPRisk.Level() < allowed.Level() {
		allowed = hooks.MaxMCPRisk
	}
	if e.Risk.AtMost(allowed) {
		return nil
	}
	detail := map[string]any{
		"capability": e.Name,
		"risk":       e.Risk,
		"allowed":    allowed,
		"hint":       fmt.Sprintf("%s属于%s操作，超出当前允许的上限（%s），请在管理页面手动执行", e.Summary, e.Risk.Label(), allowed.Label()),
	}
	if e.HTTP != nil {
		detail["path"] = e.HTTP.Path
	}
	return errcode.Newf(errcode.RiskExceeded, "%s 属于%s操作，需要人工在页面执行", e.Summary, e.Risk.Label()).
		WithDetail(detail)
}

func decode(e *Entry, raw json.RawMessage) (any, error) {
	in := e.NewInput()
	if len(raw) > 0 && string(raw) != "null" {
		dec := json.NewDecoder(bytes.NewReader(raw))
		// Unknown fields are rejected rather than ignored: a model that
		// hallucinates a parameter should be told, not silently obeyed with
		// the parameter dropped.
		dec.DisallowUnknownFields()
		if err := dec.Decode(in); err != nil {
			return nil, errcode.BadRequestf("参数格式错误：%v", err)
		}
	}
	if err := validate(e, in); err != nil {
		return nil, err
	}
	return in, nil
}

func validate(e *Entry, in any) error {
	if hooks.Validate == nil {
		return nil
	}
	if err := hooks.Validate(in); err != nil {
		return errcode.BadRequestf("参数校验失败：%v", err)
	}
	return nil
}

// run is the single execution path. Order matters and is asserted by tests:
// permission, risk, then execute inside a transaction that also commits the
// changeset, then audit.
func run(ctx context.Context, e *Entry, actor *Actor, in any) (out any, err error) {
	started := time.Now()
	rec := AuditRecord{
		Capability:   e.Name,
		Title:        e.LogTitle,
		BizType:      e.BizType,
		Risk:         e.Risk,
		ActorType:    actor.Type,
		Source:       actor.Source,
		UserID:       actor.UserID,
		UserName:     actor.UserName,
		IP:           actor.IP,
		UserAgent:    actor.UserAgent,
		TokenID:      actor.TokenID,
		Intent:       actor.Intent,
		PlanID:       actor.PlanID,
		ApprovalMode: actor.ApprovalMode,
		Param:        TruncateJSON(auditParameters(e.Name, in), maxAuditParam),
		OccurredAt:   started,
	}

	defer func() {
		if r := recover(); r != nil {
			slog.Error("capability panic", "capability", e.Name, "panic", r)
			err = errcode.Internalf("执行 %s 时发生内部错误", e.Name)
			out = nil
		}
		// Audit every mutating attempt, successful or not. A rejected write is
		// exactly the kind of event an operator wants to find later.
		if !e.Mutating && actor.Source != SourceMCP {
			return
		}
		rec.CostMS = time.Since(started).Milliseconds()
		rec.Success = err == nil
		if err != nil {
			rec.ErrorMsg = errcode.From(err).Msg
		} else {
			rec.Result = TruncateJSON(SanitizeValue(out), maxAuditResult)
		}
		if hooks.WriteAudit != nil {
			hooks.WriteAudit(ctx, rec)
		}
	}()

	if err = checkPermission(e, actor); err != nil {
		return nil, err
	}
	if err = checkRisk(e, actor); err != nil {
		return nil, err
	}

	if !e.Mutating {
		return e.invoke(ctx, in)
	}

	recorder := NewRecorder()
	exec := func(txCtx context.Context) error {
		txCtx = WithRecorder(txCtx, recorder)
		txCtx = withInvocation(txCtx, auditParameters(e.Name, in))
		result, ierr := e.invoke(txCtx, in)
		if ierr != nil {
			return ierr
		}
		out = result

		items := recorder.Items()
		if hooks.MaxChangesetItems > 0 && len(items) > hooks.MaxChangesetItems {
			return errcode.BadRequestf("本次操作影响 %d 条记录，超过单次上限 %d，请缩小范围后分批执行",
				len(items), hooks.MaxChangesetItems)
		}
		if len(items) == 0 || hooks.PersistChanges == nil {
			return nil
		}
		undo, _ := recorder.UndoOverride()
		id, cerr := hooks.PersistChanges(txCtx, e, actor, items, undo)
		if cerr != nil {
			return cerr
		}
		rec.ChangesetID = id
		if result := callResult(ctx); result != nil {
			result.ChangesetID = id
		}
		return nil
	}

	if hooks.Tx != nil {
		err = hooks.Tx(ctx, exec)
	} else {
		err = exec(ctx)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

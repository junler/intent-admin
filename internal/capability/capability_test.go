package capability

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type testIn struct {
	ID   int64  `json:"id"   validate:"required,gt=0"`
	Name string `json:"name" validate:"omitempty,max=8"`
}

type testOut struct {
	Echo string `json:"echo"`
}

// install wires minimal hooks: no database, validation that only enforces the
// one rule the tests need, and a recording audit sink.
func install(t *testing.T) *[]AuditRecord {
	t.Helper()
	var audits []AuditRecord
	Install(Hooks{
		Validate: func(in any) error {
			v, ok := in.(*testIn)
			if !ok {
				return nil
			}
			if v.ID <= 0 {
				return errcode.BadRequestf("id 必填")
			}
			if len(v.Name) > 8 {
				return errcode.BadRequestf("name 过长")
			}
			return nil
		},
		WriteAudit: func(_ context.Context, rec AuditRecord) {
			audits = append(audits, rec)
		},
	})
	t.Cleanup(func() { Install(Hooks{}) })
	return &audits
}

func reset(t *testing.T) {
	t.Helper()
	mu.Lock()
	entries = map[string]*Entry{}
	warnings = nil
	mu.Unlock()
}

func actor(perms []string, src Source, max Risk) *Actor {
	return &Actor{
		Type: ActorHuman, Source: src, UserID: 1, UserName: "tester",
		Perms: perms, MaxRisk: max,
	}
}

func TestRiskCeiling(t *testing.T) {
	for _, tc := range []struct {
		risk, ceiling Risk
		want          bool
	}{
		{RiskLow, RiskLow, true},
		{RiskMedium, RiskLow, false},
		{RiskHigh, RiskCritical, true},
		{RiskCritical, RiskHigh, false},
		{Risk("nonsense"), RiskCritical, true}, // unknown maps to critical
		{Risk("nonsense"), RiskHigh, false},    // and therefore fails closed
	} {
		if got := tc.risk.AtMost(tc.ceiling); got != tc.want {
			t.Errorf("Risk(%q).AtMost(%q) = %v, want %v", tc.risk, tc.ceiling, got, tc.want)
		}
	}
}

func TestUndeclaredRiskBecomesCritical(t *testing.T) {
	reset(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{Name: "test.noRisk", Summary: "未声明风险"},
		Fn:   func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
	})
	e, ok := Lookup("test.noRisk")
	if !ok {
		t.Fatal("capability 未注册")
	}
	// Failing closed matters: an un-reviewed capability must not be reachable
	// by an AI just because someone forgot a field.
	if e.Risk != RiskCritical {
		t.Fatalf("Risk = %q, want critical", e.Risk)
	}
	if len(Warnings()) == 0 {
		t.Error("应产生一条警告，提醒补上 Risk")
	}
}

func TestMutatingWithoutDryRunPanics(t *testing.T) {
	reset(t)
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Mutating 且无 DryRun 应当 panic")
		}
		if !strings.Contains(r.(string), "DryRun") {
			t.Errorf("panic 信息应提示 DryRun，实际为 %v", r)
		}
	}()
	Register(Capability[testIn, testOut]{
		Meta: Meta{Name: "test.badWrite", Summary: "写操作缺少预演", Risk: RiskMedium, Mutating: true},
		Fn:   func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
	})
}

func TestPermissionDenied(t *testing.T) {
	reset(t)
	install(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{Name: "test.read", Summary: "读", Perm: "test:read", Risk: RiskLow},
		Fn:   func(_ context.Context, in testIn) (testOut, error) { return testOut{Echo: in.Name}, nil },
	})

	ctx := WithActor(context.Background(), actor([]string{"other:perm"}, SourceHTTP, RiskCritical))
	_, err := InvokeJSON(ctx, "test.read", json.RawMessage(`{"id":1}`))
	if errcode.From(err).Code != errcode.Forbidden {
		t.Fatalf("code = %d, want 403", errcode.From(err).Code)
	}
}

func TestSuperPermBypassesPermissionButNotRisk(t *testing.T) {
	reset(t)
	install(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{
			Name: "test.dangerous", Summary: "危险操作", Perm: "test:danger",
			Risk: RiskCritical, Mutating: true, AIEnabled: true,
		},
		Fn:     func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
		DryRun: func(context.Context, testIn) (*Preview, error) { return &Preview{}, nil },
	})

	// Wildcard permission satisfies Perm but the risk ceiling still applies:
	// being an administrator does not mean the AI may act as one.
	ctx := WithActor(context.Background(), actor([]string{SuperPerm}, SourceAI, RiskHigh))
	_, err := InvokeJSON(ctx, "test.dangerous", json.RawMessage(`{"id":1}`))
	e := errcode.From(err)
	if e.Code != errcode.RiskExceeded {
		t.Fatalf("code = %d, want 423", e.Code)
	}
	detail, ok := e.Detail.(map[string]any)
	if !ok {
		t.Fatalf("423 应携带结构化 detail，实际 %T", e.Detail)
	}
	if detail["hint"] == "" || detail["capability"] != "test.dangerous" {
		t.Errorf("detail 内容不完整: %+v", detail)
	}
}

func TestEntryPointVisibilityReports404(t *testing.T) {
	reset(t)
	install(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{
			Name: "test.pageOnly", Summary: "仅页面可用", Risk: RiskLow,
			AIEnabled: false, MCPEnabled: false,
		},
		Fn: func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
	})

	for _, src := range []Source{SourceAI, SourceMCP} {
		ctx := WithActor(context.Background(), actor([]string{SuperPerm}, src, RiskCritical))
		_, err := InvokeJSON(ctx, "test.pageOnly", json.RawMessage(`{"id":1}`))
		// 404 rather than 403 on purpose: telling a model that a capability
		// exists but is off-limits leaks the catalogue and invites probing.
		if got := errcode.From(err).Code; got != errcode.NotFound {
			t.Errorf("source=%s code = %d, want 404", src, got)
		}
	}

	ctx := WithActor(context.Background(), actor([]string{SuperPerm}, SourceHTTP, RiskCritical))
	if _, err := InvokeJSON(ctx, "test.pageOnly", json.RawMessage(`{"id":1}`)); err != nil {
		t.Errorf("HTTP 入口应当可用，实际 %v", err)
	}
}

func TestUnknownFieldsRejected(t *testing.T) {
	reset(t)
	install(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{Name: "test.strict", Summary: "严格入参", Risk: RiskLow},
		Fn:   func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
	})

	ctx := WithActor(context.Background(), actor([]string{SuperPerm}, SourceHTTP, RiskCritical))
	// A hallucinated parameter must be reported, not silently dropped — the
	// model needs the feedback to correct itself.
	_, err := InvokeJSON(ctx, "test.strict", json.RawMessage(`{"id":1,"bogus":true}`))
	if got := errcode.From(err).Code; got != errcode.BadRequest {
		t.Fatalf("code = %d, want 400", got)
	}
}

func TestValidationApplied(t *testing.T) {
	reset(t)
	install(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{Name: "test.validated", Summary: "校验", Risk: RiskLow},
		Fn:   func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
	})
	ctx := WithActor(context.Background(), actor([]string{SuperPerm}, SourceHTTP, RiskCritical))
	if _, err := InvokeJSON(ctx, "test.validated", json.RawMessage(`{"id":0}`)); errcode.From(err).Code != errcode.BadRequest {
		t.Error("id=0 应当被校验拒绝")
	}
	if _, err := InvokeJSON(ctx, "test.validated", json.RawMessage(`{"id":1,"name":"123456789"}`)); errcode.From(err).Code != errcode.BadRequest {
		t.Error("过长的 name 应当被校验拒绝")
	}
}

func TestPanicContained(t *testing.T) {
	reset(t)
	install(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{Name: "test.panics", Summary: "会 panic", Risk: RiskLow},
		Fn:   func(context.Context, testIn) (testOut, error) { panic("boom") },
	})
	ctx := WithActor(context.Background(), actor([]string{SuperPerm}, SourceHTTP, RiskCritical))
	_, err := InvokeJSON(ctx, "test.panics", json.RawMessage(`{"id":1}`))
	if got := errcode.From(err).Code; got != errcode.InternalError {
		t.Fatalf("code = %d, want 500", got)
	}
}

func TestAuditOnlyForMutating(t *testing.T) {
	reset(t)
	audits := install(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{Name: "test.read2", Summary: "读", Risk: RiskLow, AIEnabled: true},
		Fn:   func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
	})
	Register(Capability[testIn, testOut]{
		Meta: Meta{
			Name: "test.write2", Summary: "写", Risk: RiskMedium, Mutating: true,
			AIEnabled: true, LogTitle: "测试", BizType: BizUpdate,
			Examples: []Example{{Utterance: "改 1 号", Args: `{"id":1}`}},
		},
		Fn:     func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
		DryRun: func(context.Context, testIn) (*Preview, error) { return &Preview{}, nil },
	})

	a := actor([]string{SuperPerm}, SourceAI, RiskCritical)
	a.Type = ActorAgent
	a.Intent = "把 1 号改掉"
	ctx := WithActor(context.Background(), a)

	if _, err := InvokeJSON(ctx, "test.read2", json.RawMessage(`{"id":1}`)); err != nil {
		t.Fatal(err)
	}
	if len(*audits) != 0 {
		t.Errorf("只读操作不应写审计，实际 %d 条", len(*audits))
	}

	if _, err := InvokeJSON(ctx, "test.write2", json.RawMessage(`{"id":1}`)); err != nil {
		t.Fatal(err)
	}
	if len(*audits) != 1 {
		t.Fatalf("写操作应写 1 条审计，实际 %d 条", len(*audits))
	}
	rec := (*audits)[0]
	if rec.ActorType != ActorAgent || rec.Source != SourceAI {
		t.Errorf("审计主体记录错误: %+v", rec)
	}
	if rec.Intent != "把 1 号改掉" {
		t.Errorf("审计应保留 intent 原话，实际 %q", rec.Intent)
	}
	if !rec.Success {
		t.Error("成功的操作应记为 success")
	}
}

func TestAuditRecordsFailure(t *testing.T) {
	reset(t)
	audits := install(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{
			Name: "test.denied", Summary: "无权限的写", Perm: "test:nope",
			Risk: RiskMedium, Mutating: true,
		},
		Fn:     func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
		DryRun: func(context.Context, testIn) (*Preview, error) { return &Preview{}, nil },
	})
	ctx := WithActor(context.Background(), actor(nil, SourceHTTP, RiskCritical))
	_, _ = InvokeJSON(ctx, "test.denied", json.RawMessage(`{"id":1}`))

	// A rejected write is exactly what an operator wants to find later.
	if len(*audits) != 1 || (*audits)[0].Success {
		t.Fatalf("被拒绝的写操作也应写审计且标记失败，实际 %+v", *audits)
	}
}

func TestVisibleTrimsByPermissionEntryAndRisk(t *testing.T) {
	reset(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{Name: "test.a", Summary: "可见", Perm: "test:a", Risk: RiskLow, AIEnabled: true},
		Fn:   func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
	})
	Register(Capability[testIn, testOut]{
		Meta: Meta{Name: "test.b", Summary: "无权限", Perm: "test:b", Risk: RiskLow, AIEnabled: true},
		Fn:   func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
	})
	Register(Capability[testIn, testOut]{
		Meta: Meta{Name: "test.c", Summary: "风险过高", Perm: "test:a", Risk: RiskCritical, AIEnabled: true},
		Fn:   func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
	})
	Register(Capability[testIn, testOut]{
		Meta: Meta{Name: "test.d", Summary: "不对 AI 开放", Perm: "test:a", Risk: RiskLow},
		Fn:   func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
	})

	got := Visible(SourceAI, []string{"test:a"}, RiskHigh)
	if len(got) != 1 || got[0].Name != "test.a" {
		names := make([]string, len(got))
		for i, e := range got {
			names[i] = e.Name
		}
		t.Fatalf("可见目录 = %v, want [test.a]", names)
	}
}

func TestSearchMatchesSummaryAndTags(t *testing.T) {
	reset(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{
			Name: "system.user.list", Summary: "查询用户", Tags: []string{"系统管理"},
			Risk: RiskLow, AIEnabled: true,
		},
		Fn: func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
	})
	for _, kw := range []string{"用户", "system.user", "系统管理", ""} {
		if got := Search(SourceAI, []string{SuperPerm}, RiskHigh, kw, 10); len(got) != 1 {
			t.Errorf("keyword %q 命中 %d 条, want 1", kw, len(got))
		}
	}
	if got := Search(SourceAI, []string{SuperPerm}, RiskHigh, "不存在的关键词", 10); len(got) != 0 {
		t.Errorf("无关关键词应命中 0 条，实际 %d", len(got))
	}
}

func TestSelfCheckRejectsAIWritableConfig(t *testing.T) {
	reset(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{
			Name: "system.aiProvider.update", Summary: "改模型配置",
			Risk: RiskHigh, Mutating: true,
			// This is the mistake the self-check exists to catch: a prompt
			// injection could repoint base_url and exfiltrate every session.
			AIEnabled: true,
		},
		Fn:     func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
		DryRun: func(context.Context, testIn) (*Preview, error) { return &Preview{}, nil },
	})
	err := SelfCheck()
	if err == nil {
		t.Fatal("配置类写操作对 AI 开放时，自检必须失败")
	}
	if !strings.Contains(err.Error(), "system.aiProvider.update") {
		t.Errorf("错误信息应指出具体 capability，实际 %v", err)
	}
}

func TestSelfCheckPassesForProperConfigCapability(t *testing.T) {
	reset(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{
			Name: "system.storage.update", Summary: "改存储配置",
			Risk: RiskHigh, Mutating: true, AIEnabled: false, MCPEnabled: false,
		},
		Fn:     func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
		DryRun: func(context.Context, testIn) (*Preview, error) { return &Preview{}, nil },
	})
	if err := SelfCheck(); err != nil {
		t.Fatalf("合规的配置类 capability 不应被拒绝: %v", err)
	}
}

func TestPreviewIsEnrichedAndReadOnly(t *testing.T) {
	reset(t)
	var readOnlyUsed bool
	Install(Hooks{
		ReadOnly: func(ctx context.Context, fn func(context.Context) error) error {
			readOnlyUsed = true
			return fn(ctx)
		},
	})
	t.Cleanup(func() { Install(Hooks{}) })

	Register(Capability[testIn, testOut]{
		Meta: Meta{
			Name: "test.previewed", Summary: "有预演", Risk: RiskHigh,
			Mutating: true, Reversible: true,
		},
		Fn: func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
		DryRun: func(context.Context, testIn) (*Preview, error) {
			p := &Preview{}
			p.AddChange(Change{Entity: "t", ID: "1", Op: OpUpdate})
			return p, nil
		},
		Undo: func(context.Context, json.RawMessage) error { return nil },
	})

	// A preview is low risk by definition: the point is letting a caller see
	// the consequences of something it may not be allowed to perform.
	ctx := WithActor(context.Background(), actor([]string{SuperPerm}, SourceHTTP, RiskLow))
	p, err := RunPreview(ctx, "test.previewed", json.RawMessage(`{"id":1}`))
	if err != nil {
		t.Fatalf("预演不应受风险门槛限制: %v", err)
	}
	if !readOnlyUsed {
		t.Error("预演必须在只读上下文中执行")
	}
	if p.Capability != "test.previewed" || p.Risk != RiskHigh || !p.Reversible {
		t.Errorf("预演结果未被执行器补全: %+v", p)
	}
	if p.Summary == "" {
		t.Error("预演应有默认摘要")
	}
	if p.Affected != 1 {
		t.Errorf("Affected = %d, want 1", p.Affected)
	}
}

func TestPreviewTruncatesButKeepsCounts(t *testing.T) {
	p := &Preview{}
	for i := range MaxPreviewChanges + 50 {
		p.AddChange(Change{Entity: "t", ID: ID(i), Op: OpUpdate})
	}
	if p.Affected != MaxPreviewChanges+50 {
		t.Errorf("Affected = %d, 应统计全部", p.Affected)
	}
	if len(p.Changes) != MaxPreviewChanges {
		t.Errorf("列出的变更 = %d, want %d", len(p.Changes), MaxPreviewChanges)
	}
	if !p.Truncated {
		t.Error("超出上限时应标记 Truncated")
	}
}

func TestPreviewWarnDeduplicates(t *testing.T) {
	p := &Preview{}
	p.Warn("跳过超管账号")
	p.Warn("跳过超管账号")
	p.Warn("另一条")
	if len(p.Warnings) != 2 {
		t.Errorf("Warnings = %v, 应去重", p.Warnings)
	}
}

func TestMissingActorIsUnauthorized(t *testing.T) {
	reset(t)
	install(t)
	Register(Capability[testIn, testOut]{
		Meta: Meta{Name: "test.needsActor", Summary: "需要身份", Risk: RiskLow},
		Fn:   func(context.Context, testIn) (testOut, error) { return testOut{}, nil },
	})
	_, err := InvokeJSON(context.Background(), "test.needsActor", json.RawMessage(`{"id":1}`))
	if got := errcode.From(err).Code; got != errcode.Unauthorized {
		t.Fatalf("code = %d, want 401", got)
	}
}

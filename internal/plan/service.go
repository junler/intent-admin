// Package plan implements persisted, owner-bound preview/confirm/apply plans.
package plan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/changeset"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type Options struct {
	TTL         time.Duration
	MaxSteps    int
	MaxAffected int
	MaxRisk     capability.Risk
	Changesets  *changeset.Service
}
type Service struct {
	db         *gorm.DB
	opts       Options
	changesets *changeset.Service
}

func New(db *gorm.DB, opts Options) *Service {
	if opts.TTL <= 0 {
		opts.TTL = 30 * time.Minute
	}
	if opts.MaxSteps <= 0 {
		opts.MaxSteps = 20
	}
	if opts.MaxAffected <= 0 {
		opts.MaxAffected = 200
	}
	if !opts.MaxRisk.Valid() {
		opts.MaxRisk = capability.RiskHigh
	}
	if opts.Changesets == nil {
		opts.Changesets = changeset.New(db)
	}
	return &Service{db: db, opts: opts, changesets: opts.Changesets}
}

type Step struct {
	Seq          int                 `json:"seq"`
	Capability   string              `json:"capability"`
	Args         json.RawMessage     `json:"args"`
	Note         string              `json:"note"`
	Preview      *capability.Preview `json:"preview,omitempty"`
	Result       any                 `json:"result,omitempty"`
	Status       string              `json:"status"`
	Error        string              `json:"error,omitempty"`
	ResolvedArgs json.RawMessage     `json:"resolvedArgs,omitempty"`
	Fingerprint  string              `json:"fingerprint,omitempty"`
}
type Plan struct {
	ID          string          `json:"id"`
	Intent      string          `json:"intent"`
	SessionID   string          `json:"sessionId,omitempty"`
	Steps       []Step          `json:"steps"`
	MaxRisk     capability.Risk `json:"maxRisk"`
	Affected    int             `json:"affected"`
	Mode        string          `json:"mode"`
	Status      string          `json:"status"`
	ChangesetID string          `json:"changesetId,omitempty"`
	CreateTime  time.Time       `json:"createTime"`
	ExpiresAt   time.Time       `json:"expiresAt"`
	PageContext map[string]any  `json:"pageContext,omitempty"`
	OnError     string          `json:"onError"`
	UserID      int64           `json:"-"`
}
type CreateIn struct {
	Intent      string         `json:"intent"`
	SessionID   string         `json:"sessionId,omitempty"`
	Steps       []Step         `json:"steps"`
	PageContext map[string]any `json:"pageContext,omitempty"`
	OnError     string         `json:"onError,omitempty"`
}
type ApplyIn struct {
	Mode    string `json:"mode"`
	StepSeq *int   `json:"stepSeq,omitempty"`
}
type row struct {
	ID            string `gorm:"column:plan_id;primaryKey"`
	UserID        int64
	SessionID     string
	Intent        string
	Steps         json.RawMessage `gorm:"type:jsonb"`
	MaxRisk       capability.Risk
	AffectedCount int
	Mode          string
	Status        string
	PageContext   json.RawMessage `gorm:"type:jsonb"`
	CreateTime    time.Time
	ExpireTime    time.Time
}

func (row) TableName() string      { return "sys_ai_plan" }
func encode(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

type metadata struct {
	Context     map[string]any `json:"context"`
	OnError     string         `json:"onError"`
	ChangesetID string         `json:"changesetId,omitempty"`
}

func toRow(p *Plan) row {
	return row{ID: p.ID, UserID: p.UserID, SessionID: p.SessionID, Intent: p.Intent, Steps: encode(p.Steps), MaxRisk: p.MaxRisk, AffectedCount: p.Affected, Mode: p.Mode, Status: p.Status, PageContext: encode(metadata{Context: p.PageContext, OnError: p.OnError, ChangesetID: p.ChangesetID}), CreateTime: p.CreateTime, ExpireTime: p.ExpiresAt}
}
func fromRow(r row) (*Plan, error) {
	p := &Plan{ID: r.ID, UserID: r.UserID, SessionID: r.SessionID, Intent: r.Intent, MaxRisk: r.MaxRisk, Affected: r.AffectedCount, Mode: r.Mode, Status: r.Status, CreateTime: r.CreateTime, ExpiresAt: r.ExpireTime}
	var meta metadata
	if err := decodeJSON(r.Steps, &p.Steps); err != nil {
		return nil, err
	}
	if len(r.PageContext) > 0 {
		if err := decodeJSON(r.PageContext, &meta); err != nil {
			return nil, err
		}
	}
	p.PageContext, p.OnError, p.ChangesetID = meta.Context, meta.OnError, meta.ChangesetID
	return p, nil
}
func (s *Service) principal(ctx context.Context) (context.Context, *capability.Actor, error) {
	a, ok := capability.ActorFrom(ctx)
	if !ok {
		return ctx, nil, errcode.Unauthorizedf("请先登录")
	}
	if !a.Can("ai:agent:use") {
		return ctx, nil, errcode.Forbiddenf("没有使用 AI 计划的权限")
	}
	copy := *a
	copy.Source = capability.SourceAI
	copy.Type = capability.ActorAgent
	if !copy.MaxRisk.Valid() || s.opts.MaxRisk.Level() < copy.MaxRisk.Level() {
		copy.MaxRisk = s.opts.MaxRisk
	}
	return capability.WithActor(ctx, &copy), &copy, nil
}
func (s *Service) save(ctx context.Context, p *Plan) error {
	r := toRow(p)
	return dbx.DBFrom(ctx, s.db).Model(&row{}).Where("plan_id=? AND user_id=?", p.ID, p.UserID).Updates(map[string]any{"steps": r.Steps, "max_risk": r.MaxRisk, "affected_count": r.AffectedCount, "mode": r.Mode, "status": r.Status, "page_context": r.PageContext, "expire_time": r.ExpireTime}).Error
}
func (s *Service) load(ctx context.Context, id string, lock bool) (*Plan, error) {
	_, a, err := s.principal(ctx)
	if err != nil {
		return nil, err
	}
	q := dbx.DBFrom(ctx, s.db)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var r row
	if err := q.Where("plan_id=? AND user_id=?", id, a.UserID).First(&r).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFoundf("计划不存在")
		}
		return nil, err
	}
	return fromRow(r)
}
func (s *Service) Get(ctx context.Context, id string) (*Plan, error) { return s.load(ctx, id, false) }
func (s *Service) List(ctx context.Context) ([]Plan, error) {
	_, a, err := s.principal(ctx)
	if err != nil {
		return nil, err
	}
	rows := []row{}
	if err := dbx.DBFrom(ctx, s.db).Where("user_id=?", a.UserID).Order("create_time DESC").Limit(100).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := []Plan{}
	for _, r := range rows {
		p, err := fromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, nil
}
func (s *Service) Create(ctx context.Context, in CreateIn) (*Plan, error) {
	ctx, a, err := s.principal(ctx)
	if err != nil {
		return nil, err
	}
	if len(in.Steps) == 0 || len(in.Steps) > s.opts.MaxSteps {
		return nil, errcode.BadRequestf("计划步骤数必须在 1 到 %d 之间", s.opts.MaxSteps)
	}
	if len([]rune(in.Intent)) > 10000 {
		return nil, errcode.BadRequestf("计划意图过长")
	}
	if in.OnError == "" {
		in.OnError = "stop"
	}
	if in.OnError != "stop" && in.OnError != "continue" {
		return nil, errcode.BadRequestf("onError 只能为 stop 或 continue")
	}
	if ids, ok := in.PageContext["selectedIds"].([]any); ok && len(ids) > 500 {
		return nil, errcode.BadRequestf("最多选择500条记录")
	}
	if ids, ok := in.PageContext["visibleIds"].([]any); ok && len(ids) > 200 {
		return nil, errcode.BadRequestf("最多提供200条可见记录")
	}
	now := time.Now()
	p := &Plan{ID: uuid.NewString(), UserID: a.UserID, Intent: in.Intent, SessionID: in.SessionID, Steps: in.Steps, PageContext: in.PageContext, OnError: in.OnError, MaxRisk: capability.RiskLow, Mode: "all", Status: "draft", CreateTime: now, ExpiresAt: now.Add(s.opts.TTL)}
	for i := range p.Steps {
		if err := rejectSensitiveArguments(in.Steps[i].Args); err != nil {
			return nil, err
		}
		p.Steps[i] = Step{Seq: i, Capability: in.Steps[i].Capability, Args: in.Steps[i].Args, Note: in.Steps[i].Note, Status: "pending"}
		if _, err := s.entry(a, p.Steps[i].Capability); err != nil {
			return nil, err
		}
	}
	if err := s.preview(ctx, p); err != nil {
		return nil, err
	}
	r := toRow(p)
	if err := dbx.DBFrom(ctx, s.db).Create(&r).Error; err != nil {
		return nil, err
	}
	return p, nil
}
func (s *Service) entry(a *capability.Actor, name string) (*capability.Entry, error) {
	e, ok := capability.Lookup(name)
	if !ok || !e.AIEnabled {
		return nil, errcode.NotFoundf("功能不可用于 AI 计划")
	}
	if !a.Can(e.Perm) {
		return nil, errcode.Forbiddenf("没有权限执行 %s", e.Summary)
	}
	return e, nil
}
func fingerprint(args json.RawMessage, p *capability.Preview) string {
	changes := append([]capability.Change(nil), p.Changes...)
	for i := range changes {
		changes[i].Fields = append([]capability.FieldDiff(nil), changes[i].Fields...)
		sort.Slice(changes[i].Fields, func(a, b int) bool { return changes[i].Fields[a].Field < changes[i].Fields[b].Field })
	}
	sort.Slice(changes, func(i, j int) bool {
		a, b := changes[i], changes[j]
		if a.Entity != b.Entity {
			return a.Entity < b.Entity
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Op < b.Op
	})
	v := struct {
		Args      json.RawMessage     `json:"args"`
		Affected  int                 `json:"affected"`
		Skipped   int                 `json:"skipped"`
		Changes   []capability.Change `json:"changes"`
		Truncated bool                `json:"truncated"`
	}{args, p.Affected, p.Skipped, changes, p.Truncated}
	digest := sha256.Sum256(encode(v))
	return hex.EncodeToString(digest[:])
}
func (s *Service) preview(ctx context.Context, p *Plan) error {
	ctx, a, err := s.principal(ctx)
	if err != nil {
		return err
	}
	a.Intent = p.Intent
	a.PlanID = p.ID
	p.Affected = 0
	p.MaxRisk = capability.RiskLow
	for i := range p.Steps {
		step := &p.Steps[i]
		e, err := s.entry(a, step.Capability)
		if err != nil {
			return err
		}
		if e.Risk.Level() > p.MaxRisk.Level() {
			p.MaxRisk = e.Risk
		}
		if step.Status == "applied" {
			continue
		}
		resolved, err := Resolve(step.Args, p.Steps[:i], p.PageContext)
		if errors.Is(err, ErrPendingReference) {
			if strings.HasPrefix(e.Name, "system.config.") {
				var pending map[string]any
				_ = decodeJSON(step.Args, &pending)
				if _, ok := pending["configValue"]; ok {
					return errcode.BadRequestf("配置值依赖写操作结果，请先完成前一步后重建计划")
				}
			}
			step.Status = "pending"
			step.Preview = &capability.Preview{Summary: step.Note, Warnings: []string{"依赖上一步写操作的结果，请分步执行并确认实际值"}, Reversible: e.Reversible}
			step.Fingerprint = ""
			continue
		}
		if err != nil {
			return err
		}
		if err := s.rejectCredentialConfig(ctx, e.Name, resolved); err != nil {
			return err
		}
		step.ResolvedArgs = resolved
		if !e.Mutating {
			if e.Risk != capability.RiskLow {
				return errcode.New(errcode.RiskExceeded, "预演不能执行有风险的查询")
			}
			out, err := capability.InvokeJSON(dbx.WithReadOnly(ctx), e.Name, resolved)
			if err != nil {
				return err
			}
			step.Result = capability.SanitizeValue(out)
			step.Status = "previewed"
			step.Preview = nil
			continue
		}
		preview, err := capability.RunPreview(ctx, e.Name, resolved)
		if err != nil {
			return err
		}
		step.Preview = preview
		step.Fingerprint = fingerprint(resolved, preview)
		step.Status = "previewed"
		step.Error = ""
		p.Affected += preview.Affected
		if preview.Truncated {
			return errcode.BadRequestf("预演超过可逐项确认的上限，请缩小范围")
		}
		if p.Affected > s.opts.MaxAffected {
			return errcode.BadRequestf("计划影响 %d 条，超过上限 %d", p.Affected, s.opts.MaxAffected)
		}
	}
	p.Status = "previewed"
	p.ExpiresAt = time.Now().Add(s.opts.TTL)
	return nil
}
func (s *Service) Preview(ctx context.Context, id string) (*Plan, error) {
	var p *Plan
	err := dbx.NewTxRunner(s.db)(ctx, func(txctx context.Context) error {
		var e error
		p, e = s.load(txctx, id, true)
		if e != nil {
			return e
		}
		if p.Status == "done" || p.Status == "aborted" {
			return errcode.Conflictf("该计划不能再次预演")
		}
		if e = s.preview(txctx, p); e != nil {
			return e
		}
		return s.save(txctx, p)
	})
	return p, err
}
func (s *Service) Apply(ctx context.Context, id string, in ApplyIn) (*Plan, error) {
	ctx, a, err := s.principal(ctx)
	if err != nil {
		return nil, err
	}
	if in.Mode == "" {
		in.Mode = "all"
	}
	if in.Mode != "all" && in.Mode != "stepwise" {
		return nil, errcode.BadRequestf("mode 必须是 all 或 stepwise")
	}
	var result *Plan
	err = dbx.NewTxRunner(s.db)(ctx, func(txctx context.Context) error {
		p, e := s.load(txctx, id, true)
		if e != nil {
			return e
		}
		result = p
		if p.Status != "previewed" && p.Status != "partial" {
			return errcode.Conflictf("计划需要先预演或已执行")
		}
		if time.Now().After(p.ExpiresAt) {
			return errcode.New(errcode.StaleData, "计划已过期，请重新预演")
		}
		// Locks cover filtered-set phantoms as well as existing rows. They are held
		// through re-preview and execution, closing the check/write race.
		if e = s.lockBusiness(txctx); e != nil {
			return e
		}
		approved := append([]Step(nil), p.Steps...)
		freshBytes := encode(p)
		var fresh Plan
		if e = decodeJSON(freshBytes, &fresh); e != nil {
			return e
		}
		fresh.UserID = p.UserID
		if e = s.preview(txctx, &fresh); e != nil {
			return e
		}
		for i := range approved {
			if approved[i].Status == "applied" {
				continue
			}
			if approved[i].Fingerprint != "" && approved[i].Fingerprint != fresh.Steps[i].Fingerprint {
				return errcode.New(errcode.StaleData, "数据已变化，请重新预演并确认").WithDetail(map[string]any{"planId": id, "stepSeq": i})
			}
		}
		p.Steps = fresh.Steps
		p.Affected = fresh.Affected
		p.MaxRisk = fresh.MaxRisk
		p.Mode = in.Mode
		a.Intent = p.Intent
		a.PlanID = p.ID
		a.ApprovalMode = "confirmed"
		if p.MaxRisk.Level() >= capability.RiskHigh.Level() {
			a.ApprovalMode = "diff_confirmed"
		}
		a.ChangesetID = p.ChangesetID
		chosen := -1
		for i, step := range p.Steps {
			if step.Status != "applied" {
				entry, _ := capability.Lookup(step.Capability)
				if entry.Mutating {
					chosen = i
					break
				}
			}
		}
		if in.Mode == "stepwise" && in.StepSeq != nil && *in.StepSeq != chosen {
			return errcode.Conflictf("必须按步骤顺序确认当前步骤")
		}
		if in.Mode == "all" {
			for _, step := range p.Steps {
				if step.Status == "pending" {
					return errcode.BadRequestf("依赖写操作结果的计划需要分步执行")
				}
			}
		}
		if a.ChangesetID == "" {
			a.ChangesetID, e = s.changesets.Begin(txctx, a, p.Intent)
			if e != nil {
				return e
			}
			p.ChangesetID = a.ChangesetID
		}
		failed := false
		executedWrite := false
		for i := range p.Steps {
			step := &p.Steps[i]
			if step.Status == "applied" {
				continue
			}
			entry, e := s.entry(a, step.Capability)
			if e != nil {
				return e
			}
			if !entry.Mutating {
				step.Status = "applied"
				continue
			}
			if in.Mode == "stepwise" && executedWrite {
				break
			}
			if step.Status == "pending" {
				break
			}
			if !entry.Risk.AtMost(a.MaxRisk) || entry.Risk == capability.RiskCritical {
				return errcode.New(errcode.RiskExceeded, "计划含超出 AI 风险上限的操作，请在页面手动执行").WithDetail(map[string]any{"capability": entry.Name, "risk": entry.Risk, "allowed": a.MaxRisk})
			}
			resolved, e := Resolve(step.Args, p.Steps[:i], p.PageContext)
			if e != nil {
				return e
			}
			out, invokeErr := capability.InvokeJSON(capability.WithActor(txctx, a), entry.Name, resolved)
			if invokeErr != nil {
				step.Status = "failed"
				step.Error = errcode.From(invokeErr).Msg
				failed = true
				if p.OnError != "continue" {
					break
				}
				continue
			}
			step.Status = "applied"
			step.Result = capability.SanitizeValue(out)
			executedWrite = true
		}
		p.Status = "done"
		for _, step := range p.Steps {
			if step.Status != "applied" {
				p.Status = "partial"
				break
			}
		}
		if failed {
			if e := s.changesets.Finish(txctx, p.ChangesetID, "partial"); e != nil {
				return e
			}
		}
		if p.Status == "partial" && !failed {
			if e := s.preview(txctx, p); e != nil {
				return e
			}
		}
		return s.save(txctx, p)
	})
	return result, err
}
func (s *Service) Abort(ctx context.Context, id string) (*Plan, error) {
	var p *Plan
	err := dbx.NewTxRunner(s.db)(ctx, func(txctx context.Context) error {
		var e error
		p, e = s.load(txctx, id, true)
		if e != nil {
			return e
		}
		if p.Status == "done" {
			return errcode.Conflictf("已完成计划不能取消")
		}
		p.Status = "aborted"
		return s.save(txctx, p)
	})
	return p, err
}
func (s *Service) lockBusiness(ctx context.Context) error {
	var tables []string
	if err := dbx.DBFrom(ctx, s.db).Raw(`SELECT tablename FROM pg_tables WHERE schemaname=current_schema() AND tablename NOT IN ('schema_migrations','sys_ai_plan','sys_changeset','sys_changeset_item','sys_oper_log','sys_login_log','sys_job_log') ORDER BY tablename`).Scan(&tables).Error; err != nil {
		return err
	}
	return s.changesets.LockEntities(ctx, tables)
}

func (p *Plan) String() string { return fmt.Sprintf("%s (%s, %d steps)", p.ID, p.Status, len(p.Steps)) }

// Plans persist their arguments for later confirmation. Secret-bearing calls
// belong to dedicated human forms and must never turn into stored model input.
func rejectSensitiveArguments(args json.RawMessage) error {
	if len(args) == 0 {
		return nil
	}
	var value any
	if err := decodeJSON(args, &value); err != nil {
		return errcode.BadRequestf("计划参数不是有效 JSON")
	}
	var walk func(any) error
	walk = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				if capability.IsSensitive(k) && val != nil && val != "" {
					return errcode.BadRequestf("包含凭据的操作必须在专用页面执行，不能保存到 AI 计划")
				}
				if err := walk(val); err != nil {
					return err
				}
			}
		case []any:
			for _, val := range x {
				if err := walk(val); err != nil {
					return err
				}
			}
		case string:
			if len(x) > 0 && (x[0] == '{' || x[0] == '[') {
				var nested any
				if decodeJSON([]byte(x), &nested) == nil {
					return walk(nested)
				}
			}
		}
		return nil
	}
	return walk(value)
}

// ConfirmedRun gives other confirmation adapters the same atomic lock/recheck/
// execute boundary as Plan.Apply. The adapter still authenticates its subject,
// consumes its one-use confirmation and invokes the capability executor.
func (s *Service) ConfirmedRun(ctx context.Context, fn func(context.Context) error) error {
	return dbx.NewTxRunner(s.db)(ctx, func(txctx context.Context) error {
		if err := s.lockBusiness(txctx); err != nil {
			return err
		}
		return fn(txctx)
	})
}

func (s *Service) rejectCredentialConfig(ctx context.Context, name string, args json.RawMessage) error {
	if !strings.HasPrefix(name, "system.config.") {
		return nil
	}
	var input map[string]any
	if decodeJSON(args, &input) != nil {
		return errcode.BadRequestf("配置参数无效")
	}
	_, hasValue := input["configValue"]
	if !hasValue {
		return nil
	}
	key, _ := input["configKey"].(string)
	secret := capability.IsSensitive(key)
	id := input["configId"]
	if id == nil {
		id = input["id"]
	}
	if id != nil {
		var existing string
		if err := dbx.DBFrom(ctx, s.db).Table("sys_config").Select("config_key").Where("config_id=?", id).Scan(&existing).Error; err != nil {
			return err
		}
		secret = secret || capability.IsSensitive(existing)
	}
	if secret {
		return errcode.BadRequestf("凭据类系统参数必须在专用页面修改，不能保存到 AI 计划")
	}
	return nil
}

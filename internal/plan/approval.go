package plan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
)

const ApprovalPermission = "ai:plan:approve"

// ApprovalNotification carries routing metadata only. Business previews stay
// behind the participant-bound approval endpoint and its live grant checks.
type ApprovalNotification struct {
	UserID     int64
	ApprovalID string
	PlanID     string
	Status     string
}

type Approval struct {
	ID              string     `json:"id"`
	PlanID          string     `json:"planId"`
	ProposerID      int64      `json:"proposerId"`
	ProposerName    string     `json:"proposerName"`
	ReviewerID      int64      `json:"reviewerId"`
	ReviewerName    string     `json:"reviewerName"`
	Status          string     `json:"status"`
	Fingerprint     string     `json:"fingerprint"`
	RequestComment  string     `json:"requestComment"`
	DecisionComment string     `json:"decisionComment"`
	CreateTime      time.Time  `json:"createTime"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	DecisionTime    *time.Time `json:"decisionTime,omitempty"`
	UpdateTime      time.Time  `json:"updateTime"`
	ChangesetID     string     `json:"changesetId,omitempty"`
	Plan            *Plan      `json:"plan,omitempty"`
}

// Use an explicit database representation: response-only Plan and ExpiresAt
// must never become inferred columns in GORM's embedded model.
type storedApproval struct {
	ID              string `gorm:"column:approval_id;primaryKey"`
	PlanID          string
	ProposerID      int64
	ProposerName    string
	ReviewerID      int64
	ReviewerName    string
	Status          string
	Fingerprint     string
	Snapshot        json.RawMessage `gorm:"type:jsonb"`
	RequestComment  string
	DecisionComment string
	CreateTime      time.Time
	ExpireTime      time.Time
	DecisionTime    *time.Time
	UpdateTime      time.Time
	ChangesetID     string
}

func (storedApproval) TableName() string { return "sys_ai_plan_approval" }
func (r storedApproval) public(detail bool) (*Approval, error) {
	a := &Approval{ID: r.ID, PlanID: r.PlanID, ProposerID: r.ProposerID, ProposerName: r.ProposerName, ReviewerID: r.ReviewerID, ReviewerName: r.ReviewerName, Status: r.Status, Fingerprint: r.Fingerprint, RequestComment: r.RequestComment, DecisionComment: r.DecisionComment, CreateTime: r.CreateTime, ExpiresAt: r.ExpireTime, DecisionTime: r.DecisionTime, UpdateTime: r.UpdateTime, ChangesetID: r.ChangesetID}
	if (a.Status == "pending" || a.Status == "approved") && time.Now().After(a.ExpiresAt) {
		a.Status = "expired"
	}
	if detail {
		if err := decodeJSON(r.Snapshot, &a.Plan); err != nil {
			return nil, err
		}
	}
	return a, nil
}

type RequestApprovalIn struct {
	ReviewerID int64  `json:"reviewerId"`
	Comment    string `json:"comment"`
	TTLMinutes int    `json:"ttlMinutes,omitempty"`
}
type ApprovalDecisionIn struct {
	Comment string `json:"comment"`
}
type ApprovalQuery struct {
	paginate.Query
	Box    string `form:"box" json:"box"`
	Status string `form:"status" json:"status"`
}
type Reviewer struct {
	UserID   int64  `json:"userId"`
	UserName string `json:"userName"`
}

type approvalExecutionKey struct{}

func approvedContext(ctx context.Context, id string) bool {
	value, _ := ctx.Value(approvalExecutionKey{}).(string)
	return id != "" && value == id
}
func human(ctx context.Context) (*capability.Actor, error) {
	a, ok := capability.ActorFrom(ctx)
	if !ok || a.UserID <= 0 {
		return nil, errcode.Unauthorizedf("请先登录")
	}
	if a.Type != capability.ActorHuman || a.Source != capability.SourceHTTP || a.TokenID != 0 {
		return nil, errcode.Forbiddenf("跨人审批只能由登录管理页面的本人操作")
	}
	if !a.Can("ai:agent:use") {
		return nil, errcode.Forbiddenf("没有使用 AI 计划的权限")
	}
	return a, nil
}
func (s *Service) liveActor(ctx context.Context, id int64) (*capability.Actor, error) {
	if s.ActorLoader == nil {
		return nil, errcode.Forbiddenf("跨人审批尚未配置实时权限校验")
	}
	a, err := s.ActorLoader(ctx, id)
	if err != nil {
		return nil, err
	}
	if a == nil || a.UserID != id || id <= 0 || !a.Can("ai:agent:use") {
		return nil, errcode.Forbiddenf("审批参与人没有使用 AI 计划的权限")
	}
	copy := *a
	copy.Type = capability.ActorHuman
	copy.Source = capability.SourceHTTP
	copy.TokenID = 0
	if !copy.MaxRisk.Valid() {
		return nil, errcode.New(errcode.RiskExceeded, "审批参与人的 AI 风险上限无效")
	}
	if copy.MaxRisk.Level() > s.opts.MaxRisk.Level() {
		copy.MaxRisk = s.opts.MaxRisk
	}
	return &copy, nil
}
func (s *Service) validateApprovalActor(a *capability.Actor, p *Plan, reviewer bool) error {
	if reviewer && !a.Can(ApprovalPermission) {
		return errcode.Forbiddenf("没有计划审批权限")
	}
	for _, step := range p.Steps {
		e, err := s.entry(a, step.Capability)
		if err != nil {
			return err
		}
		if e.Risk == capability.RiskCritical || !e.Risk.AtMost(a.MaxRisk) {
			return errcode.New(errcode.RiskExceeded, "计划超出审批参与人的 AI 风险上限，请在管理页面手动执行")
		}
	}
	return nil
}
func approvalFingerprint(p *Plan) string {
	// Read results and resolved references are part of the approved instruction.
	// Per-step fingerprints already canonicalize current changed field values.
	type approvedStep struct {
		Capability   string
		Args         any
		ResolvedArgs any
		Fingerprint  string
		Result       any
		Status       string
		Risk         capability.Risk
		Permission   string
		AIEnabled    bool
		Mutating     bool
	}
	steps := make([]approvedStep, len(p.Steps))
	for i, step := range p.Steps {
		var args, resolved any
		_ = decodeJSON(step.Args, &args)
		_ = decodeJSON(step.ResolvedArgs, &resolved)
		value := approvedStep{Capability: step.Capability, Args: args, ResolvedArgs: resolved, Fingerprint: step.Fingerprint, Result: step.Result, Status: step.Status}
		if e, ok := capability.Lookup(step.Capability); ok {
			value.Risk, value.Permission, value.AIEnabled, value.Mutating = e.Risk, e.Perm, e.AIEnabled, e.Mutating
		}
		steps[i] = value
	}
	digest := sha256.Sum256(encode(struct {
		ID, Intent, OnError string
		Context             map[string]any
		Steps               []approvedStep
		Risk                capability.Risk
		Affected            int
	}{p.ID, p.Intent, p.OnError, p.PageContext, steps, p.MaxRisk, p.Affected}))
	return hex.EncodeToString(digest[:])
}
func (s *Service) refreshed(ctx context.Context, p *Plan, a *capability.Actor) (*Plan, error) {
	fresh, err := fromRow(toRow(p))
	if err != nil {
		return nil, err
	}
	if err = s.validateApprovalActor(a, fresh, false); err != nil {
		return nil, err
	}
	if err = s.preview(capability.WithActor(ctx, a), fresh); err != nil {
		return nil, err
	}
	return fresh, nil
}
func (s *Service) approvalRow(ctx context.Context, id string, lock bool) (*storedApproval, error) {
	q := dbx.DBFrom(ctx, s.db)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var r storedApproval
	if err := q.Where("approval_id=?", id).First(&r).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFoundf("审批记录不存在")
		}
		return nil, err
	}
	return &r, nil
}
func (s *Service) attachApproval(ctx context.Context, p *Plan) error {
	if p.ApprovalID == "" {
		p.Approval = nil
		return nil
	}
	r, err := s.approvalRow(ctx, p.ApprovalID, false)
	if err != nil {
		return err
	}
	if r.PlanID != p.ID || r.ProposerID != p.UserID {
		return errcode.Internalf("审批记录与计划不匹配")
	}
	p.Approval, err = r.public(false)
	return err
}
func (s *Service) setApprovalStatus(ctx context.Context, r *storedApproval, status, comment string) error {
	r.Status, r.UpdateTime = status, time.Now()
	values := map[string]any{"status": status, "update_time": r.UpdateTime, "changeset_id": r.ChangesetID}
	if status == "approved" || status == "rejected" || status == "revoked" {
		r.DecisionComment = comment
		r.DecisionTime = &r.UpdateTime
		values["decision_comment"], values["decision_time"] = comment, r.DecisionTime
	}
	return dbx.DBFrom(ctx, s.db).Model(&storedApproval{}).Where("approval_id=?", r.ID).Updates(values).Error
}
func (s *Service) invalidateApproval(ctx context.Context, p *Plan, status string) error {
	if p.ApprovalID == "" {
		return nil
	}
	r, err := s.approvalRow(ctx, p.ApprovalID, true)
	if err != nil {
		return err
	}
	if r.Status == "pending" || r.Status == "approved" {
		if err = s.setApprovalStatus(ctx, r, status, ""); err != nil {
			return err
		}
		if a, ok := capability.ActorFrom(ctx); ok {
			s.auditApproval(ctx, a, r, "invalidate")
		}
		if err = s.notifyApproval(ctx, r, r.ReviewerID); err != nil {
			return err
		}
	}
	if status == "revoked" {
		p.ApprovalID = ""
		p.Approval = nil
	}
	return nil
}
func (s *Service) auditApproval(ctx context.Context, a *capability.Actor, r *storedApproval, action string) {
	risk := capability.RiskLow
	var snapshot Plan
	if decodeJSON(r.Snapshot, &snapshot) == nil && snapshot.MaxRisk.Valid() {
		risk = snapshot.MaxRisk
	}
	approvedBy := ""
	if r.Status == "approved" || r.Status == "applied" {
		approvedBy = strconv.FormatInt(r.ReviewerID, 10)
	}
	s.changesets.WriteAudit(ctx, capability.AuditRecord{Capability: "ai.plan.approval." + action, Title: "AI 计划跨人审批", BizType: 2, Risk: risk, ActorType: a.Type, Source: a.Source, UserID: a.UserID, UserName: a.UserName, IP: a.IP, UserAgent: a.UserAgent, PlanID: r.PlanID, ChangesetID: r.ChangesetID, ApprovalMode: "cross_human", ApprovedBy: approvedBy, Param: string(encode(map[string]any{"approvalId": r.ID, "reviewerId": r.ReviewerID, "status": r.Status})), Success: true, OccurredAt: time.Now()})
}
func (s *Service) notifyApproval(ctx context.Context, r *storedApproval, userID int64) error {
	if s.Notify == nil {
		return nil
	}
	return s.Notify(ctx, ApprovalNotification{UserID: userID, ApprovalID: r.ID, PlanID: r.PlanID, Status: r.Status})
}
func (s *Service) RequestApproval(ctx context.Context, id string, in RequestApprovalIn) (*Approval, error) {
	a, err := human(ctx)
	if err != nil {
		return nil, err
	}
	if in.ReviewerID <= 0 || in.ReviewerID == a.UserID {
		return nil, errcode.BadRequestf("请选择另一位有权限的审批人")
	}
	if len([]rune(in.Comment)) > 1000 {
		return nil, errcode.BadRequestf("审批说明最多1000字")
	}
	if in.TTLMinutes == 0 {
		in.TTLMinutes = 30
	}
	if in.TTLMinutes < 1 || in.TTLMinutes > 1440 {
		return nil, errcode.BadRequestf("审批有效期必须在1到1440分钟之间")
	}
	var result *Approval
	err = dbx.NewTxRunner(s.db)(ctx, func(txctx context.Context) error {
		p, e := s.load(txctx, id, true)
		if e != nil {
			return e
		}
		if p.Status != "previewed" || p.ChangesetID != "" {
			return errcode.Conflictf("只有尚未执行的完整预演计划可以发起审批")
		}
		if time.Now().After(p.ExpiresAt) {
			return errcode.New(errcode.StaleData, "计划已过期，请重新预演后发起审批")
		}
		if e = s.lockBusiness(txctx); e != nil {
			return e
		}
		proposer, e := s.liveActor(txctx, a.UserID)
		if e != nil {
			return e
		}
		reviewer, e := s.liveActor(txctx, in.ReviewerID)
		if e != nil {
			return e
		}
		if e = s.validateApprovalActor(reviewer, p, true); e != nil {
			return e
		}
		writes := 0
		for _, step := range p.Steps {
			e, _ := capability.Lookup(step.Capability)
			if step.Status == "pending" || step.Status == "applied" {
				return errcode.BadRequestf("依赖未完成步骤或已执行步骤的计划请继续本人逐步确认")
			}
			if e != nil && e.Mutating {
				writes++
			}
		}
		if writes == 0 {
			return errcode.BadRequestf("只读计划无需审批")
		}
		fresh, e := s.refreshed(txctx, p, proposer)
		if e != nil {
			return e
		}
		if approvalFingerprint(p) != approvalFingerprint(fresh) {
			return errcode.New(errcode.StaleData, "数据已变化，请重新预演后发起审批")
		}
		if p.ApprovalID != "" {
			old, e := s.approvalRow(txctx, p.ApprovalID, true)
			if e != nil {
				return e
			}
			if old.Status == "pending" || old.Status == "approved" {
				if time.Now().Before(old.ExpireTime) {
					return errcode.Conflictf("该计划已有有效审批，请先撤销")
				}
				if e = s.setApprovalStatus(txctx, old, "expired", ""); e != nil {
					return e
				}
			}
		}
		now := time.Now()
		expires := now.Add(time.Duration(in.TTLMinutes) * time.Minute)
		if p.ExpiresAt.Before(expires) {
			expires = p.ExpiresAt
		}
		r := storedApproval{ID: uuid.NewString(), PlanID: p.ID, ProposerID: a.UserID, ProposerName: proposer.UserName, ReviewerID: reviewer.UserID, ReviewerName: reviewer.UserName, Status: "pending", Fingerprint: approvalFingerprint(p), Snapshot: encode(p), RequestComment: strings.TrimSpace(in.Comment), CreateTime: now, ExpireTime: expires, UpdateTime: now}
		if e = dbx.DBFrom(txctx, s.db).Create(&r).Error; e != nil {
			return e
		}
		p.ApprovalID = r.ID
		if e = s.save(txctx, p); e != nil {
			return e
		}
		if e = s.notifyApproval(txctx, &r, r.ReviewerID); e != nil {
			return e
		}
		s.auditApproval(txctx, a, &r, "request")
		result, e = r.public(true)
		return e
	})
	return result, err
}

func (s *Service) ListApprovals(ctx context.Context, in ApprovalQuery) ([]Approval, int64, error) {
	a, err := human(ctx)
	if err != nil {
		return nil, 0, err
	}
	live, err := s.liveActor(ctx, a.UserID)
	if err != nil {
		return nil, 0, err
	}
	q := dbx.DBFrom(ctx, s.db).Model(&storedApproval{})
	switch in.Box {
	case "", "inbox":
		if !live.Can(ApprovalPermission) {
			return nil, 0, errcode.Forbiddenf("没有计划审批权限")
		}
		q = q.Where("reviewer_id=?", a.UserID)
	case "sent":
		q = q.Where("proposer_id=?", a.UserID)
	case "all":
		q = q.Where("proposer_id=? OR reviewer_id=?", a.UserID, a.UserID)
	default:
		return nil, 0, errcode.BadRequestf("审批列表类型无效")
	}
	if in.Status != "" {
		switch in.Status {
		case "expired":
			q = q.Where("status='expired' OR (status IN ('pending','approved') AND expire_time<=?)", time.Now())
		case "pending", "approved":
			q = q.Where("status=? AND expire_time>?", in.Status, time.Now())
		case "rejected", "revoked", "stale", "applied":
			q = q.Where("status=?", in.Status)
		default:
			return nil, 0, errcode.BadRequestf("审批状态无效")
		}
	}
	var total int64
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []storedApproval
	if err = q.Select("approval_id,plan_id,proposer_id,proposer_name,reviewer_id,reviewer_name,status,fingerprint,request_comment,decision_comment,create_time,expire_time,decision_time,update_time,changeset_id").Order("create_time DESC,approval_id DESC").Scopes(in.Query.Scope()).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]Approval, 0, len(rows))
	for _, r := range rows {
		v, _ := r.public(false)
		out = append(out, *v)
	}
	return out, total, nil
}
func (s *Service) GetApproval(ctx context.Context, id string) (*Approval, error) {
	a, err := human(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.approvalRow(ctx, id, false)
	if err != nil {
		return nil, err
	}
	if a.UserID != r.ProposerID && a.UserID != r.ReviewerID {
		return nil, errcode.NotFoundf("审批记录不存在")
	}
	live, err := s.liveActor(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	v, err := r.public(true)
	if err != nil {
		return nil, err
	}
	if err = s.validateApprovalActor(live, v.Plan, a.UserID == r.ReviewerID); err != nil {
		return nil, err
	}
	return v, nil
}

// lockedApproval consistently takes the plan lock before the approval lock;
// preview, request, decision and execution therefore serialize per plan.
func (s *Service) lockedApproval(ctx context.Context, id string, a *capability.Actor) (*storedApproval, *Plan, error) {
	initial, err := s.approvalRow(ctx, id, false)
	if err != nil {
		return nil, nil, err
	}
	if a.UserID != initial.ProposerID && a.UserID != initial.ReviewerID {
		return nil, nil, errcode.NotFoundf("审批记录不存在")
	}
	var pr row
	if err = dbx.DBFrom(ctx, s.db).Clauses(clause.Locking{Strength: "UPDATE"}).Where("plan_id=?", initial.PlanID).First(&pr).Error; err != nil {
		return nil, nil, err
	}
	p, err := fromRow(pr)
	if err != nil {
		return nil, nil, err
	}
	if p.UserID != initial.ProposerID {
		return nil, nil, errcode.Internalf("审批记录与计划主体不匹配")
	}
	r, err := s.approvalRow(ctx, id, true)
	return r, p, err
}
func (s *Service) DecideApproval(ctx context.Context, id, decision string, in ApprovalDecisionIn) (*Approval, error) {
	a, err := human(ctx)
	if err != nil {
		return nil, err
	}
	if decision != "approve" && decision != "reject" && decision != "revoke" {
		return nil, errcode.BadRequestf("审批操作无效")
	}
	if len([]rune(in.Comment)) > 1000 {
		return nil, errcode.BadRequestf("审批意见最多1000字")
	}
	var result *Approval
	var outcome error
	err = dbx.NewTxRunner(s.db)(ctx, func(txctx context.Context) error {
		r, p, e := s.lockedApproval(txctx, id, a)
		if e != nil {
			return e
		}
		terminalRelease := decision == "revoke" && a.UserID == r.ProposerID && (r.Status == "rejected" || r.Status == "stale" || r.Status == "expired")
		if r.Status != "pending" && r.Status != "approved" && !terminalRelease {
			return errcode.Conflictf("该审批已结束")
		}
		if p.ApprovalID != r.ID {
			return errcode.Conflictf("审批已被更新")
		}
		if e = s.lockBusiness(txctx); e != nil {
			return e
		}
		live, e := s.liveActor(txctx, a.UserID)
		if e != nil {
			return e
		}
		if decision == "revoke" {
			if a.UserID != r.ProposerID && !(a.UserID == r.ReviewerID && r.Status == "approved" && live.Can(ApprovalPermission)) {
				return errcode.Forbiddenf("只有发起人或已批准的审批人可以撤销")
			}
			// Preserve the original rejection/expiry record when the proposer
			// explicitly switches back to self confirmation.
			if !terminalRelease {
				if e = s.setApprovalStatus(txctx, r, "revoked", in.Comment); e != nil {
					return e
				}
			}
			p.ApprovalID = ""
			p.Status = "draft"
			if e = s.save(txctx, p); e != nil {
				return e
			}
		} else {
			if a.UserID != r.ReviewerID || a.UserID == r.ProposerID || !live.Can(ApprovalPermission) {
				return errcode.Forbiddenf("只有指定的另一位审批人可以处理")
			}
			if r.Status != "pending" {
				return errcode.Conflictf("该审批已处理")
			}
			if time.Now().After(r.ExpireTime) {
				if e = s.setApprovalStatus(txctx, r, "expired", ""); e != nil {
					return e
				}
				outcome = errcode.New(errcode.StaleData, "审批已过期，请发起人重新预演并申请审批")
			} else if decision == "reject" {
				if strings.TrimSpace(in.Comment) == "" {
					return errcode.BadRequestf("请填写驳回原因")
				}
				if e = s.setApprovalStatus(txctx, r, "rejected", in.Comment); e != nil {
					return e
				}
			} else {
				proposer, e := s.liveActor(txctx, r.ProposerID)
				if e != nil {
					return e
				}
				if e = s.validateApprovalActor(live, p, true); e != nil {
					return e
				}
				fresh, e := s.refreshed(txctx, p, proposer)
				if e != nil {
					return e
				}
				if approvalFingerprint(p) != r.Fingerprint || approvalFingerprint(fresh) != r.Fingerprint {
					if e = s.setApprovalStatus(txctx, r, "stale", ""); e != nil {
						return e
					}
					outcome = errcode.New(errcode.StaleData, "审批差异已变化，请发起人重新预演并申请审批")
				} else if e = s.setApprovalStatus(txctx, r, "approved", in.Comment); e != nil {
					return e
				}
			}
		}
		s.auditApproval(txctx, a, r, decision)
		target := r.ProposerID
		if target == a.UserID {
			target = r.ReviewerID
		}
		if e = s.notifyApproval(txctx, r, target); e != nil {
			return e
		}
		result, e = r.public(false)
		return e
	})
	if err == nil {
		err = outcome
	}
	return result, err
}
func (s *Service) ApplyApproval(ctx context.Context, id string) (*Plan, error) {
	a, err := human(ctx)
	if err != nil {
		return nil, err
	}
	var result *Plan
	var outcome error
	err = dbx.NewTxRunner(s.db)(ctx, func(txctx context.Context) error {
		r, p, e := s.lockedApproval(txctx, id, a)
		if e != nil {
			return e
		}
		if a.UserID != r.ProposerID {
			return errcode.Forbiddenf("批准后应由发起人执行计划")
		}
		if r.Status != "approved" || p.ApprovalID != r.ID {
			return errcode.Conflictf("计划尚未获得有效审批")
		}
		if e = s.lockBusiness(txctx); e != nil {
			return e
		}
		if time.Now().After(r.ExpireTime) || time.Now().After(p.ExpiresAt) {
			if e = s.setApprovalStatus(txctx, r, "expired", ""); e != nil {
				return e
			}
			outcome = errcode.New(errcode.StaleData, "审批已过期，请重新预演并申请审批")
			s.auditApproval(txctx, a, r, "expire")
			return s.notifyApproval(txctx, r, r.ReviewerID)
		}
		proposer, e := s.liveActor(txctx, r.ProposerID)
		if e != nil {
			return e
		}
		reviewer, e := s.liveActor(txctx, r.ReviewerID)
		if e != nil {
			return e
		}
		if e = s.validateApprovalActor(reviewer, p, true); e != nil {
			return e
		}
		fresh, e := s.refreshed(txctx, p, proposer)
		if e != nil {
			return e
		}
		if approvalFingerprint(p) != r.Fingerprint || approvalFingerprint(fresh) != r.Fingerprint {
			if e = s.setApprovalStatus(txctx, r, "stale", ""); e != nil {
				return e
			}
			outcome = errcode.New(errcode.StaleData, "数据已变化，原审批已失效，请重新预演并申请审批")
			s.auditApproval(txctx, a, r, "invalidate")
			return s.notifyApproval(txctx, r, r.ReviewerID)
		}
		proposer.IP, proposer.UserAgent = a.IP, a.UserAgent
		proposer.ApprovalMode = "cross_human"
		proposer.ApprovedBy = strconv.FormatInt(reviewer.UserID, 10)
		approvedCtx := context.WithValue(capability.WithActor(txctx, proposer), approvalExecutionKey{}, r.ID)
		result, e = s.Apply(approvedCtx, p.ID, ApplyIn{Mode: "all"})
		if e != nil {
			return e
		}
		r.ChangesetID = result.ChangesetID
		if e = s.setApprovalStatus(txctx, r, "applied", ""); e != nil {
			return e
		}
		s.auditApproval(txctx, a, r, "apply")
		if e = s.notifyApproval(txctx, r, r.ReviewerID); e != nil {
			return e
		}
		return s.attachApproval(txctx, result)
	})
	if err == nil {
		err = outcome
	}
	return result, err
}

func (s *Service) Reviewers(ctx context.Context, planID string) ([]Reviewer, error) {
	a, err := human(ctx)
	if err != nil {
		return nil, err
	}
	p, err := s.load(ctx, planID, false)
	if err != nil {
		return nil, err
	}
	proposer, err := s.liveActor(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	if err = s.validateApprovalActor(proposer, p, false); err != nil {
		return nil, err
	}
	var ids []int64
	// Bound the picker and prefilter the approval grant; returned accounts must
	// additionally pass the exact plan's live permissions and risk checks.
	q := `SELECT DISTINCT u.user_id FROM sys_user u WHERE u.status='0' AND u.del_flag='0' AND u.user_id<>? AND (
	 EXISTS(SELECT 1 FROM sys_user_perm up WHERE up.user_id=u.user_id AND up.perm IN (?, '*:*:*')) OR
	 EXISTS(SELECT 1 FROM sys_user_role ur JOIN sys_role r ON r.role_id=ur.role_id WHERE ur.user_id=u.user_id AND r.status='0' AND r.del_flag='0' AND (r.role_key='admin' OR EXISTS(SELECT 1 FROM sys_role_menu rm JOIN sys_menu m ON m.menu_id=rm.menu_id WHERE rm.role_id=r.role_id AND m.status='0' AND m.del_flag='0' AND m.perms IN (?, '*:*:*'))))) ORDER BY u.user_id LIMIT 200`
	if err = dbx.DBFrom(ctx, s.db).Raw(q, a.UserID, ApprovalPermission, ApprovalPermission).Scan(&ids).Error; err != nil {
		return nil, err
	}
	out := []Reviewer{}
	for _, id := range ids {
		reviewer, e := s.liveActor(ctx, id)
		if e == nil && s.validateApprovalActor(reviewer, p, true) == nil {
			out = append(out, Reviewer{UserID: id, UserName: reviewer.UserName})
		}
	}
	return out, nil
}

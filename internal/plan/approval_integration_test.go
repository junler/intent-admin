package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/junler/intent-admin/internal/auth"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/capability/dryrun"
	"github.com/junler/intent-admin/internal/capability/undo"
	"github.com/junler/intent-admin/internal/changeset"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/service"
)

func TestCrossHumanApprovalLiveGrantsAndImmutablePreview(t *testing.T) {
	db := integrationDB(t)
	sets := changeset.New(db)
	capability.Install(capability.Hooks{Tx: dbx.NewTxRunner(db), ReadOnly: dbx.NewReadOnlyRunner(), PersistChanges: sets.Persist, WriteAudit: sets.WriteAudit, MaxAIRisk: capability.RiskHigh})
	const capName = "approvalFixture.item.update"
	const perm = "fixture:item:edit"
	demo := service.NewDemoService(db)
	capability.Register(capability.Capability[service.DemoUpdateIn, capability.Empty]{
		Meta: capability.Meta{Name: capName, Summary: "审批测试修改", Perm: perm, Risk: capability.RiskMedium, Mutating: true, Reversible: true, AIEnabled: true},
		Fn:   demo.Update,
		DryRun: func(ctx context.Context, in service.DemoUpdateIn) (*capability.Preview, error) {
			var current model.DemoItem
			if err := dbx.DBFrom(ctx, db).Where("item_id=?", in.ItemID).First(&current).Error; err != nil {
				return nil, err
			}
			return dryrun.ForUpdate(ctx, db, &model.DemoItem{}, []any{in.ItemID}, service.DemoPatchFor(in, current), dryrun.LabelOpt{Entity: "demo_item", IDField: "item_id"})
		},
		Undo: undo.RestoreFieldsFn(db, undo.Table{Name: "demo_item", IDField: "item_id"}),
	})
	for _, id := range []int64{11, 12, 13, 14, 15} {
		if err := db.Exec("INSERT INTO sys_user(user_id,user_name,nick_name,password,ai_max_risk) VALUES(?,?,'审批测试','disabled-test-fixture',?)", id, fmt.Sprintf("approval%d", id), "high").Error; err != nil {
			t.Fatal(err)
		}
		perms := []string{"ai:agent:use", perm}
		if id != 11 {
			perms = append(perms, ApprovalPermission)
		}
		if id == 15 {
			perms = []string{"ai:agent:use", ApprovalPermission}
		}
		for _, p := range perms {
			if err := db.Exec("INSERT INTO sys_user_perm(user_id,perm) VALUES(?,?)", id, p).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.Exec("UPDATE sys_user SET ai_max_risk='low' WHERE user_id=14").Error; err != nil {
		t.Fatal(err)
	}
	plans := New(db, Options{TTL: time.Minute, Changesets: sets})
	identity := auth.New(db, nil, &config.Config{AI: config.AI{MaxRisk: "high"}})
	plans.ActorLoader = identity.CurrentAIActor
	var notifications []ApprovalNotification
	plans.Notify = func(_ context.Context, n ApprovalNotification) error {
		notifications = append(notifications, n)
		return nil
	}
	humanCtx := func(id int64) context.Context {
		return capability.WithActor(context.Background(), &capability.Actor{UserID: id, UserName: fmt.Sprintf("approval%d", id), Type: capability.ActorHuman, Source: capability.SourceHTTP, Perms: []string{capability.SuperPerm}, MaxRisk: capability.RiskCritical})
	}
	owner, reviewer, stranger := humanCtx(11), humanCtx(12), humanCtx(13)
	checkCode := func(err error, want int) {
		t.Helper()
		if err == nil || errcode.From(err).Code != want {
			t.Fatalf("wanted%d got%v", want, err)
		}
	}
	mustPlan := func() *Plan {
		t.Helper()
		if err := db.Exec("UPDATE demo_item SET status='0' WHERE item_id IN(1,2)").Error; err != nil {
			t.Fatal(err)
		}
		p, err := plans.Create(owner, CreateIn{Intent: "两人核对后停用示例", Steps: []Step{{Capability: capName, Args: json.RawMessage(`{"itemId":1,"status":"1"}`)}, {Capability: capName, Args: json.RawMessage(`{"itemId":2,"status":"1"}`)}}})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	request := func(p *Plan) *Approval {
		t.Helper()
		a, err := plans.RequestApproval(owner, p.ID, RequestApprovalIn{ReviewerID: 12, Comment: "请核对两个状态变化"})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	approve := func(a *Approval) {
		t.Helper()
		out, err := plans.DecideApproval(reviewer, a.ID, "approve", ApprovalDecisionIn{Comment: "差异已核对"})
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != "approved" {
			t.Fatal(out)
		}
	}
	assertNoChanges := func() {
		t.Helper()
		var n int64
		db.Table("demo_item").Where("item_id IN(1,2) AND status='1'").Count(&n)
		if n != 0 {
			t.Fatalf("unapproved mutation: %d", n)
		}
	}

	p := mustPlan()
	previousNotify := plans.Notify
	plans.Notify = func(context.Context, ApprovalNotification) error {
		return errors.New("notification transaction failed")
	}
	if _, failure := plans.RequestApproval(owner, p.ID, RequestApprovalIn{ReviewerID: 12}); failure == nil {
		t.Fatal("notification failure committed request")
	}
	var failedRequests int64
	db.Table("sys_ai_plan_approval").Where("plan_id=?", p.ID).Count(&failedRequests)
	if failedRequests != 0 {
		t.Fatal("failed notification left an approval behind")
	}
	untouched, failure := plans.Get(owner, p.ID)
	if failure != nil || untouched.ApprovalID != "" {
		t.Fatalf("failed request changed plan: %v", failure)
	}
	plans.Notify = previousNotify
	_, err := plans.RequestApproval(owner, p.ID, RequestApprovalIn{ReviewerID: 11})
	checkCode(err, 400)
	_, err = plans.RequestApproval(owner, p.ID, RequestApprovalIn{ReviewerID: 14})
	checkCode(err, 423)
	_, err = plans.RequestApproval(owner, p.ID, RequestApprovalIn{ReviewerID: 15})
	checkCode(err, 403)
	a := request(p)
	if a.Plan == nil || a.Plan.Intent != p.Intent || a.Plan.Steps[0].Preview == nil {
		t.Fatal("missing immutable preview")
	}
	_, err = plans.DecideApproval(owner, a.ID, "approve", ApprovalDecisionIn{})
	checkCode(err, 403)
	_, err = plans.DecideApproval(stranger, a.ID, "approve", ApprovalDecisionIn{})
	checkCode(err, 404)
	_, err = plans.GetApproval(stranger, a.ID)
	checkCode(err, 404)
	_, err = plans.Apply(owner, p.ID, ApplyIn{})
	checkCode(err, 403)
	_, err = plans.ApplyApproval(owner, a.ID)
	checkCode(err, 409)
	agent := *testActor(capability.SourceAI, capability.RiskHigh)
	agent.UserID = 12
	_, err = plans.DecideApproval(capability.WithActor(context.Background(), &agent), a.ID, "approve", ApprovalDecisionIn{})
	checkCode(err, 403)
	approve(a)
	_, err = plans.ApplyApproval(reviewer, a.ID)
	checkCode(err, 403)
	p, err = plans.ApplyApproval(owner, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "done" || p.Approval == nil || p.Approval.Status != "applied" {
		t.Fatalf("bad approval result %+v", p)
	}
	var group changeset.Set
	if err = db.Where("changeset_id=?", p.ChangesetID).First(&group).Error; err != nil {
		t.Fatal(err)
	}
	if group.ApprovedBy != "12" || group.ApprovalMode != "cross_human" || group.ActorID != 11 {
		t.Fatalf("approval attribution lost: %+v", group)
	}
	var count int64
	db.Table("sys_changeset_item").Where("changeset_id=?", p.ChangesetID).Count(&count)
	if count != 2 {
		t.Fatal("approval did not group writes")
	}
	db.Table("sys_oper_log").Where("plan_id=? AND capability=? AND approved_by='12' AND approval_mode='cross_human'", p.ID, capName).Count(&count)
	if count != 2 {
		t.Fatalf("missing approver in audit: %d", count)
	}
	_, err = plans.ApplyApproval(owner, a.ID)
	checkCode(err, 409)
	if len(notifications) < 3 || notifications[0].UserID != 12 || notifications[1].UserID != 11 {
		t.Fatal("missing personal notifications")
	}

	// A current permission/risk/account change invalidates execution even when
	// the browser context still carries a wildcard from its earlier session.
	for _, change := range []struct {
		sql, restore string
		code         int
	}{
		{"DELETE FROM sys_user_perm WHERE user_id=12 AND perm='fixture:item:edit'", "INSERT INTO sys_user_perm(user_id,perm) VALUES(12,'fixture:item:edit')", 403},
		{"UPDATE sys_user SET ai_max_risk='low' WHERE user_id=12", "UPDATE sys_user SET ai_max_risk='high' WHERE user_id=12", 423},
		{"UPDATE sys_user SET status='1' WHERE user_id=11", "UPDATE sys_user SET status='0' WHERE user_id=11", 403},
	} {
		p = mustPlan()
		a = request(p)
		approve(a)
		if err = db.Exec(change.sql).Error; err != nil {
			t.Fatal(err)
		}
		_, err = plans.ApplyApproval(owner, a.ID)
		checkCode(err, change.code)
		assertNoChanges()
		if err = db.Exec(change.restore).Error; err != nil {
			t.Fatal(err)
		}
	}

	// Changes before either decision or application expire the immutable grant.
	for _, beforeDecision := range []bool{true, false} {
		p = mustPlan()
		a = request(p)
		if !beforeDecision {
			approve(a)
		}
		if err = db.Exec("UPDATE demo_item SET status='2' WHERE item_id=1").Error; err != nil {
			t.Fatal(err)
		}
		if beforeDecision {
			_, err = plans.DecideApproval(reviewer, a.ID, "approve", ApprovalDecisionIn{})
		} else {
			_, err = plans.ApplyApproval(owner, a.ID)
		}
		checkCode(err, 412)
		var status string
		db.Table("sys_ai_plan_approval").Where("approval_id=?", a.ID).Select("status").Scan(&status)
		if status != "stale" {
			t.Fatalf("stale decision was rolled back: %s", status)
		}
		var status2 string
		db.Table("demo_item").Where("item_id=2").Select("status").Scan(&status2)
		if status2 != "0" {
			t.Fatal("stale approval wrote row2")
		}
	}
	p = mustPlan()
	a = request(p)
	_, err = plans.DecideApproval(reviewer, a.ID, "reject", ApprovalDecisionIn{})
	checkCode(err, 400)
	rejected, err := plans.DecideApproval(reviewer, a.ID, "reject", ApprovalDecisionIn{Comment: "请缩小范围"})
	if err != nil || rejected.Status != "rejected" {
		t.Fatal(err)
	}
	_, err = plans.Apply(owner, p.ID, ApplyIn{})
	checkCode(err, 403)

	p = mustPlan()
	a = request(p)
	approve(a)
	revoked, err := plans.DecideApproval(reviewer, a.ID, "revoke", ApprovalDecisionIn{Comment: "撤回批准"})
	if err != nil || revoked.Status != "revoked" {
		t.Fatal(err)
	}
	_, err = plans.Apply(owner, p.ID, ApplyIn{})
	checkCode(err, 409)
	if _, err = plans.Preview(owner, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = plans.Apply(owner, p.ID, ApplyIn{}); err != nil {
		t.Fatalf("explicit self-confirm after revoke failed: %v", err)
	}

	p = mustPlan()
	a = request(p)
	if err = db.Exec("UPDATE sys_ai_plan_approval SET expire_time=now()-interval '1 second' WHERE approval_id=?", a.ID).Error; err != nil {
		t.Fatal(err)
	}
	_, err = plans.DecideApproval(reviewer, a.ID, "approve", ApprovalDecisionIn{})
	checkCode(err, 412)
	got, err := plans.GetApproval(owner, a.ID)
	if err != nil || got.Status != "expired" {
		t.Fatalf("expiry: %+v %v", got, err)
	}

	p = mustPlan()
	entry, _ := capability.Lookup(capName)
	entry.Risk = capability.RiskCritical
	_, err = plans.RequestApproval(owner, p.ID, RequestApprovalIn{ReviewerID: 12})
	checkCode(err, 423)
	entry.Risk = capability.RiskMedium
	entry.AIEnabled = false
	_, err = plans.RequestApproval(owner, p.ID, RequestApprovalIn{ReviewerID: 12})
	checkCode(err, 404)
	entry.AIEnabled = true

	p = mustPlan()
	a = request(p)
	approve(a)
	if _, err = plans.Preview(owner, p.ID); err != nil {
		t.Fatal(err)
	}
	_, err = plans.ApplyApproval(owner, a.ID)
	checkCode(err, 409)
	_, err = plans.Apply(owner, p.ID, ApplyIn{})
	checkCode(err, 403)
	// Re-request after a fresh preview creates a new record, leaving history.
	next, err := plans.RequestApproval(owner, p.ID, RequestApprovalIn{ReviewerID: 12})
	if err != nil || next.ID == a.ID {
		t.Fatalf("reapproval: %v", err)
	}
	rows, total, err := plans.ListApprovals(reviewer, ApprovalQuery{Box: "inbox", Status: "pending"})
	if err != nil || total < 1 || len(rows) < 1 {
		t.Fatalf("inbox: %v", err)
	}
	for _, row := range rows {
		if row.ReviewerID != 12 || row.Plan != nil {
			t.Fatal("inbox leaked another account or preview")
		}
	}

	// Self confirmation shares the live boundary. A stale browser wildcard
	// cannot restore revoked grants or a previously higher AI risk ceiling.
	p = mustPlan()
	a = request(p)
	decisions := make(chan error, 2)
	for range 2 {
		go func() {
			_, failure := plans.DecideApproval(reviewer, a.ID, "approve", ApprovalDecisionIn{})
			decisions <- failure
		}()
	}
	successes := 0
	for range 2 {
		failure := <-decisions
		if failure == nil {
			successes++
		} else {
			checkCode(failure, 409)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent decisions accepted %d approvals", successes)
	}
	applications := make(chan error, 2)
	for range 2 {
		go func() { _, failure := plans.ApplyApproval(owner, a.ID); applications <- failure }()
	}
	successes = 0
	for range 2 {
		failure := <-applications
		if failure == nil {
			successes++
		} else {
			checkCode(failure, 409)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent application accepted %d executions", successes)
	}
	p = mustPlan()
	if err = db.Exec("UPDATE sys_user SET ai_max_risk='low' WHERE user_id=11").Error; err != nil {
		t.Fatal(err)
	}
	_, err = plans.Apply(owner, p.ID, ApplyIn{})
	checkCode(err, 423)
	assertNoChanges()
	if err = db.Exec("UPDATE sys_user SET ai_max_risk='high' WHERE user_id=11").Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("DELETE FROM sys_user_perm WHERE user_id=11 AND perm=?", perm).Error; err != nil {
		t.Fatal(err)
	}
	_, err = plans.Apply(owner, p.ID, ApplyIn{})
	checkCode(err, 403)
	assertNoChanges()
}

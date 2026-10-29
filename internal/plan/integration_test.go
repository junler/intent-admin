package plan

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/capability/undo"
	"github.com/junler/intent-admin/internal/changeset"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/feature/demo"
	"github.com/junler/intent-admin/internal/knowledge"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/migrations"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("INTENT_ADMIN_TEST_DSN")
	if dsn == "" {
		t.Skip("INTENT_ADMIN_TEST_DSN is required for real PostgreSQL integration tests")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "plan_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE"); admin.Close() })
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("schema_migrations")
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.Up(sqlDB, migrations.Dir); err != nil {
		t.Fatal(err)
	}
	return db
}
func testActor(source capability.Source, risk capability.Risk) *capability.Actor {
	return &capability.Actor{UserID: 1, UserName: "admin", Type: capability.ActorAgent, Source: source, Perms: []string{capability.SuperPerm}, MaxRisk: risk, ApprovalMode: "manual"}
}
func TestPlanPersistenceRecheckGroupingRollback(t *testing.T) {
	db := integrationDB(t)
	sqlDB, _ := db.DB()
	var beforeMenu, afterMenu int64
	if err := db.Table("sys_menu").Count(&beforeMenu).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrations.Seed(context.Background(), sqlDB); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Seed(context.Background(), sqlDB); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("sys_menu").Count(&afterMenu).Error; err != nil {
		t.Fatal(err)
	}
	if beforeMenu != afterMenu {
		t.Fatalf("seed rerun duplicated menus: before%d after%d", beforeMenu, afterMenu)
	}
	sets := changeset.New(db)
	sets.Register()
	demo.Register(db)
	capability.Install(capability.Hooks{Tx: dbx.NewTxRunner(db), ReadOnly: dbx.NewReadOnlyRunner(), PersistChanges: sets.Persist, WriteAudit: sets.WriteAudit, MaxAIRisk: capability.RiskHigh})
	plans := New(db, Options{TTL: time.Minute, MaxSteps: 5, MaxAffected: 200, Changesets: sets})
	ctx := capability.WithActor(context.Background(), testActor(capability.SourceAI, capability.RiskHigh))
	var secretConfigID int64
	if err := db.Table("sys_config").Where("config_key=?", "sys.user.initPassword").Select("config_id").Scan(&secretConfigID).Error; err != nil {
		t.Fatal(err)
	}
	if err := plans.rejectCredentialConfig(ctx, "system.config.update", encode(map[string]any{"configId": secretConfigID, "configValue": "must-not-persist"})); err == nil {
		t.Fatal("credential configuration accepted into plan")
	}
	p, err := plans.Create(ctx, CreateIn{Intent: "停用两个示例", Steps: []Step{{Capability: "demo.item.update", Args: json.RawMessage(`{"itemId":1,"status":"1"}`)}, {Capability: "demo.item.update", Args: json.RawMessage(`{"itemId":2,"status":"1"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Affected != 2 || p.Status != "previewed" {
		t.Fatalf("bad plan %+v", p)
	}
	if err = db.Exec("UPDATE demo_item SET status='2' WHERE item_id=1").Error; err != nil {
		t.Fatal(err)
	}
	if _, err = plans.Apply(ctx, p.ID, ApplyIn{}); err == nil || errcode.From(err).Code != errcode.StaleData {
		t.Fatalf("wanted412 got%v", err)
	}
	var count int64
	db.Table("sys_changeset").Count(&count)
	if count != 0 {
		t.Fatal("stale plan created changeset")
	}
	if err = db.Exec("UPDATE demo_item SET status='0' WHERE item_id=1").Error; err != nil {
		t.Fatal(err)
	}
	p, err = plans.Preview(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err = plans.Apply(ctx, p.ID, ApplyIn{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "done" || p.ChangesetID == "" {
		t.Fatalf("bad result %+v", p)
	}
	db.Table("sys_changeset").Count(&count)
	if count != 1 {
		t.Fatalf("plan produced %d changesets", count)
	}
	db.Table("sys_changeset_item").Where("changeset_id=?", p.ChangesetID).Count(&count)
	if count != 2 {
		t.Fatalf("group contains %d items", count)
	}
	human := func() context.Context {
		return capability.WithActor(context.Background(), testActor(capability.SourceHTTP, capability.RiskCritical))
	}
	out, err := capability.InvokeJSON(human(), "system.changeset.rollback", json.RawMessage(fmt.Sprintf(`{"id":%q}`, p.ChangesetID)))
	if err != nil {
		t.Fatal(err)
	}
	rolled := out.(*changeset.Set)
	if rolled.Status != "rolled_back" || rolled.RollbackID == "" {
		t.Fatalf("bad rollback %+v", rolled)
	}
	db.Table("demo_item").Where("item_id IN (1,2) AND status='0'").Count(&count)
	if count != 2 {
		t.Fatal("rows not restored")
	}
	if _, err = capability.InvokeJSON(human(), "system.changeset.rollback", json.RawMessage(fmt.Sprintf(`{"id":%q}`, rolled.RollbackID))); err != nil {
		t.Fatal(err)
	}
	db.Table("demo_item").Where("item_id IN (1,2) AND status='1'").Count(&count)
	if count != 2 {
		t.Fatal("rollback of rollback did not restore applied state")
	}

	// A conflict in one row does not overwrite it or prevent independent rows
	// from being restored; the unresolved item remains visible and retryable.
	conflictPlan, err := plans.Create(ctx, CreateIn{Intent: "部分回滚", Steps: []Step{{Capability: "demo.item.update", Args: json.RawMessage(`{"itemId":1,"status":"0"}`)}, {Capability: "demo.item.update", Args: json.RawMessage(`{"itemId":2,"status":"0"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	conflictPlan, err = plans.Apply(ctx, conflictPlan.ID, ApplyIn{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("UPDATE demo_item SET status='1' WHERE item_id=1").Error; err != nil {
		t.Fatal(err)
	}
	conflictOut, err := capability.InvokeJSON(human(), "system.changeset.rollback", encode(map[string]any{"id": conflictPlan.ChangesetID}))
	if err != nil {
		t.Fatal(err)
	}
	conflicted := conflictOut.(*changeset.Set)
	if conflicted.Status != "rollback_failed" {
		t.Fatalf("wanted partial rollback got%s", conflicted.Status)
	}
	db.Table("demo_item").Where("item_id IN (1,2) AND status='1'").Count(&count)
	if count != 2 {
		t.Fatal("conflicting row was overwritten or independent rollback failed")
	}
	// Query results can feed a later write without trusting model-guessed IDs.
	refPlan, err := plans.Create(ctx, CreateIn{Intent: "删除alpha分类", Steps: []Step{{Capability: "demo.item.list", Args: json.RawMessage(`{"category":"alpha"}`)}, {Capability: "demo.item.remove", Args: json.RawMessage(`{"itemIds":"$steps[0].result.rows[*].itemId"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if refPlan.Affected != 2 {
		t.Fatalf("reference affected%d", refPlan.Affected)
	}
	refPlan, err = plans.Apply(ctx, refPlan.ID, ApplyIn{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = capability.InvokeJSON(human(), "system.changeset.rollback", encode(map[string]any{"id": refPlan.ChangesetID})); err != nil {
		t.Fatal(err)
	}
	db.Table("demo_item").Where("item_id IN (1,2) AND del_flag='0'").Count(&count)
	if count != 2 {
		t.Fatal("soft-deleted rows were not restored")
	}
	// Each stepwise confirmation commits one write into the same group.
	stepPlan, err := plans.Create(ctx, CreateIn{Intent: "分步修改", Steps: []Step{{Capability: "demo.item.update", Args: json.RawMessage(`{"itemId":3,"status":"0"}`)}, {Capability: "demo.item.update", Args: json.RawMessage(`{"itemId":4,"status":"1"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	zero, one := 0, 1
	stepPlan, err = plans.Apply(ctx, stepPlan.ID, ApplyIn{Mode: "stepwise", StepSeq: &zero})
	if err != nil {
		t.Fatal(err)
	}
	if stepPlan.Status != "previewed" || stepPlan.Steps[1].Status == "applied" {
		t.Fatal("stepwise applied too much")
	}
	stepPlan, err = plans.Apply(ctx, stepPlan.ID, ApplyIn{Mode: "stepwise", StepSeq: &one})
	if err != nil {
		t.Fatal(err)
	}
	if stepPlan.Status != "done" {
		t.Fatal("second step not finished")
	}
	db.Table("sys_changeset_item").Where("changeset_id=?", stepPlan.ChangesetID).Count(&count)
	if count != 2 {
		t.Fatalf("step group has%d items", count)
	}
	// Deferring a reference to a newly created ID requires a fresh, explicit
	// confirmation after that ID becomes known.
	type createIn struct {
		Name string `json:"name"`
	}
	type created struct {
		ItemID int64 `json:"itemId"`
	}
	capability.Register(capability.Capability[createIn, created]{Meta: capability.Meta{Name: "testing.planCreate", Summary: "测试新建", Perm: "demo:item:edit", Risk: capability.RiskMedium, Mutating: true, Reversible: true, AIEnabled: true}, Fn: func(c context.Context, in createIn) (created, error) {
		item := model.DemoItem{Name: in.Name, Category: "test", Amount: 0, Status: "0", DelFlag: "0", CreateTime: time.Now(), UpdateTime: time.Now()}
		if err := dbx.DBFrom(c, db).Create(&item).Error; err != nil {
			return created{}, err
		}
		row := map[string]any{"name": in.Name, "category": "test", "amount": 0, "status": "0", "del_flag": "0"}
		capability.RecorderFrom(c).Create("demo_item", fmt.Sprint(item.ItemID), in.Name, row)
		return created{ItemID: item.ItemID}, nil
	}, DryRun: func(c context.Context, in createIn) (*capability.Preview, error) {
		p := &capability.Preview{Reversible: true}
		p.AddChange(capability.Change{Entity: "demo_item", Op: capability.OpCreate, Fields: []capability.FieldDiff{{Field: "name", After: in.Name}}})
		return p, nil
	}, Undo: undo.DeleteCreatedFn(db, undo.Table{Name: "demo_item", IDField: "item_id"})})
	deferred, err := plans.Create(ctx, CreateIn{Intent: "新建再停用", Steps: []Step{{Capability: "testing.planCreate", Args: json.RawMessage(`{"name":"deferred"}`)}, {Capability: "demo.item.update", Args: json.RawMessage(`{"itemId":"$steps[0].result.itemId","status":"1"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if deferred.Steps[1].Status != "pending" {
		t.Fatal("write reference prematurely resolved")
	}
	if _, err = plans.Apply(ctx, deferred.ID, ApplyIn{}); err == nil {
		t.Fatal("unresolved plan applied without stepwise confirmation")
	}
	deferred, err = plans.Apply(ctx, deferred.ID, ApplyIn{Mode: "stepwise", StepSeq: &zero})
	if err != nil {
		t.Fatal(err)
	}
	if deferred.Steps[1].Status != "previewed" || deferred.Steps[1].Preview.Affected != 1 {
		t.Fatalf("dependent write not previewed with actualID: %s", encode(deferred))
	}
	deferred, err = plans.Apply(ctx, deferred.ID, ApplyIn{Mode: "stepwise", StepSeq: &one})
	if err != nil {
		t.Fatal(err)
	}
	if deferred.Status != "done" {
		t.Fatal("dependent plan not finished")
	}
	// Rolled-back nested work must not invalidate caches before or after commit.
	callbacks := []string{}
	err = dbx.NewTxRunner(db)(ctx, func(outer context.Context) error {
		dbx.AfterCommit(outer, func() { callbacks = append(callbacks, "outer") })
		innerErr := dbx.NewTxRunner(db)(outer, func(inner context.Context) error {
			dbx.AfterCommit(inner, func() { callbacks = append(callbacks, "discarded") })
			return errors.New("rollback nested")
		})
		if innerErr == nil {
			return errors.New("nested failure lost")
		}
		if len(callbacks) != 0 {
			return errors.New("callback ran before commit")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(callbacks) != 1 || callbacks[0] != "outer" {
		t.Fatalf("bad callbacks%v", callbacks)
	}
	// A fallible post-commit action reports its failure without pretending
	// that the database commit was undone, and still drains cache callbacks.
	drained := false
	err = dbx.NewTxRunner(db)(ctx, func(txctx context.Context) error {
		if err := dbx.DBFrom(txctx, db).Table("demo_item").Where("item_id=5").Update("amount", 777).Error; err != nil {
			return err
		}
		_ = dbx.AfterCommitE(txctx, func() error { return errors.New("queue unavailable") })
		dbx.AfterCommit(txctx, func() { drained = true })
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "已提交") || !drained {
		t.Fatalf("post-commit state error%v drained%v", err, drained)
	}
	var committedAmount int64
	if err := db.Table("demo_item").Where("item_id=5").Select("amount").Scan(&committedAmount).Error; err != nil {
		t.Fatal(err)
	}
	if committedAmount != 777 {
		t.Fatal("post-commit failure rolled back committed business row")
	}
	// Reads inside a transaction see its own dictionary writes without
	// publishing those uncommitted values into the shared knowledge cache.
	library := knowledge.New(db)
	beforeKnowledge, err := library.Get(ctx, "sys_normal_disable")
	if err != nil {
		t.Fatal(err)
	}
	originalLabel := beforeKnowledge.Enums[0].Values["0"]
	transactionErr := dbx.NewTxRunner(db)(ctx, func(txctx context.Context) error {
		if err := dbx.DBFrom(txctx, db).Table("sys_dict_data").Where("dict_type='sys_normal_disable' AND dict_value='0'").Update("dict_label", "uncommitted-label").Error; err != nil {
			return err
		}
		inside, err := library.Get(txctx, "sys_normal_disable")
		if err != nil {
			return err
		}
		if inside.Enums[0].Values["0"] != "uncommitted-label" {
			return errors.New("transaction read used stale knowledge cache")
		}
		return errors.New("intentional dictionary rollback")
	})
	if transactionErr == nil || transactionErr.Error() != "intentional dictionary rollback" {
		t.Fatal(transactionErr)
	}
	afterKnowledge, err := library.Get(ctx, "sys_normal_disable")
	if err != nil {
		t.Fatal(err)
	}
	if afterKnowledge.Enums[0].Values["0"] != originalLabel {
		t.Fatal("uncommitted dictionary escaped into knowledge cache")
	}
	// Successful nested calls are not recorded as committed successes until
	// their enclosing transaction commits. Its rollback records a failed
	// attempt without a dangling changeset link.
	auditActor := testActor(capability.SourceHTTP, capability.RiskCritical)
	auditActor.PlanID = uuid.NewString()
	auditCtx := capability.WithActor(context.Background(), auditActor)
	auditErr := dbx.NewTxRunner(db)(auditCtx, func(txctx context.Context) error {
		if _, err := capability.InvokeJSON(txctx, "demo.item.update", json.RawMessage(`{"itemId":5,"status":"1"}`)); err != nil {
			return err
		}
		var premature int64
		if err := db.Table("sys_oper_log").Where("plan_id=?", auditActor.PlanID).Count(&premature).Error; err != nil {
			return err
		}
		if premature != 0 {
			return errors.New("success audit published before outer commit")
		}
		return errors.New("intentional enclosing rollback")
	})
	if auditErr == nil || auditErr.Error() != "intentional enclosing rollback" {
		t.Fatal(auditErr)
	}
	var auditRows []struct {
		Status      string
		ChangesetID string
	}
	if err := db.Table("sys_oper_log").Select("status,changeset_id").Where("plan_id=?", auditActor.PlanID).Scan(&auditRows).Error; err != nil {
		t.Fatal(err)
	}
	if len(auditRows) != 1 || auditRows[0].Status != "1" || auditRows[0].ChangesetID != "" {
		t.Fatalf("outer rollback audit inconsistent: %+v", auditRows)
	}
	// Integer cents remain exact through JSONB compensation, including values
	// beyond float64's exact integer range and inverse rollback payloads.
	exactAmount := int64(9007199254740993)
	if _, err := capability.InvokeJSON(human(), "demo.item.update", encode(map[string]any{"itemId": 5, "amount": exactAmount})); err != nil {
		t.Fatal(err)
	}
	amountResult := &capability.CallResult{}
	if _, err := capability.InvokeJSON(capability.WithCallResult(human(), amountResult), "demo.item.update", encode(map[string]any{"itemId": 5, "amount": 0})); err != nil {
		t.Fatal(err)
	}
	amountUndo, err := capability.InvokeJSON(human(), "system.changeset.rollback", encode(map[string]any{"id": amountResult.ChangesetID}))
	if err != nil {
		t.Fatal(err)
	}
	var restoredAmount int64
	if err := db.Table("demo_item").Where("item_id=5").Select("amount").Scan(&restoredAmount).Error; err != nil {
		t.Fatal(err)
	}
	if restoredAmount != exactAmount {
		t.Fatalf("integer cents corrupted by undo: got%d want%d", restoredAmount, exactAmount)
	}
	if _, err := capability.InvokeJSON(human(), "system.changeset.rollback", encode(map[string]any{"id": amountUndo.(*changeset.Set).RollbackID})); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("demo_item").Where("item_id=5").Select("amount").Scan(&restoredAmount).Error; err != nil {
		t.Fatal(err)
	}
	if restoredAmount != 0 {
		t.Fatalf("inverse compensation corrupted integer cents: got%d", restoredAmount)
	}
	// The full natural-language intent can exceed the display summary column.
	longIntent := strings.Repeat("详细说明", 150)
	longPlan, err := plans.Create(ctx, CreateIn{Intent: longIntent, Steps: []Step{{Capability: "demo.item.update", Args: json.RawMessage(`{"itemId":4,"status":"0"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	longPlan, err = plans.Apply(ctx, longPlan.ID, ApplyIn{})
	if err != nil {
		t.Fatal(err)
	}
	var intentMetadata struct {
		Summary string
		Intent  string
	}
	if err := db.Table("sys_changeset").Select("summary,intent").Where("changeset_id=?", longPlan.ChangesetID).Scan(&intentMetadata).Error; err != nil {
		t.Fatal(err)
	}
	if len([]rune(intentMetadata.Summary)) != 500 || intentMetadata.Intent != longIntent {
		t.Fatal("long plan intent was lost or summary exceeded its column")
	}
	// Ownership is checked independently of being an administrator.
	other := testActor(capability.SourceAI, capability.RiskHigh)
	other.UserID = 999
	if _, err = plans.Get(capability.WithActor(context.Background(), other), p.ID); err == nil || errcode.From(err).Code != errcode.NotFound {
		t.Fatalf("cross-user read %v", err)
	}
	// A low-risk actor can inspect a proposal, but cannot execute its writes.
	low := capability.WithActor(context.Background(), testActor(capability.SourceAI, capability.RiskLow))
	limited := New(db, Options{MaxRisk: capability.RiskLow, Changesets: sets})
	lp, err := limited.Create(low, CreateIn{Intent: "风险限制", Steps: []Step{{Capability: "demo.item.update", Args: json.RawMessage(`{"itemId":1,"status":"0"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = limited.Apply(low, lp.ID, ApplyIn{}); err == nil || errcode.From(err).Code != errcode.RiskExceeded {
		t.Fatalf("wanted423 got%v", err)
	}
}

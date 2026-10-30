package changeset

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/feature/resources"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/secret"
	"github.com/junler/intent-admin/migrations"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func rollbackTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("INTENT_ADMIN_TEST_DSN")
	if dsn == "" {
		t.Skip("requires INTENT_ADMIN_TEST_DSN for isolated PostgreSQL schema")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "rollback_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE"); admin.Close() })
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	t.Cleanup(func() { conn.Close() })
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("schema_migrations")
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.Up(conn, migrations.Dir); err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("CREATE TABLE rollback_fixture (id bigint PRIMARY KEY, value int NOT NULL, note text NOT NULL DEFAULT '')").Error; err != nil {
		t.Fatal(err)
	}
	return db
}
func TestSelectiveRollbackIntegration(t *testing.T) {
	db := rollbackTestDB(t)
	svc := New(db)
	svc.Register()
	capName := "testing.selective.update"
	capability.Register(capability.Capability[capability.Empty, capability.Empty]{Meta: capability.Meta{Name: capName, Summary: "Selective rollback fixture", Perm: "test:update", Risk: capability.RiskMedium, Mutating: true, Reversible: true, AIEnabled: true, MCPEnabled: true}, Fn: func(context.Context, capability.Empty) (capability.Empty, error) { return capability.Empty{}, nil }, DryRun: func(context.Context, capability.Empty) (*capability.Preview, error) {
		return &capability.Preview{}, nil
	}, Undo: svc.Undo})
	capability.Install(capability.Hooks{Tx: dbx.NewTxRunner(db), ReadOnly: dbx.NewReadOnlyRunner(), PersistChanges: svc.Persist, WriteAudit: svc.WriteAudit})
	t.Cleanup(func() { capability.Install(capability.Hooks{}) })
	human := func() context.Context {
		return capability.WithActor(context.Background(), &capability.Actor{UserID: 1, UserName: "rollback-admin", Type: capability.ActorHuman, Source: capability.SourceHTTP, Perms: []string{capability.SuperPerm}, MaxRisk: capability.RiskCritical, ApprovalMode: "diff_confirmed", ApprovedBy: "approver"})
	}
	counter := int64(0)
	create := func(count int, custom bool) *Set {
		t.Helper()
		a, _ := capability.ActorFrom(human())
		records := []capability.RecordedItem{}
		for range count {
			counter++
			if err := db.Exec("INSERT INTO rollback_fixture(id,value) VALUES (?,2)", counter).Error; err != nil {
				t.Fatal(err)
			}
			records = append(records, capability.RecordedItem{Entity: "rollback_fixture", ID: capability.ID(counter), Op: capability.OpUpdate, Before: map[string]any{"value": 1}, After: map[string]any{"value": 2}})
		}
		entry, _ := capability.Lookup(capName)
		var override json.RawMessage
		if custom {
			actions := []rollbackAction{}
			for _, record := range records {
				actions = append(actions, rollbackAction{Capability: "system.changeset.rollback", Guard: capName, Payload: raw(map[string]any{"entity": record.Entity, "id": record.ID, "op": "update", "before": record.Before, "after": record.After})})
			}
			override = raw(rollbackUndo{Actions: actions})
		}
		id, err := svc.Persist(human(), entry, a, records, override)
		if err != nil {
			t.Fatal(err)
		}
		set, err := svc.load(human(), id, false)
		if err != nil {
			t.Fatal(err)
		}
		if set.ApprovedBy != "approver" {
			t.Fatal("missing approver propagation")
		}
		return set
	}
	invoke := func(in RollbackIn) *Set {
		t.Helper()
		out, err := capability.InvokeJSON(human(), "system.changeset.rollback", raw(in))
		if err != nil {
			t.Fatal(err)
		}
		return out.(*Set)
	}
	preview := func(set *Set, ids []int64) *capability.Preview {
		t.Helper()
		out, err := svc.PreviewRollbackSelected(human(), RollbackIn{ID: set.ID, ItemIDs: ids})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	value := func(id string) int {
		t.Helper()
		var n int
		if err := db.Table("rollback_fixture").Select("value").Where("id=?", id).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	expectCode := func(in RollbackIn, code int) {
		t.Helper()
		_, err := capability.InvokeJSON(human(), "system.changeset.rollback", raw(in))
		if err == nil || errcode.From(err).Code != code {
			t.Fatalf("wanted %d, got %v", code, err)
		}
	}
	t.Run("selection-validation-proof-idempotence-and-inverse", func(t *testing.T) {
		set := create(2, false)
		one, two := set.Items[0], set.Items[1]
		ids := []int64{two.ID}
		p := preview(set, ids)
		if p.Affected != 1 || p.Selection.Remaining != 1 || p.ConfirmationHash == "" || p.Changes[0].ItemID != two.ID {
			t.Fatalf("bad selection preview %+v", p)
		}
		expectCode(RollbackIn{ID: set.ID, ItemIDs: []int64{}}, errcode.BadRequest)
		expectCode(RollbackIn{ID: set.ID, ItemIDs: []int64{two.ID, two.ID}}, errcode.BadRequest)
		other := create(1, false)
		expectCode(RollbackIn{ID: set.ID, ItemIDs: []int64{other.Items[0].ID}}, errcode.BadRequest)
		expectCode(RollbackIn{ID: set.ID, ItemIDs: ids}, errcode.StaleData)
		if err := db.Exec("UPDATE rollback_fixture SET value=3 WHERE id=?", two.EntityID).Error; err != nil {
			t.Fatal(err)
		}
		expectCode(RollbackIn{ID: set.ID, ItemIDs: ids, PreviewHash: p.ConfirmationHash}, errcode.StaleData)
		if value(two.EntityID) != 3 {
			t.Fatal("stale confirm modified data")
		}
		if err := db.Exec("UPDATE rollback_fixture SET value=2 WHERE id=?", two.EntityID).Error; err != nil {
			t.Fatal(err)
		}
		in := RollbackIn{ID: set.ID, ItemIDs: ids, PreviewHash: p.ConfirmationHash}
		rolled := invoke(in)
		if rolled.Status != "partially_rolled_back" || rolled.RollbackID == "" || value(one.EntityID) != 2 || value(two.EntityID) != 1 {
			t.Fatalf("bad partial %+v", rolled)
		}
		if rolled.Items[1].RollbackID != rolled.RollbackID || rolled.Items[1].RolledBackAt == nil {
			t.Fatal("missing per-item rollback provenance")
		}
		var before int64
		db.Model(&Set{}).Count(&before)
		again := invoke(in)
		var after int64
		db.Model(&Set{}).Count(&after)
		if again.Status != "partially_rolled_back" || before != after || value(two.EntityID) != 1 {
			t.Fatal("retry was not idempotent")
		}
		inverse := invoke(RollbackIn{ID: rolled.RollbackID})
		original, err := svc.load(human(), set.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if original.Status != "applied" || original.Items[1].Status != "applied" || original.Items[1].RollbackID != "" || value(two.EntityID) != 2 {
			t.Fatalf("undo inverse failed %+v, inverse %+v", original, inverse)
		}
		redo := invoke(RollbackIn{ID: inverse.RollbackID})
		original, _ = svc.load(human(), set.ID, false)
		if original.Status != "partially_rolled_back" || value(two.EntityID) != 1 {
			t.Fatalf("redo inverse failed %+v redo %+v", original, redo)
		}
		remainder := invoke(RollbackIn{ID: set.ID})
		if remainder.Status != "rolled_back" || value(one.EntityID) != 1 || len(remainder.RollbackIDs) < 2 {
			t.Fatalf("remaining rollback failed %+v", remainder)
		}
		var auditCount int64
		if err := db.Table("sys_oper_log").Where("capability=? AND changeset_id=? AND status='0'", "system.changeset.rollback", remainder.RollbackID).Count(&auditCount).Error; err != nil || auditCount != 1 {
			t.Fatalf("missing inverse-linked audit: %d %v", auditCount, err)
		}
	})
	t.Run("conflicts-are-specific-and-retryable", func(t *testing.T) {
		set := create(2, false)
		first := set.Items[0]
		if err := db.Exec("UPDATE rollback_fixture SET value=8 WHERE id=?", first.EntityID).Error; err != nil {
			t.Fatal(err)
		}
		p := preview(set, nil)
		found := false
		for _, change := range p.Changes {
			if change.ItemID == first.ID {
				found = change.Status == "conflict" && len(change.ConflictFields) == 1 && change.ConflictFields[0] == "value"
			}
		}
		if !found {
			t.Fatalf("missing field conflict %+v", p)
		}
		result := invoke(RollbackIn{ID: set.ID})
		if result.Status != "rollback_failed" || result.Items[0].ErrorCode != errcode.StaleData || value(first.EntityID) != 8 || value(set.Items[1].EntityID) != 1 {
			t.Fatalf("bad conflict result %+v", result)
		}
		if err := db.Exec("UPDATE rollback_fixture SET value=2 WHERE id=?", first.EntityID).Error; err != nil {
			t.Fatal(err)
		}
		ids := []int64{first.ID}
		p = preview(set, ids)
		result = invoke(RollbackIn{ID: set.ID, ItemIDs: ids, PreviewHash: p.ConfirmationHash})
		if result.Status != "rolled_back" || result.Items[0].ErrorCode != 0 || result.Items[0].ErrorMsg != "" {
			t.Fatalf("failed retry %+v", result)
		}
	})
	t.Run("custom-groups-cannot-be-split-and-remain-reversible", func(t *testing.T) {
		set := create(2, true)
		ids := []int64{set.Items[0].ID, set.Items[1].ID}
		if _, err := svc.PreviewRollbackSelected(human(), RollbackIn{ID: set.ID, ItemIDs: ids[:1]}); err == nil || errcode.From(err).Code != errcode.Conflict {
			t.Fatalf("split preview allowed: %v", err)
		}
		expectCode(RollbackIn{ID: set.ID, ItemIDs: ids[:1], PreviewHash: "fake"}, errcode.Conflict)
		if value(set.Items[0].EntityID) != 2 || value(set.Items[1].EntityID) != 2 {
			t.Fatal("split changed group")
		}
		p := preview(set, ids)
		rolled := invoke(RollbackIn{ID: set.ID, ItemIDs: ids, PreviewHash: p.ConfirmationHash})
		if rolled.Status != "rolled_back" {
			t.Fatalf("custom rollback %+v", rolled)
		}
		invoke(RollbackIn{ID: rolled.RollbackID})
		original, _ := svc.load(human(), set.ID, false)
		if original.Status != "applied" || value(set.Items[0].EntityID) != 2 || value(set.Items[1].EntityID) != 2 {
			t.Fatalf("custom inverse failed %+v", original)
		}
	})
	t.Run("real-credential-custom-undo-and-source-boundaries", func(t *testing.T) {
		cfg, err := config.Load("../../configs/config.example.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if os.Getenv("INTENT_ADMIN_REDIS_ADDR") == "" {
			cfg.Redis.Addr = "127.0.0.1:16379"
		}
		cfg.Redis.KeyPrefix = "selective-rollback:" + uuid.NewString() + ":"
		c, err := cache.Open(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		defer func() {
			keys, _ := c.Keys(context.Background(), c.Prefix()+"*").Result()
			if len(keys) > 0 {
				c.Del(context.Background(), keys...)
			}
		}()
		cipher, err := secret.New(cfg.App.EncryptKey)
		if err != nil {
			t.Fatal(err)
		}
		resources.Register(db, c, cfg, cipher)
		out, err := capability.InvokeJSON(human(), "system.aiProvider.create", raw(resources.ProviderInput{Name: "Credential fixture", Vendor: "custom", BaseURL: "https://example.com/v1", APIKey: "credential-before-private", DefaultModel: "test-model"}))
		if err != nil {
			t.Fatal(err)
		}
		provider := out.(resources.ProviderView)
		ctx := human()
		a, _ := capability.ActorFrom(ctx)
		a.ChangesetID, err = svc.Begin(ctx, a, "Provider and independent row")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = capability.InvokeJSON(ctx, "system.aiProvider.update", raw(resources.ProviderInput{ProviderID: provider.ProviderID, Name: "Credential fixture", Vendor: "custom", BaseURL: "https://example.com/v1", APIKey: "credential-after-private", DefaultModel: "test-model"})); err != nil {
			t.Fatal(err)
		}
		counter++
		if err = db.Exec("INSERT INTO rollback_fixture(id,value)VALUES(?,2)", counter).Error; err != nil {
			t.Fatal(err)
		}
		entry, _ := capability.Lookup(capName)
		if _, err = svc.Persist(ctx, entry, a, []capability.RecordedItem{{Entity: "rollback_fixture", ID: capability.ID(counter), Op: capability.OpUpdate, Before: map[string]any{"value": 1}, After: map[string]any{"value": 2}}}, nil); err != nil {
			t.Fatal(err)
		}
		set, err := svc.load(human(), a.ChangesetID, false)
		if err != nil {
			t.Fatal(err)
		}
		var item Item
		for _, candidate := range set.Items {
			if candidate.Entity == "sys_ai_provider" {
				item = candidate
			}
		}
		ids := []int64{item.ID}
		p := preview(set, ids)
		// The generic rollback entry must not open provider configuration to AI or MCP.
		for _, source := range []capability.Source{capability.SourceAI, capability.SourceMCP} {
			denied := human()
			actor, _ := capability.ActorFrom(denied)
			actor.Source = source
			check, err := svc.PreviewRollbackSelected(denied, RollbackIn{ID: set.ID, ItemIDs: ids})
			if err != nil {
				t.Fatal(err)
			}
			if check.Affected != 0 || check.Skipped != 1 {
				t.Fatalf("closed provider source exposed: %+v", check)
			}
		}
		rolled := invoke(RollbackIn{ID: set.ID, ItemIDs: ids, PreviewHash: p.ConfirmationHash})
		readKey := func() string {
			t.Helper()
			var row model.AIProvider
			if err := db.Where("provider_id=?", provider.ProviderID).First(&row).Error; err != nil {
				t.Fatal(err)
			}
			plain, err := cipher.Decrypt(row.APIKeyEnc)
			if err != nil {
				t.Fatal(err)
			}
			return plain
		}
		if rolled.Status != "partially_rolled_back" || readKey() != "credential-before-private" {
			t.Fatalf("credential partial failed %+v", rolled)
		}
		for _, source := range []capability.Source{capability.SourceAI, capability.SourceMCP} {
			denied := human()
			actor, _ := capability.ActorFrom(denied)
			actor.Source = source
			inverse, err := svc.load(human(), rolled.RollbackID, false)
			if err != nil {
				t.Fatal(err)
			}
			// Direct compensation enforces the original source visibility before changing the key.
			if err = svc.Undo(denied, inverse.Items[0].Undo); err == nil || errcode.From(err).Code != errcode.NotFound {
				t.Fatalf("inverse provider bypass: %v", err)
			}
		}
		invoke(RollbackIn{ID: rolled.RollbackID})
		if readKey() != "credential-after-private" {
			t.Fatal("custom credential inverse lost current key")
		}
		original, _ := svc.load(human(), set.ID, false)
		if original.Status != "applied" {
			t.Fatalf("credential source state not restored: %+v", original)
		}
		var stored []Item
		if err = db.Where("changeset_id IN ?", []string{set.ID, rolled.RollbackID}).Find(&stored).Error; err != nil {
			t.Fatal(err)
		}
		encoded := string(raw(stored))
		if strings.Contains(encoded, "credential-before-private") || strings.Contains(encoded, "credential-after-private") {
			t.Fatal("credential leaked in changeset snapshots")
		}
		// Inspect the otherwise-private undo column as well; only opaque vault refs are persisted.
		for _, snapshot := range stored {
			if strings.Contains(string(snapshot.Undo), "credential-before-private") || strings.Contains(string(snapshot.Undo), "credential-after-private") {
				t.Fatal("credential leaked in compensation payload")
			}
		}
	})

}

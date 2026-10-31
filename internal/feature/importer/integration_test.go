package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
	"github.com/xuri/excelize/v2"

	"github.com/junler/intent-admin/internal/adapter/httpx"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/changeset"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/feature/demo"
	"github.com/junler/intent-admin/internal/feature/system"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/migrations"
)

// This suite creates and drops its own database, never reusing operator data.
func TestImportIntegration(t *testing.T) {
	if os.Getenv("INTENT_ADMIN_IMPORT_INTEGRATION") != "1" {
		t.Skip("set INTENT_ADMIN_IMPORT_INTEGRATION=1 with disposable PostgreSQL credentials")
	}
	cfg, err := config.Load("../../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cfg.DB.Name, "_integration") {
		t.Fatal("disposable integration configuration required")
	}
	adminCfg := cfg.DB
	adminCfg.Name = "postgres"
	admin, err := pgx.Connect(context.Background(), adminCfg.DSN())
	if err != nil {
		t.Fatal(err)
	}
	database := fmt.Sprintf("intent_import_%d_integration", time.Now().UnixNano())
	cfg.DB.Name = database
	if _, err = admin.Exec(context.Background(), `CREATE DATABASE "`+database+`"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP DATABASE "`+database+`" WITH (FORCE)`)
		_ = admin.Close(context.Background())
	})
	db, err := dbx.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbx.Close(db)
	sqlDB, _ := db.DB()
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("schema_migrations")
	_ = goose.SetDialect("postgres")
	if err = goose.Up(sqlDB, "."); err != nil {
		t.Fatal(err)
	}
	redis := miniredis.RunT(t)
	cfg.Redis.Addr = redis.Addr()
	cfg.Redis.KeyPrefix = "import-test:"
	c, err := cache.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	demo.Register(db, nil)
	sys := system.Register(db, c, cfg)
	sets := changeset.New(db)
	sets.AuthorizeUndo = system.AuthorizeUndo(sys)
	sets.Invalidate = sys.Invalidate
	sets.Register()
	v := validator.New(validator.WithRequiredStructEnabled())
	capability.Install(capability.Hooks{Tx: dbx.NewTxRunner(db), ReadOnly: dbx.NewReadOnlyRunner(), PersistChanges: sets.Persist, WriteAudit: sets.WriteAudit, Validate: func(in any) error { return v.Struct(in) }, MaxChangesetItems: MaxRows})
	s := New(db, c, sets)
	actor := &capability.Actor{UserID: 1, UserName: "admin", Type: capability.ActorHuman, Source: capability.SourceHTTP, MaxRisk: capability.RiskCritical, Perms: []string{capability.SuperPerm}}
	ctx := capability.WithActor(context.Background(), actor)
	preview := func(name string, headers []string, rows [][]any) *Report {
		t.Helper()
		report, e := s.Preview(ctx, testSpec(name), bytes.NewReader(fileBytes(t, headers, rows)))
		if e != nil {
			t.Fatal(e)
		}
		return report
	}
	confirm := func(ctx context.Context, name string, r *Report) (Result, error) {
		ctx = capability.WithCallResult(ctx, &capability.CallResult{})
		a, _ := capability.ActorFrom(ctx)
		confirmed := *a
		confirmed.ApprovalMode = "diff_confirmed"
		ctx = capability.WithActor(ctx, &confirmed)
		out, e := capability.InvokeTyped(ctx, importName(testSpec(name)), &ConfirmInput{ConfirmationID: r.ConfirmationID, Fingerprint: r.Fingerprint})
		if e != nil {
			return Result{}, e
		}
		return out.(Result), nil
	}
	count := func(table string, query string, args ...any) int64 {
		t.Helper()
		var n int64
		if e := db.Table(table).Where(query, args...).Count(&n).Error; e != nil {
			t.Fatal(e)
		}
		return n
	}
	baseline := count("demo_item", "1=1")
	setsBefore := count("sys_changeset", "1=1")
	r := preview("demo.item.create", []string{"name", "category", "amount"}, [][]any{{"first", "import", 100}, {"second", "import", 200}})
	if !r.Valid || r.Preview.Affected != 2 || count("demo_item", "1=1") != baseline || count("sys_changeset", "1=1") != setsBefore {
		t.Fatal("preview mutated data", r)
	}
	other := *actor
	other.UserID = 2
	if _, _, e := s.load(capability.WithActor(context.Background(), &other), testSpec("demo.item.create"), r.ConfirmationID); e == nil || errcode.From(e).Code != errcode.NotFound {
		t.Fatal("foreign preview readable", e)
	}
	_, err = capability.InvokeTyped(ctx, "demo.item.import", &ConfirmInput{ConfirmationID: r.ConfirmationID, Fingerprint: strings.Repeat("a", 64)})
	if err == nil || errcode.From(err).Code != errcode.StaleData {
		t.Fatal("argument tampering accepted", err)
	}
	result, err := confirm(ctx, "demo.item.create", r)
	if err != nil {
		t.Fatal(err)
	}
	if result.Affected != 2 || result.ChangesetID == "" || count("sys_changeset_item", "changeset_id=?", result.ChangesetID) != 2 || count("sys_changeset", "1=1") != setsBefore+1 {
		t.Fatal("import not grouped", result)
	}
	if count("sys_oper_log", "capability='demo.item.import' AND changeset_id=? AND status='0' AND approval_mode='diff_confirmed'", result.ChangesetID) != 1 {
		t.Fatal("outer import audit lost changeset linkage")
	}
	if _, err = confirm(ctx, "demo.item.create", r); err == nil {
		t.Fatal("confirmation replay accepted")
	}
	if _, err = capability.InvokeJSON(ctx, "system.changeset.rollback", json.RawMessage(`{"id":"`+result.ChangesetID+`"}`)); err != nil {
		t.Fatal("whole batch rollback", err)
	}
	if count("demo_item", "category='import' AND del_flag='0'") != 0 {
		t.Fatal("batch rollback left live rows")
	}

	// Every practical entity uses its real capability validation and rollback.
	for _, test := range []struct {
		name, table string
		headers     []string
		rows        [][]any
	}{
		{"system.user.create", "sys_user", []string{"userName", "nickName"}, [][]any{{"batch_user_a", "导入用户甲"}, {"batch_user_b", "导入用户乙"}}},
		{"system.role.create", "sys_role", []string{"roleName", "roleKey"}, [][]any{{"导入角色甲", "batch_role_a"}, {"导入角色乙", "batch_role_b"}}},
		{"system.config.create", "sys_config", []string{"configName", "configKey", "configValue"}, [][]any{{"导入配置甲", "custom.batch.a", "one"}, {"导入配置乙", "custom.batch.b", "two"}}},
		{"system.dictData.create", "sys_dict_data", []string{"dictType", "dictLabel", "dictValue"}, [][]any{{"sys_normal_disable", "导入选项甲", "batch_a"}, {"sys_normal_disable", "导入选项乙", "batch_b"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := preview(test.name, test.headers, test.rows)
			if !r.Valid {
				t.Fatal(r.Errors)
			}
			res, e := confirm(ctx, test.name, r)
			if e != nil {
				t.Fatal(e)
			}
			if count("sys_changeset_item", "changeset_id=? AND entity=?", res.ChangesetID, test.table) != 2 {
				t.Fatal("wrong rows")
			}
			var snapshots []struct{ Args, Before, After json.RawMessage }
			if e = db.Table("sys_changeset_item").Where("changeset_id=?", res.ChangesetID).Find(&snapshots).Error; e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(snapshots)
			if strings.Contains(string(raw), "123456") || strings.Contains(string(raw), "$2a$") {
				t.Fatal("initial password entered changeset")
			}
			if _, e = capability.InvokeJSON(ctx, "system.changeset.rollback", json.RawMessage(`{"id":"`+res.ChangesetID+`"}`)); e != nil {
				t.Fatal(e)
			}
		})
	}

	t.Run("stale default password", func(t *testing.T) {
		r := preview("system.user.create", []string{"userName", "nickName"}, [][]any{{"stale_password_user", "用户"}})
		if e := db.Exec("UPDATE sys_config SET config_value='changed-password' WHERE config_key='sys.user.initPassword'").Error; e != nil {
			t.Fatal(e)
		}
		if _, e := confirm(ctx, "system.user.create", r); e == nil || errcode.From(e).Code != errcode.StaleData {
			t.Fatal(e)
		}
		if count("sys_user", "user_name='stale_password_user'") != 0 {
			t.Fatal("stale password import applied")
		}
	})
	t.Run("concurrent database change", func(t *testing.T) {
		r := preview("demo.item.create", []string{"name", "category"}, [][]any{{"stale_a", "stale"}, {"stale_b", "stale"}})
		if e := db.Exec("INSERT INTO demo_item(name,category) VALUES('stale_b','stale')").Error; e != nil {
			t.Fatal(e)
		}
		if _, e := confirm(ctx, "demo.item.create", r); e == nil || errcode.From(e).Code != errcode.StaleData {
			t.Fatal(e)
		}
		if count("demo_item", "name='stale_a'") != 0 {
			t.Fatal("part of stale batch applied")
		}
	})
	t.Run("late row failure rolls everything back", func(t *testing.T) {
		r := preview("demo.item.create", []string{"name", "category"}, [][]any{{"rollback_a", "atomic"}, {"rollback_b", "atomic"}})
		before := count("sys_changeset", "1=1")
		if e := db.Exec(`CREATE FUNCTION reject_import_row() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.name='rollback_b' THEN RAISE EXCEPTION 'late constraint'; END IF; RETURN NEW; END $$`).Error; e != nil {
			t.Fatal(e)
		}
		if e := db.Exec(`CREATE TRIGGER reject_import_row BEFORE INSERT ON demo_item FOR EACH ROW EXECUTE FUNCTION reject_import_row()`).Error; e != nil {
			t.Fatal(e)
		}
		if _, e := confirm(ctx, "demo.item.create", r); e == nil {
			t.Fatal("constraint failure accepted")
		}
		if count("demo_item", "category='atomic'") != 0 || count("sys_changeset", "1=1") != before {
			t.Fatal("partial import or changeset committed")
		}
		_ = db.Exec("DROP TRIGGER reject_import_row ON demo_item").Error
	})
	t.Run("permissions revoked after upload", func(t *testing.T) {
		if e := db.Exec("INSERT INTO sys_user(user_name,nick_name,password) VALUES('import_operator','导入操作者','unused')").Error; e != nil {
			t.Fatal(e)
		}
		var uid int64
		_ = db.Table("sys_user").Select("user_id").Where("user_name='import_operator'").Scan(&uid).Error
		if e := db.Exec("INSERT INTO sys_user_perm(user_id,perm) VALUES(?,?)", uid, "demo:item:edit").Error; e != nil {
			t.Fatal(e)
		}
		op := *actor
		op.UserID = uid
		op.Perms = []string{"demo:item:edit"}
		opctx := capability.WithActor(context.Background(), &op)
		r, e := s.Preview(opctx, testSpec("demo.item.create"), bytes.NewReader(fileBytes(t, []string{"name"}, [][]any{{"revoked_row"}})))
		if e != nil {
			t.Fatal(e)
		}
		_ = db.Exec("DELETE FROM sys_user_perm WHERE user_id=?", uid).Error
		if _, e = confirm(opctx, "demo.item.create", r); e == nil || errcode.From(e).Code != errcode.Forbidden {
			t.Fatal("revoked permission reused", e)
		}
		if count("demo_item", "name='revoked_row'") != 0 {
			t.Fatal("unauthorized import applied")
		}
	})
	t.Run("one use under concurrent confirmation", func(t *testing.T) {
		r := preview("demo.item.create", []string{"name"}, [][]any{{"concurrent_once"}})
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Go(func() { _, e := confirm(ctx, "demo.item.create", r); results <- e })
		}
		wg.Wait()
		close(results)
		success := 0
		for e := range results {
			if e == nil {
				success++
			}
		}
		if success != 1 || count("demo_item", "name='concurrent_once'") != 1 {
			t.Fatal("concurrent confirmation replay", success)
		}
	})
	t.Run("expiration and closed model entry points", func(t *testing.T) {
		r := preview("demo.item.create", []string{"name"}, [][]any{{"expires"}})
		redis.FastForward(previewTTL + time.Second)
		if _, e := confirm(ctx, "demo.item.create", r); e == nil || errcode.From(e).Code != errcode.StaleData {
			t.Fatal(e)
		}
		for _, source := range []capability.Source{capability.SourceAI, capability.SourceMCP} {
			a := *actor
			a.Source = source
			if _, e := confirm(capability.WithActor(context.Background(), &a), "demo.item.create", r); e == nil || errcode.From(e).Code != errcode.NotFound {
				t.Fatal("model entry exposed", e)
			}
		}
	})
	t.Run("HTTP template upload and protected errors", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.Use(func(c *gin.Context) {
			a := *actor
			if c.GetHeader("X-Test-Owner") == "other" {
				a.UserID = 2
			}
			c.Request = c.Request.WithContext(capability.WithActor(c.Request.Context(), &a))
			c.Next()
		})
		s.Mount(router.Group("/api"))
		httpx.Mount(router.Group("/api"), capability.All(), httpx.Options{})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/api/system/user/import/template?locale=en-US", nil))
		if !strings.Contains(w.Header().Get("Content-Type"), "spreadsheetml") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Header(), w.Body.String())
		}
		rows, errs, _, e := parse(testSpec("system.user.create"), bytes.NewReader(w.Body.Bytes()))
		if e == nil || len(rows) != 0 || len(errs) != 0 {
			t.Fatal("template should have headers without a fake data row")
		}
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("file", "users.xlsx")
		_, _ = part.Write(fileBytes(t, []string{"userName", "nickName"}, [][]any{{"admin", "duplicate"}, {"bad_name!", "invalid"}}))
		_ = form.Close()
		req := httptest.NewRequest("POST", "/api/system/user/import/preview", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var response struct {
			Code int    `json:"code"`
			Data Report `json:"data"`
		}
		if e = json.Unmarshal(w.Body.Bytes(), &response); e != nil {
			t.Fatal(e)
		}
		if response.Code != 200 || response.Data.Valid || len(response.Data.Errors) != 2 {
			t.Fatal(w.Body.String())
		}
		confirmationID := response.Data.ConfirmationID
		path := "/api/system/user/import/" + confirmationID + "/errors?locale=en-US"
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if !strings.Contains(w.Header().Get("Content-Type"), "spreadsheetml") {
			t.Fatal(w.Body.String())
		}
		errFile, e := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
		if e != nil {
			t.Fatal(e)
		}
		defer errFile.Close()
		message, _ := errFile.GetCellValue("Sheet1", "B2")
		if message != "The unique key already exists. Imports can only create records." {
			t.Fatal("English errors not localized", message)
		}
		foreign := httptest.NewRequest("GET", path, nil)
		foreign.Header.Set("X-Test-Owner", "other")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, foreign)
		if e = json.Unmarshal(w.Body.Bytes(), &response); e != nil || response.Code != errcode.NotFound {
			t.Fatal("foreign errors downloaded", w.Body.String())
		}
		foreign = httptest.NewRequest("DELETE", "/api/system/user/import/"+confirmationID, nil)
		foreign.Header.Set("X-Test-Owner", "other")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, foreign)
		if e = json.Unmarshal(w.Body.Bytes(), &response); e != nil || response.Code != errcode.NotFound {
			t.Fatal("foreign preview discarded", w.Body.String())
		}
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/system/user/import/"+confirmationID, nil))
		if e = json.Unmarshal(w.Body.Bytes(), &response); e != nil || response.Code != 200 {
			t.Fatal(w.Body.String())
		}
		if _, _, e = s.load(ctx, testSpec("system.user.create"), confirmationID); e == nil || errcode.From(e).Code != errcode.StaleData {
			t.Fatal("discard retained preview", e)
		}
	})
	t.Run("large amounts remain exact in reviewed diff", func(t *testing.T) {
		r := preview("demo.item.create", []string{"name", "amount"}, [][]any{{"large_exact_amount", "9007199254740993"}})
		var amount any
		for _, f := range r.Preview.Changes[0].Fields {
			if f.Field == "amount" {
				amount = f.After
			}
		}
		if amount != "9007199254740993" {
			t.Fatalf("browser would round reviewed amount: %#v", amount)
		}
		if _, e := confirm(ctx, "demo.item.create", r); e != nil {
			t.Fatal(e)
		}
		var stored int64
		if e := db.Table("demo_item").Select("amount").Where("name='large_exact_amount'").Scan(&stored).Error; e != nil || stored != 9007199254740993 {
			t.Fatal(stored, e)
		}
	})
	t.Run("preview quotas and unavailable Redis fail closed", func(t *testing.T) {
		redis.FastForward(previewTTL + time.Second)
		var last *Report
		for i := range 5 {
			last = preview("demo.item.create", []string{"name"}, [][]any{{fmt.Sprintf("quota_%d", i)}})
		}
		_, e := s.Preview(ctx, testSpec("demo.item.create"), bytes.NewReader(fileBytes(t, []string{"name"}, [][]any{{"quota_extra"}})))
		if e == nil || errcode.From(e).Code != errcode.TooManyReq {
			t.Fatal("preview quota not enforced", e)
		}
		before := count("sys_changeset", "1=1")
		redis.SetError("ERR unavailable")
		_, e = confirm(ctx, "demo.item.create", last)
		redis.SetError("")
		if e == nil || errcode.From(e).Code != errcode.InternalError || count("sys_changeset", "1=1") != before || count("demo_item", "name LIKE 'quota_%'") != 0 {
			t.Fatal("Redis failure did not fail closed", e)
		}
	})
}

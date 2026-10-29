package system_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/pressly/goose/v3"

	"github.com/junler/intent-admin/internal/adapter/httpx"
	"github.com/junler/intent-admin/internal/auth"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/capability/undo"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	feature "github.com/junler/intent-admin/internal/feature/system"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/migrations"
)

// The same test always validates the complete route registry. Opt in to real
// PostgreSQL behavioral checks with INTENT_ADMIN_SYSTEM_INTEGRATION=1.
func TestSystemManagement(t *testing.T) {
	cfg, err := config.Load("../../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	redis := miniredis.RunT(t)
	cfg.Redis.Addr = redis.Addr()
	cfg.Redis.KeyPrefix = "system-test:"
	c, err := cache.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if os.Getenv("INTENT_ADMIN_SYSTEM_INTEGRATION") != "1" {
		feature.Register(nil, c, cfg)
		gin.SetMode(gin.TestMode)
		router := gin.New()
		httpx.Mount(router.Group("/api"), capability.All(), httpx.Options{})
		if capability.Count() < 50 {
			t.Fatal("system capability catalogue incomplete")
		}
		if err := capability.SelfCheck(); err != nil {
			t.Fatal(err)
		}
		return
	}
	if !strings.HasSuffix(cfg.DB.Name, "_integration") {
		t.Fatal("disposable integration database required")
	}
	db, err := dbx.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbx.Close(db)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("schema_migrations")
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.Up(sqlDB, "."); err != nil {
		t.Fatal(err)
	}
	systemService := feature.Register(db, c, cfg)
	var lastRecords []capability.RecordedItem
	var lastOverride json.RawMessage
	dictionaryChanges := 0
	systemService.OnDictionaryChange = func() { dictionaryChanges++ }
	capability.Install(capability.Hooks{Tx: dbx.NewTxRunner(db), ReadOnly: dbx.NewReadOnlyRunner(), PersistChanges: func(ctx context.Context, e *capability.Entry, a *capability.Actor, rows []capability.RecordedItem, override json.RawMessage) (string, error) {
		lastRecords = rows
		lastOverride = append(json.RawMessage(nil), override...)
		return "", nil
	}})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	httpx.Mount(router.Group("/api"), capability.All(), httpx.Options{})
	if err = capability.SelfCheck(); err != nil {
		t.Fatal(err)
	}
	ctx := capability.WithActor(context.Background(), &capability.Actor{UserID: 1, UserName: "admin", Type: capability.ActorHuman, Source: capability.SourceHTTP, Perms: []string{capability.SuperPerm}, MaxRisk: capability.RiskCritical})
	call := func(ctx context.Context, name string, in any) any {
		t.Helper()
		raw, _ := json.Marshal(in)
		out, err := capability.InvokeJSON(ctx, name, raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}
	reject := func(ctx context.Context, name string, in any, code int) {
		t.Helper()
		raw, _ := json.Marshal(in)
		_, err := capability.InvokeJSON(ctx, name, raw)
		if err == nil || errcode.From(err).Code != code {
			t.Fatalf("%s: got %v want%d", name, err, code)
		}
	}
	suffix := fmt.Sprint(time.Now().UnixNano())
	create := call(ctx, "system.user.create", map[string]any{"userName": "u" + suffix, "nickName": "测试用户", "password": "initial-pass"}).(map[string]any)
	uid := create["userId"].(int64)
	if uid <= 0 {
		t.Fatal("missing created ID")
	}
	role := call(ctx, "system.role.create", map[string]any{"roleName": "测试角色", "roleKey": "role" + suffix}).(map[string]any)
	rid := role["roleId"].(int64)
	menu := call(ctx, "system.menu.create", map[string]any{"menuName": "测试页面", "menuType": "C", "path": "/test" + suffix, "component": "demo/item/index", "perms": "test:" + suffix + ":list"}).(map[string]any)
	mid := menu["menuId"].(int64)
	call(ctx, "system.role.authMenu", map[string]any{"roleId": rid, "menuIds": []int64{mid}})
	if err = c.Set(context.Background(), c.Key("user_perms", fmt.Sprint(uid)), "stale", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	call(ctx, "system.user.authRole", map[string]any{"userId": uid, "roleIds": []int64{rid}})
	if c.Exists(context.Background(), c.Key("user_perms", fmt.Sprint(uid))).Val() != 0 {
		t.Fatal("authorization cache survived committed role grant")
	}
	call(ctx, "system.user.authPerm", map[string]any{"userId": uid, "permissions": []string{"demo:item:list"}})
	// Regular field updates and preview/undo share precisely the same patch.
	rec := capability.NewRecorder()
	recordCtx := capability.WithRecorder(ctx, rec)
	call(recordCtx, "system.user.update", map[string]any{"userId": uid, "email": "test@example.com"})
	got := call(ctx, "system.user.get", map[string]any{"userId": uid}).(map[string]any)
	if got["email"] != "test@example.com" || strings.Contains(fmt.Sprint(got), "initial-pass") {
		t.Fatal(got)
	}
	reject(ctx, "system.user.remove", map[string]any{"ids": []int64{1}}, errcode.Forbidden)
	reject(ctx, "system.user.changeStatus", map[string]any{"userId": 1, "status": "1"}, errcode.Forbidden)
	reject(ctx, "system.role.remove", map[string]any{"ids": []int64{1}}, errcode.Forbidden)
	low := capability.WithActor(context.Background(), &capability.Actor{UserID: uid, UserName: "reader", Source: capability.SourceHTTP, Perms: []string{"system:user:edit"}, MaxRisk: capability.RiskCritical})
	reject(low, "system.user.authRole", map[string]any{"userId": uid, "roleIds": []int64{1}}, errcode.Forbidden)
	// PAT creation returns its plaintext exactly once; listing and audit-safe
	// snapshots expose only its prefix. Revocation is checked on every request.
	own := capability.WithActor(context.Background(), &capability.Actor{UserID: uid, UserName: "reader", Source: capability.SourceHTTP, Perms: []string{}, MaxRisk: capability.RiskCritical})
	tok := call(own, "system.token.create", map[string]any{"name": "test token", "scopes": []string{"demo:item:list"}, "maxRisk": "low"}).(map[string]any)
	plain := tok["token"].(string)
	tid := tok["tokenId"].(int64)
	a := auth.New(db, c, cfg)
	actor, err := a.AuthenticatePAT(context.Background(), plain)
	if err != nil {
		t.Fatal(err)
	}
	if actor.TokenID != tid || !actor.Can("demo:item:list") || actor.Can("system:user:list") || actor.MaxRisk != capability.RiskLow {
		t.Fatal(actor)
	}
	list := call(own, "system.token.list", map[string]any{})
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), plain) || strings.Contains(string(raw), "tokenHash") {
		t.Fatal("token secret leaked")
	}
	call(own, "system.token.revoke", map[string]any{"ids": []int64{tid}})
	_, err = a.AuthenticatePAT(context.Background(), plain)
	if err == nil || errcode.From(err).Code != 401 {
		t.Fatalf("revoked PAT accepted:%v", err)
	}

	roleCreate, _ := capability.Lookup("system.role.create")
	roleUndo, _ := json.Marshal(undo.Payload{Entity: "sys_role", ID: fmt.Sprint(rid), Op: capability.OpCreate})
	if err = roleCreate.Undo(ctx, roleUndo); err == nil || errcode.From(err).Code != errcode.Conflict {
		t.Fatalf("undo create cascaded a later rolegrant:%v", err)
	}

	// Both wildcard sources define superaccounts, including inverse compensation.
	call(ctx, "system.menu.update", map[string]any{"menuId": mid, "perms": "*:*:*"})
	reject(ctx, "system.user.changeStatus", map[string]any{"userId": uid, "status": "1"}, errcode.Forbidden)
	call(ctx, "system.menu.update", map[string]any{"menuId": mid, "perms": "test:" + suffix + ":list"})
	call(ctx, "system.user.authPerm", map[string]any{"userId": uid, "permissions": []string{"*:*:*"}})
	inverseGuard := feature.AuthorizeUndo(systemService)
	if err = inverseGuard(low, undo.Payload{Entity: "sys_user", ID: fmt.Sprint(uid), Op: capability.OpUpdate, Before: map[string]any{"status": "1"}, After: map[string]any{"status": "0"}}); err == nil || errcode.From(err).Code != errcode.Forbidden {
		t.Fatalf("inverse bypassed wildcard superprotection:%v", err)
	}
	call(ctx, "system.user.authPerm", map[string]any{"userId": uid, "permissions": []string{"demo:item:list"}})

	// Undo of a role grant uses the same guard and invalidates authorization.
	entry, _ := capability.Lookup("system.user.authRole")
	payload, _ := json.Marshal(undo.Payload{Entity: "sys_user_role", ID: fmt.Sprint(uid), Op: capability.OpUpdate, Before: map[string]any{"role_ids": []int64{}}, After: map[string]any{"role_ids": []int64{rid}}})
	if err = dbx.NewTxRunner(db)(ctx, func(txctx context.Context) error { return entry.Undo(txctx, payload) }); err != nil {
		t.Fatal(err)
	}
	got = call(ctx, "system.user.get", map[string]any{"userId": uid}).(map[string]any)
	if len(got["roleIds"].([]int64)) != 0 {
		t.Fatal("role undo failed")
	}

	// Exercise the remaining resource families and enforce reference guards.
	dict := call(ctx, "system.dictType.create", map[string]any{"dictName": "测试字典", "dictType": "test_" + suffix}).(map[string]any)
	did := dict["dictId"].(int64)
	item := call(ctx, "system.dictData.create", map[string]any{"dictLabel": "启用", "dictValue": "0", "dictType": "test_" + suffix, "dictSort": 0}).(map[string]any)
	dc := item["dictCode"].(int64)
	reject(ctx, "system.dictType.remove", map[string]any{"ids": []int64{did}}, errcode.Conflict)
	values := call(ctx, "system.dictData.byType", map[string]any{"dictType": "test_" + suffix}).([]map[string]any)
	if len(values) != 1 || values[0]["dictLabel"] != "启用" {
		t.Fatal(values)
	}

	if c.Exists(context.Background(), c.Key("dict", "test_"+suffix)).Val() != 1 {
		t.Fatal("dictionary cache was not populated")
	}

	call(ctx, "system.dictData.update", map[string]any{"dictCode": dc, "dictLabel": "正常"})
	if c.Exists(context.Background(), c.Key("dict", "test_"+suffix)).Val() != 0 || dictionaryChanges == 0 {
		t.Fatal("dictionary cache/knowledge were not invalidated")
	}

	call(ctx, "system.dictData.remove", map[string]any{"ids": []int64{dc}})
	call(ctx, "system.dictType.remove", map[string]any{"ids": []int64{did}})
	cfgRow := call(ctx, "system.config.create", map[string]any{"configName": "测试参数", "configKey": "test." + suffix, "configValue": "hello"}).(map[string]any)
	cid := cfgRow["configId"].(int64)
	if v := call(ctx, "system.config.byKey", map[string]any{"configKey": "test." + suffix}); v != "hello" {
		t.Fatal(v)
	}

	if c.Exists(context.Background(), c.Key("config", "test."+suffix)).Val() != 1 {
		t.Fatal("configuration cache was not populated")
	}
	call(ctx, "system.config.update", map[string]any{"configId": cid, "configValue": "updated"})
	if c.Exists(context.Background(), c.Key("config", "test."+suffix)).Val() != 0 {
		t.Fatal("configuration update retained stale cache")
	}
	if v, err := systemService.ConfigValue(context.Background(), "test."+suffix); err != nil || v != "updated" {
		t.Fatal(v, err)
	}
	rollback := errors.New("rollback cache probe")
	err = dbx.NewTxRunner(db)(ctx, func(txctx context.Context) error {
		if e := dbx.DBFrom(txctx, db).Table("sys_config").Where("config_id=?", cid).Update("config_value", "uncommitted").Error; e != nil {
			return e
		}
		v, e := systemService.ConfigValue(txctx, "test."+suffix)
		if e != nil || v != "uncommitted" {
			t.Fatalf("transaction read stale cache:%q %v", v, e)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if v, err := systemService.ConfigValue(context.Background(), "test."+suffix); err != nil || v != "updated" {
		t.Fatal("rolledback value polluted cache", v, err)
	}
	// Credential-valued settings never appear in output/snapshots. Their
	// reversible compensation stores only an opaque encrypted-vault reference.
	secretKey := "test.secret." + suffix
	oldSecret := "old-secret-value"
	newSecret := "new-secret-value"
	secretRow := call(ctx, "system.config.create", map[string]any{"configName": "凭据参数", "configKey": secretKey, "configValue": oldSecret}).(map[string]any)
	secretID := secretRow["configId"].(int64)
	rowView := call(ctx, "system.config.get", map[string]any{"configId": secretID}).(map[string]any)
	if rowView["configValue"] != "***" {
		t.Fatal("credential setting leaked", rowView)
	}
	ai := capability.WithActor(context.Background(), &capability.Actor{UserID: 1, UserName: "admin", Source: capability.SourceAI, Perms: []string{capability.SuperPerm}, MaxRisk: capability.RiskHigh})
	reject(ai, "system.config.byKey", map[string]any{"configKey": secretKey}, errcode.Forbidden)
	reject(ai, "system.config.update", map[string]any{"configId": secretID, "configValue": "injected-value"}, errcode.Forbidden)
	args, _ := json.Marshal(map[string]any{"configId": secretID, "configValue": newSecret})
	proposal, e := capability.RunPreview(ctx, "system.config.update", args)
	if e != nil {
		t.Fatal(e)
	}
	previewJSON, _ := json.Marshal(proposal)
	if strings.Contains(string(previewJSON), oldSecret) || strings.Contains(string(previewJSON), newSecret) {
		t.Fatal("credential preview leaked value")
	}
	call(ctx, "system.config.update", map[string]any{"configId": secretID, "configValue": newSecret})
	snapshotJSON, _ := json.Marshal(lastRecords)
	if strings.Contains(string(snapshotJSON), oldSecret) || strings.Contains(string(snapshotJSON), newSecret) || strings.Contains(string(lastOverride), newSecret) {
		t.Fatal("credential changeset leaked value")
	}
	protectedUndo := append(json.RawMessage(nil), lastOverride...)
	if len(protectedUndo) == 0 {
		t.Fatal("credential setting has no safe undo")
	}
	configEntry, _ := capability.Lookup("system.config.update")
	inverse := capability.NewRecorder()
	if err = dbx.NewTxRunner(db)(capability.WithRecorder(ctx, inverse), func(txctx context.Context) error { return configEntry.Undo(txctx, protectedUndo) }); err != nil {
		t.Fatal(err)
	}
	if v, e := systemService.ConfigValue(context.Background(), secretKey); e != nil || v != oldSecret {
		t.Fatal("credential undo failed", e)
	}
	redo, ok := inverse.UndoOverride()
	if !ok {
		t.Fatal("credential undo has no safe inverse")
	}
	if err = dbx.NewTxRunner(db)(ctx, func(txctx context.Context) error { return configEntry.Undo(txctx, redo) }); err != nil {
		t.Fatal(err)
	}
	if v, e := systemService.ConfigValue(context.Background(), secretKey); e != nil || v != newSecret {
		t.Fatal("credential inverse failed", e)
	}
	call(ctx, "system.config.update", map[string]any{"configId": secretID, "configValue": "***"})
	if v, e := systemService.ConfigValue(context.Background(), secretKey); e != nil || v != newSecret {
		t.Fatal("masked edit overwrote credential", e)
	}
	call(ctx, "system.config.remove", map[string]any{"ids": []int64{secretID}})

	call(ctx, "system.config.remove", map[string]any{"ids": []int64{cid}})
	notice := call(ctx, "system.notice.create", map[string]any{"noticeTitle": "测试公告", "noticeContent": "内容"}).(map[string]any)
	call(ctx, "system.notice.update", map[string]any{"noticeId": notice["noticeId"], "noticeContent": "更新内容"})
	call(ctx, "system.notice.remove", map[string]any{"ids": []int64{notice["noticeId"].(int64)}})
	reject(ctx, "system.menu.update", map[string]any{"menuId": mid, "parentId": mid}, errcode.BadRequest)
	reject(ctx, "system.menu.create", map[string]any{"menuName": "无效外链", "menuType": "C", "perms": "docs:list", "isFrame": "0", "path": "javascript:alert(1)"}, errcode.BadRequest)
	external := call(ctx, "system.menu.create", map[string]any{"menuName": "外链", "menuType": "C", "perms": "docs:list", "isFrame": "0", "path": "https://example.com/help"}).(map[string]any)
	call(ctx, "system.menu.remove", map[string]any{"ids": []int64{external["menuId"].(int64)}})

	// Profile updates force the caller's ID and cannot mass-assign role/password.
	call(own, "system.profile.update", map[string]any{"userId": 1, "nickName": "本人资料", "password": "injected-password", "roleIds": []int64{1}})
	ownInfo := call(own, "system.profile.get", map[string]any{}).(map[string]any)
	if ownInfo["userId"] != int32(uid) && ownInfo["userId"] != uid {
		t.Fatal(ownInfo)
	}

	if err = systemService.CheckPassword(context.Background(), uid, "initial-pass"); err != nil {
		t.Fatal("profile mass-assigned password", err)
	}
	sessionKey := c.Key("login_token", "fake-session")
	snapshot, _ := json.Marshal(map[string]any{"userId": uid})
	if err = c.Set(context.Background(), sessionKey, snapshot, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	call(ctx, "system.user.resetPwd", map[string]any{"userId": uid, "password": "changed-pass"})
	if err = systemService.CheckPassword(context.Background(), uid, "changed-pass"); err != nil {
		t.Fatal(err)
	}
	if c.Exists(context.Background(), sessionKey).Val() != 0 {
		t.Fatal("password reset did not revoke session")
	}
	defaultUser := call(ctx, "system.user.create", map[string]any{"userName": "default" + suffix, "nickName": "默认密码用户"}).(map[string]any)
	defaultID := defaultUser["userId"].(int64)
	initial, err := systemService.ConfigValue(context.Background(), "sys.user.initPassword")
	if err != nil {
		t.Fatal(err)
	}
	if err = systemService.CheckPassword(context.Background(), defaultID, initial); err != nil {
		t.Fatal("configured initial password was not used", err)
	}

	userCreate, _ := capability.Lookup("system.user.create")
	userUndo, _ := json.Marshal(undo.Payload{Entity: "sys_user", ID: fmt.Sprint(defaultID), Op: capability.OpCreate})
	if err = dbx.NewTxRunner(db)(ctx, func(txctx context.Context) error { return userCreate.Undo(txctx, userUndo) }); err != nil {
		t.Fatal(err)
	}
	// Hash stays in its hidden account row, never an undo payload.
	var alive string
	if err = db.Table("sys_user").Select("del_flag").Where("user_id=?", defaultID).Scan(&alive).Error; err != nil || alive != "2" {
		t.Fatal("creation undo did not hide account", err)
	}
	restore := undo.Payload{Entity: "sys_user", ID: fmt.Sprint(defaultID), Op: capability.OpUpdate, Before: map[string]any{"del_flag": "0"}, After: map[string]any{"del_flag": "2"}}
	if err = feature.AuthorizeUndo(systemService)(ctx, restore); err != nil {
		t.Fatal(err)
	}
	if err = db.Table("sys_user").Where("user_id=?", defaultID).Update("del_flag", "0").Error; err != nil {
		t.Fatal(err)
	}
	if err = systemService.CheckPassword(context.Background(), defaultID, initial); err != nil {
		t.Fatal("account inverse lost bcrypt hash", err)
	}

	call(ctx, "system.user.remove", map[string]any{"ids": []int64{defaultID}})

	call(ctx, "system.user.remove", map[string]any{"ids": []int64{uid}})
	call(ctx, "system.menu.remove", map[string]any{"ids": []int64{mid}})
	call(ctx, "system.role.remove", map[string]any{"ids": []int64{rid}})
}

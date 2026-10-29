package bootstrap

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/migrations"
)

// Run against an isolated PostgreSQL database and Redis instance:
// INTENT_ADMIN_INTEGRATION=1 go test ./internal/bootstrap -run TestAuthenticationIntegration -v
// DB/Redis settings use the usual INTENT_ADMIN_* configuration overrides.
func TestAuthenticationIntegration(t *testing.T) {
	if os.Getenv("INTENT_ADMIN_INTEGRATION") != "1" {
		t.Skip("requires isolated PostgreSQL and Redis; set INTENT_ADMIN_INTEGRATION=1")
	}
	cfg, err := config.Load("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cfg.DB.Name, "_integration") {
		t.Fatal("integration tests require a disposable database ending in _integration")
	}
	cfg.Redis.KeyPrefix = "intent-admin-test:" + time.Now().Format("20060102150405.000000000") + ":"
	db, err := sql.Open("pgx", cfg.DB.DSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("schema_migrations")
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatal(err)
	}
	app, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	if err := app.DB.Exec("UPDATE sys_config SET config_value = 'false' WHERE config_key = 'sys.account.registerUser'").Error; err != nil {
		t.Fatal(err)
	}
	router := app.Router()
	ctx := context.Background()
	t.Cleanup(func() {
		var cursor uint64
		for {
			keys, next, err := app.Cache.Scan(ctx, cursor, cfg.Redis.KeyPrefix+"*", 100).Result()
			if err != nil {
				t.Error(err)
				return
			}
			if len(keys) > 0 {
				_ = app.Cache.Del(ctx, keys...).Err()
			}
			cursor = next
			if cursor == 0 {
				return
			}
		}
	})
	request := func(method, path, token string, body any) (int, map[string]any) {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "/api"+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var response map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("%s: %s: %v", path, w.Body.String(), err)
		}
		return w.Code, response
	}
	expect := func(method, path, token string, body any, status, code int) map[string]any {
		t.Helper()
		gotStatus, response := request(method, path, token, body)
		if gotStatus != status || int(response["code"].(float64)) != code {
			t.Fatalf("%s %s: HTTP %d, body %v", method, path, gotStatus, response)
		}
		return response
	}
	login := func(name, password string) string {
		t.Helper()
		challenge := expect(http.MethodGet, "/captchaImage", "", nil, 200, 200)["data"].(map[string]any)
		code, err := app.Cache.Get(ctx, app.Cache.Key("captcha", challenge["uuid"].(string))).Result()
		if err != nil {
			t.Fatal(err)
		}
		body := map[string]any{"username": name, "password": password, "uuid": challenge["uuid"], "code": code}
		return expect(http.MethodPost, "/login", "", body, 200, 200)["data"].(map[string]any)["token"].(string)
	}

	expect("GET", "/demo/item/list", "", nil, 401, 401)
	adminToken := login("admin", "123456")
	info := expect("GET", "/getInfo", adminToken, nil, 200, 200)["data"].(map[string]any)
	if info["permissions"].([]any)[0] != capability.SuperPerm {
		t.Fatalf("admin permissions: %v", info)
	}
	encoded, _ := json.Marshal(info)
	if strings.Contains(string(encoded), "password") || strings.Contains(string(encoded), "$2a$") {
		t.Fatal("password leaked in user info")
	}
	routes := expect("GET", "/getRouters", adminToken, nil, 200, 200)["data"].([]any)
	if len(routes) != 2 {
		t.Fatalf("expected existing demo/capability menus, got %v", routes)
	}
	expect("GET", "/demo/item/list", adminToken, nil, 200, 200)

	// Public signup is disabled until explicitly enabled by configuration.
	limitedName := fmt.Sprintf("limited_%d", time.Now().UnixNano())
	expect("POST", "/register", "", map[string]any{"username": limitedName, "password": "123456"}, 403, 403)
	t.Cleanup(func() {
		app.DB.Exec("UPDATE sys_config SET config_value = 'false' WHERE config_key = 'sys.account.registerUser'")
	})
	if err := app.DB.Exec("UPDATE sys_config SET config_value = 'true' WHERE config_key = 'sys.account.registerUser'").Error; err != nil {
		t.Fatal(err)
	}
	challenge := expect("GET", "/captchaImage", "", nil, 200, 200)["data"].(map[string]any)
	code, err := app.Cache.Get(ctx, app.Cache.Key("captcha", challenge["uuid"].(string))).Result()
	if err != nil {
		t.Fatal(err)
	}
	register := map[string]any{"username": limitedName, "password": "123456", "uuid": challenge["uuid"], "code": code}
	expect("POST", "/register", "", register, 200, 200)
	expect("POST", "/register", "", register, 200, 400) // consumed captcha
	limited := login(limitedName, "123456")
	expect("GET", "/demo/item/list", limited, nil, 403, 403)
	menus := expect("GET", "/getRouters", limited, nil, 200, 200)["data"].([]any)
	if len(menus) != 0 {
		t.Fatalf("ungranted menus: %v", menus)
	}

	// Extra grants add access without requiring a role; invalidation is explicit.
	var userID int64
	if err := app.DB.Raw("SELECT user_id FROM sys_user WHERE user_name = ?", limitedName).Scan(&userID).Error; err != nil {
		t.Fatal(err)
	}
	if err := app.DB.Exec("INSERT INTO sys_user_perm (user_id,perm) VALUES (?, 'demo:item:list')", userID).Error; err != nil {
		t.Fatal(err)
	}
	if err := app.Auth.InvalidatePermissions(ctx, userID); err != nil {
		t.Fatal(err)
	}
	expect("GET", "/demo/item/list", limited, nil, 200, 200)
	expect("POST", "/demo/item/list/export", limited, map[string]any{}, 403, 403)

	// Extending the Redis session preserves its signed token; deleting it wins.
	payload, err := base64.RawURLEncoding.DecodeString(strings.Split(adminToken, ".")[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	key := app.Cache.Key("login_token", claims.UUID)
	if err := app.Cache.PExpire(ctx, key, time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	expect("POST", "/refreshToken", adminToken, nil, 200, 200)
	if ttl := app.Cache.TTL(ctx, key).Val(); ttl < cfg.JWT.Expire-time.Minute {
		t.Fatalf("refresh TTL = %v", ttl)
	}
	if err := app.Cache.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	expect("GET", "/getInfo", adminToken, nil, 401, 401)
	expect("POST", "/refreshToken", adminToken, nil, 401, 401)
	expect("POST", "/logout", limited, nil, 200, 200)
	expect("GET", "/getInfo", limited, nil, 401, 401)

	// The deployment risk setting is enforced even if an AI adapter supplies
	// an overbroad actor ceiling.
	capability.Install(capability.Hooks{MaxAIRisk: capability.RiskLow})
	actor := &capability.Actor{Source: capability.SourceAI, Perms: []string{capability.SuperPerm}, MaxRisk: capability.RiskCritical}
	_, err = capability.InvokeJSON(capability.WithActor(ctx, actor), "demo.item.update", json.RawMessage(`{"itemId":1,"status":"1"}`))
	if errcode.From(err).Code != errcode.RiskExceeded {
		t.Fatalf("risk gate: %v", err)
	}
}

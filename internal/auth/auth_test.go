package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"

	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/model"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

func testService(t *testing.T) (*Service, *miniredis.Miniredis) {
	t.Helper()
	r := miniredis.RunT(t)
	cfg := &config.Config{Redis: config.Redis{Addr: r.Addr(), KeyPrefix: "test:"}, JWT: config.JWT{Secret: "test-signing-key", Issuer: "test", Expire: 3 * time.Hour}, AI: config.AI{MaxRisk: "high"}}
	c, err := cache.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return New(nil, c, cfg), r
}
func requireCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil || errcode.From(err).Code != want {
		t.Fatalf("got %v, want code %d", err, want)
	}
}

func TestSignedTokenValidation(t *testing.T) {
	now := time.Now()
	id := uuid.NewString()
	token, err := signToken("key", "issuer", id, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseToken("key", "issuer", token, now)
	if err != nil || got != id {
		t.Fatalf("id=%s err=%v", got, err)
	}
	for _, tc := range []struct{ name, key, issuer, token string }{
		{"wrong signature", "wrong", "issuer", token},
		{"wrong issuer", "key", "other", token},
		{"tampered payload", "key", "issuer", strings.Replace(token, id, "different", 1) + "x"},
		{"malformed", "key", "issuer", "abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseToken(tc.key, tc.issuer, tc.token, now); err == nil {
				t.Fatal("accepted invalid token")
			}
		})
	}
	future, _ := signToken("key", "issuer", id, now.Add(2*time.Minute))
	if _, err := parseToken("key", "issuer", future, now); err == nil {
		t.Fatal("accepted future token")
	}
	header := strings.Split(token, ".")[0]
	raw, _ := base64.RawURLEncoding.DecodeString(header)
	if !strings.Contains(string(raw), `"HS256"`) {
		t.Fatal("wrong algorithm")
	}
}

func TestSessionRevocationAndRefresh(t *testing.T) {
	s, r := testService(t)
	ctx := context.Background()
	token, err := s.issueSession(ctx, &model.User{UserID: 7, UserName: "reader", Password: "must-not-be-cached"}, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	id, session, err := s.loadSession(ctx, token)
	if err != nil || session.UserID != 7 {
		t.Fatalf("session=%v err=%v", session, err)
	}
	raw, err := r.Get(s.sessionKey(id))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "must-not-be-cached") {
		t.Fatal("password cached in session")
	}
	ttl, err := s.refresh(ctx, id)
	if err != nil || ttl != 3*time.Hour {
		t.Fatalf("early refresh ttl=%s err=%v", ttl, err)
	}
	r.FastForward(2*time.Hour + time.Minute)
	ttl, err = s.refresh(ctx, id)
	if err != nil || ttl != 3*time.Hour {
		t.Fatalf("late refresh ttl=%s err=%v", ttl, err)
	}
	// The unchanged signed token remains valid beyond the initial session TTL.
	r.FastForward(time.Hour)
	if _, _, err := s.loadSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	r.Del(s.sessionKey(id))
	_, _, err = s.loadSession(ctx, token)
	requireCode(t, err, errcode.Unauthorized)
	_, err = s.refresh(ctx, id)
	requireCode(t, err, errcode.Unauthorized)
	if r.Exists(s.sessionKey(id)) {
		t.Fatal("refresh resurrected logged-out session")
	}
}

func TestSessionExpiresWithoutRefresh(t *testing.T) {
	s, r := testService(t)
	token, err := s.issueSession(context.Background(), &model.User{UserID: 1}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	r.FastForward(s.ttl())
	_, _, err = s.loadSession(context.Background(), token)
	requireCode(t, err, errcode.Unauthorized)
}

func TestLockoutStartsOnFifthFailure(t *testing.T) {
	s, r := testService(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		requireCode(t, s.failed(ctx, "reader"), errcode.BadRequest)
	}
	r.FastForward(9 * time.Minute)
	requireCode(t, s.failed(ctx, "reader"), errcode.TooManyReq)
	requireCode(t, s.checkLock(ctx, "reader"), errcode.TooManyReq)
	r.FastForward(2 * time.Minute)
	requireCode(t, s.checkLock(ctx, "reader"), errcode.TooManyReq)
	r.FastForward(8 * time.Minute)
	if err := s.checkLock(ctx, "reader"); err != nil {
		t.Fatal(err)
	}
}

func TestCaptchaConsumedOnFailureAndSuccess(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	id := uuid.NewString()
	key := s.cache.Key("captcha", id)
	if err := s.cache.Set(ctx, key, "12345", captchaTTL).Err(); err != nil {
		t.Fatal(err)
	}
	requireCode(t, s.checkCaptcha(ctx, true, id, "00000"), errcode.BadRequest)
	requireCode(t, s.checkCaptcha(ctx, true, id, "12345"), errcode.BadRequest)
	if err := s.cache.Set(ctx, key, "12345", captchaTTL).Err(); err != nil {
		t.Fatal(err)
	}
	if err := s.checkCaptcha(ctx, true, id, "12345"); err != nil {
		t.Fatal(err)
	}
	requireCode(t, s.checkCaptcha(ctx, true, id, "12345"), errcode.BadRequest)
	code, encoded, err := captchaPNG()
	if err != nil || len(code) != 5 {
		t.Fatalf("code=%q err=%v", code, err)
	}
	raw, decodeErr := base64.StdEncoding.DecodeString(encoded)
	if decodeErr != nil || len(raw) < 8 || string(raw[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatal("captcha is not a raster PNG")
	}
	if strings.Contains(string(raw), code) {
		t.Fatal("captcha leaks answer text")
	}
}

func TestRoutesFromEffectivePermissions(t *testing.T) {
	menu := func(id, parent int64, typ, path, perm string) model.Menu {
		return model.Menu{MenuID: id, ParentID: parent, MenuType: typ, Path: path, Perms: perm, Status: "0", DelFlag: "0"}
	}
	menus := []model.Menu{menu(1, 0, "M", "system", ""), menu(2, 1, "C", "users", "system:user:list"), menu(3, 1, "F", "", "system:user:edit"), menu(4, 0, "C", "home", ""), menu(5, 0, "M", "empty", ""), menu(6, 5, "C", "denied", "denied:list"), menu(7, 0, "M", "disabled", ""), menu(8, 7, "C", "child", "child:list"), menu(9, 0, "C", "deleted", "deleted:list")}
	menus[1].Visible = "1"
	menus[6].Status = "1"
	menus[8].DelFlag = "2"
	routes := BuildRoutes(menus, []string{"system:user:list", "child:list", "deleted:list"})
	if len(routes) != 2 || len(routes[0].Children) != 1 || !routes[0].Children[0].Hidden || routes[0].Children[0].Path != "users" {
		t.Fatalf("routes=%+v", routes)
	}
	if routes[1].Path != "home" {
		t.Fatal("missing authenticated home")
	}
	routes = BuildRoutes(menus, []string{"*:*:*"})
	if len(routes) != 3 {
		t.Fatalf("super routes=%+v", routes)
	}
	routes = BuildRoutes(menus, nil)
	if len(routes) != 1 || routes[0].Path != "home" {
		t.Fatalf("unprivileged routes=%+v", routes)
	}
	raw, _ := json.Marshal(routes)
	if strings.Contains(string(raw), "system:user:edit") {
		t.Fatal("button leaked into routes")
	}
}
func TestUserRiskCanOnlyNarrowGlobalLimit(t *testing.T) {
	s, _ := testService(t)
	low, critical, bad := "low", "critical", "invalid"
	for _, tc := range []struct {
		risk *string
		want capability.Risk
	}{{nil, capability.RiskHigh}, {&low, capability.RiskLow}, {&critical, capability.RiskHigh}, {&bad, capability.RiskLow}} {
		if got := s.effectiveRisk(&model.User{AIMaxRisk: tc.risk}); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}

func TestPublicRateLimitBoundsWorkAndFailsClosed(t *testing.T) {
	s, r := testService(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	calls := 0
	router.POST("/test", s.rateLimit("credentials", 2), func(c *gin.Context) { calls++; c.Status(http.StatusNoContent) })
	call := func(ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", ip)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	for i := 0; i < 2; i++ {
		if res := call("192.0.2.1"); res.Code != http.StatusNoContent {
			t.Fatal(res.Body.String())
		}
	}
	// Spoofed forwarding headers cannot create a fresh trusted IP bucket.
	if res := call("192.0.2.2"); res.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d %s", res.Code, res.Body.String())
	}
	if calls != 2 {
		t.Fatal("rate-limited handler invoked")
	}
	r.FastForward(time.Minute)
	if res := call(""); res.Code != http.StatusNoContent {
		t.Fatal(res.Body.String())
	}
	if err := s.cache.Client.Close(); err != nil {
		t.Fatal(err)
	}
	res := call("")
	if !strings.Contains(res.Body.String(), `"code":500`) || calls != 3 {
		t.Fatalf("Redis failure did not fail closed: %d %s", calls, res.Body.String())
	}
}

func TestExternalMenusAreExplicitAndValidated(t *testing.T) {
	for _, raw := range []string{"https://example.com/docs", "http://example.com/help?q=1", "HTTPS://example.com/docs"} {
		if !validExternalURL(raw) {
			t.Fatalf("validexternal rejected:%q", raw)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "data:text/html,<script>alert(1)</script>", "//example.com/", "https://user:pass@example.com/", "https://example.com/\n", " https://example.com/", "https://"} {
		if validExternalURL(raw) {
			t.Fatalf("unsafeexternal accepted:%q", raw)
		}
	}
	menus := []model.Menu{{MenuID: 50, MenuName: "文档", Path: "https://example.com/docs", IsFrame: "0", MenuType: "C", Perms: "docs:list", Status: "0", DelFlag: "0"}, {MenuID: 51, MenuName: "恶意", Path: "javascript:alert(1)", IsFrame: "0", MenuType: "C", Perms: "docs:list", Status: "0", DelFlag: "0"}}
	routes := BuildRoutes(menus, []string{"docs:list"})
	if len(routes) != 1 || !routes[0].External || routes[0].Path != "https://example.com/docs" {
		t.Fatalf("externalroute contract:%+v", routes)
	}
	if routes := BuildRoutes(menus, nil); len(routes) != 0 {
		t.Fatal("externalmenu bypassed permissions")
	}
}

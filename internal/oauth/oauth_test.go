package oauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/junler/intent-admin/internal/adapter/mcpx"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/migrations"
)

func TestStrictRedirectsAndScopes(t *testing.T) {
	for _, raw := range []string{"https://client.example/cb", "http://127.0.0.1:1234/cb", "http://[::1]:1234/cb"} {
		if _, err := validRedirect(raw); err != nil {
			t.Errorf("rejected %s", raw)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "http://client.example/cb", "http://localhost/cb", "https://user:pass@example.com/cb", "https://example.com/cb#secret", "https://example.com/cb#", "https://:443/cb", "http://127.0.0.1:99999/cb", "//example.com/cb", "https://example.com\\@evil.test/cb"} {
		if _, err := validRedirect(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if !redirectMatches("http://127.0.0.1:123/cb?q=1", "http://127.0.0.1:456/cb?q=1") {
		t.Fatal("loopback ephemeral port rejected")
	}
	for _, actual := range []string{"http://127.0.0.2:456/cb?q=1", "http://127.0.0.1:456/other?q=1", "http://127.0.0.1:456/cb?q=2", "https://127.0.0.1:456/cb?q=1"} {
		if redirectMatches("http://127.0.0.1:123/cb?q=1", actual) {
			t.Fatalf("accepted changed redirect %s", actual)
		}
	}
	if _, err := parseScopes("mcp:read admin"); err == nil {
		t.Fatal("accepted unknown scope")
	}
}
func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("INTENT_ADMIN_OAUTH_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("INTENT_ADMIN_TEST_DSN")
	}
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL via INTENT_ADMIN_OAUTH_TEST_DSN")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "oauth_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE"); _ = admin.Close() })
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
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

type harness struct {
	s         *Service
	server    *httptest.Server
	client    *http.Client
	liveWrite atomic.Bool
	disabled  atomic.Bool
	readName  string
	writeName string
	cache     *cache.Cache
}

func setupOAuth(t *testing.T) *harness {
	t.Helper()
	db := testDB(t)
	redisServer := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	h := &harness{cache: &cache.Cache{Client: rc}}
	h.liveWrite.Store(true)
	name := "oauth.fixture" + strings.ReplaceAll(uuid.NewString(), "-", "")
	h.readName = name + "Read"
	h.writeName = name + "Write"
	capability.Register(capability.Capability[struct{}, map[string]any]{Meta: capability.Meta{Name: h.readName, Summary: "OAuth 读取", Perm: "test:read", Risk: capability.RiskLow, MCPEnabled: true}, Fn: func(context.Context, struct{}) (map[string]any, error) { return map[string]any{"ok": true}, nil }})
	capability.Register(capability.Capability[struct{}, map[string]any]{Meta: capability.Meta{Name: h.writeName, Summary: "OAuth 写入", Perm: "test:write", Risk: capability.RiskMedium, MCPEnabled: true, Mutating: true}, Fn: func(context.Context, struct{}) (map[string]any, error) { return map[string]any{"ok": true}, nil }, DryRun: func(context.Context, struct{}) (*capability.Preview, error) { return &capability.Preview{}, nil }})
	r := gin.New()
	h.server = httptest.NewServer(r)
	t.Cleanup(h.server.Close)
	h.client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resolver := func(_ context.Context, id int64, scope []string, _ capability.Risk) (*capability.Actor, error) {
		if h.disabled.Load() {
			return nil, errcode.Unauthorizedf("disabled")
		}
		perms := []string{"test:read"}
		if h.liveWrite.Load() {
			perms = append(perms, "test:write")
		}
		if scope != nil {
			perms = slices.DeleteFunc(perms, func(p string) bool { return !slices.Contains(scope, p) })
		}
		return &capability.Actor{UserID: id, UserName: "admin", Perms: perms, MaxRisk: capability.RiskHigh, Type: capability.ActorMCPClient, Source: capability.SourceMCP}, nil
	}
	var err error
	h.s, err = New(db, h.cache, Options{Issuer: h.server.URL, ResourcePath: "/mcp"}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	h.s.MountPublic(r)
	api := r.Group("/api")
	api.Use(func(c *gin.Context) {
		id := int64(1)
		if c.GetHeader("Authorization") == "Bearer other-user" {
			id = 2
		}
		a := &capability.Actor{UserID: id, UserName: "admin", Type: capability.ActorHuman, Source: capability.SourceHTTP, Perms: []string{capability.SuperPerm}, MaxRisk: capability.RiskCritical}
		c.Request = c.Request.WithContext(capability.WithActor(c.Request.Context(), a))
		c.Next()
	})
	h.s.MountProtected(api)
	a := mcpx.New(h.cache, &config.Config{MCP: config.MCP{Enabled: true, MaxRisk: "high", RatePerMin: 1000}}, func(context.Context, string) (*capability.Actor, error) { return nil, errcode.Unauthorizedf("not PAT") }, nil)
	a.SetOAuth(h.s.Authenticate, h.s.ResourceMetadataURL())
	r.Any("/mcp", gin.WrapH(a.Handler()))
	return h
}
func (h *harness) do(t *testing.T, method, path, contentType, body string, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw, res.Header
}
func (h *harness) consentCode(t *testing.T, scope string) (clientID, code, verifier, redirect string) {
	t.Helper()
	redirect = "http://127.0.0.1:39293/callback"
	status, raw, _ := h.do(t, "POST", "/oauth/register", "application/json", fmt.Sprintf(`{"client_name":"Integration client","redirect_uris":[%q],"token_endpoint_auth_method":"none"}`, redirect), nil)
	if status != 201 {
		t.Fatalf("registration %d %s", status, raw)
	}
	var registration map[string]any
	_ = json.Unmarshal(raw, &registration)
	clientID = registration["client_id"].(string)
	verifier = strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {clientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "scope": {scope}, "state": {"client-state"}, "resource": {h.s.Resource()}}
	status, raw, headers := h.do(t, "GET", "/oauth/authorize?"+q.Encode(), "", "", nil)
	if status != 302 {
		t.Fatalf("authorize %d %s", status, raw)
	}
	u, _ := url.Parse(headers.Get("Location"))
	id := u.Query().Get("request_id")
	status, raw, _ = h.do(t, "GET", "/api/oauth/consent?request_id="+id, "", "", nil)
	if status != 200 {
		t.Fatalf("consent %d %s", status, raw)
	}
	status, raw, _ = h.do(t, "POST", "/api/oauth/consent", "application/json", `{"requestId":"`+id+`","approve":true}`, nil)
	if status != 200 {
		t.Fatalf("approve %d %s", status, raw)
	}
	var response struct {
		Data struct {
			RedirectURI string `json:"redirectUri"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &response)
	u, _ = url.Parse(response.Data.RedirectURI)
	if u.Query().Get("state") != "client-state" || u.Query().Get("iss") != h.server.URL {
		t.Fatalf("callback binding %s", response.Data.RedirectURI)
	}
	code = u.Query().Get("code")
	if code == "" {
		t.Fatalf("missing code %s", raw)
	}
	return
}
func (h *harness) exchangeCode(t *testing.T, id, code, verifier, redirect string) *TokenResponse {
	t.Helper()
	values := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirect}, "resource": {h.s.Resource()}}
	status, raw, _ := h.do(t, "POST", "/oauth/token", "application/x-www-form-urlencoded", values.Encode(), nil)
	if status != 200 {
		t.Fatalf("exchange %d %s", status, raw)
	}
	out := new(TokenResponse)
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestOAuthPKCERotationRevocationAndLiveGrants(t *testing.T) {
	h := setupOAuth(t)
	id, code, verifier, redirect := h.consentCode(t, "mcp:read mcp:write offline_access")
	wrong := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "code": {code}, "code_verifier": {strings.Repeat("x", 43)}, "redirect_uri": {redirect}}
	if status, raw, _ := h.do(t, "POST", "/oauth/token", "application/x-www-form-urlencoded", wrong.Encode(), nil); status != 400 || !bytes.Contains(raw, []byte("invalid_grant")) {
		t.Fatalf("PKCE accepted %d %s", status, raw)
	}
	issued := h.exchangeCode(t, id, code, verifier, redirect)
	if issued.RefreshToken == "" {
		t.Fatal("missing refresh token")
	}
	actor, err := h.s.Authenticate(context.Background(), issued.AccessToken)
	if err != nil || !actor.Can("test:write") {
		t.Fatalf("access %v %v", actor, err)
	}
	h.liveWrite.Store(false)
	actor, err = h.s.Authenticate(context.Background(), issued.AccessToken)
	if err != nil || actor.Can("test:write") {
		t.Fatalf("stale role permissions %v %v", actor, err)
	}
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {issued.RefreshToken}, "resource": {h.s.Resource()}}
	status, raw, _ := h.do(t, "POST", "/oauth/token", "application/x-www-form-urlencoded", refresh.Encode(), nil)
	if status != 200 {
		t.Fatalf("refresh %d %s", status, raw)
	}
	var rotated TokenResponse
	_ = json.Unmarshal(raw, &rotated)
	if _, err := h.s.Authenticate(context.Background(), issued.AccessToken); err == nil {
		t.Fatal("rotated access remains valid")
	}
	if _, err := h.s.Authenticate(context.Background(), rotated.AccessToken); err != nil {
		t.Fatal(err)
	}
	status, _, _ = h.do(t, "POST", "/oauth/token", "application/x-www-form-urlencoded", refresh.Encode(), nil)
	if status != 400 {
		t.Fatal("refresh replay accepted")
	}
	if _, err := h.s.Authenticate(context.Background(), rotated.AccessToken); err == nil {
		t.Fatal("refresh replay did not revoke family")
	}
	id, code, verifier, redirect = h.consentCode(t, "mcp:read offline_access")
	issued = h.exchangeCode(t, id, code, verifier, redirect)
	rev := url.Values{"client_id": {id}, "token": {issued.RefreshToken}}
	status, raw, _ = h.do(t, "POST", "/oauth/revoke", "application/x-www-form-urlencoded", rev.Encode(), nil)
	if status != 200 {
		t.Fatalf("revoke %d %s", status, raw)
	}
	if _, err := h.s.Authenticate(context.Background(), issued.AccessToken); err == nil {
		t.Fatal("revoked access accepted")
	}
	id, code, verifier, redirect = h.consentCode(t, "mcp:read")
	issued = h.exchangeCode(t, id, code, verifier, redirect)
	h.disabled.Store(true)
	if _, err := h.s.Authenticate(context.Background(), issued.AccessToken); err == nil {
		t.Fatal("disabled account accepted")
	}
}
func TestOAuthCodeReuseExpiryAndSDKHTTP(t *testing.T) {
	h := setupOAuth(t)
	id, code, verifier, redirect := h.consentCode(t, "mcp:read")
	issued := h.exchangeCode(t, id, code, verifier, redirect)
	if actor, err := h.s.Authenticate(context.Background(), issued.AccessToken); err != nil || !actor.ReadOnly {
		t.Fatalf("read scope lost ceiling: %v %v", actor, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "OAuth integration", Version: "1"}, nil)
	httpClient := &http.Client{Transport: tokenTransport{issued.AccessToken, http.DefaultTransport}}
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: h.server.URL + "/mcp", HTTPClient: httpClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "invoke_capability", Arguments: map[string]any{"name": h.readName, "args": map[string]any{}}})
	if err != nil || res.IsError {
		t.Fatalf("SDK call %v %v", res, err)
	}
	resource, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "context://me"})
	if err != nil || len(resource.Contents) != 1 {
		t.Fatalf("SDK resource %v %v", resource, err)
	}
	values := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirect}}
	if status, raw, _ := h.do(t, "POST", "/oauth/token", "application/x-www-form-urlencoded", values.Encode(), nil); status != 400 {
		t.Fatalf("code replay %d %s", status, raw)
	}
	if _, err := h.s.Authenticate(ctx, issued.AccessToken); err == nil {
		t.Fatal("code replay did not revoke original authorization")
	}
	id, code, verifier, redirect = h.consentCode(t, "mcp:read")
	h.s.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	values = url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirect}}
	if status, _, _ := h.do(t, "POST", "/oauth/token", "application/x-www-form-urlencoded", values.Encode(), nil); status != 400 {
		t.Fatal("expired code accepted")
	}
}

type tokenTransport struct {
	value string
	base  http.RoundTripper
}

func TestOfficialSDKDiscoversRegistersAndAuthorizes(t *testing.T) {
	h := setupOAuth(t)
	consented := false
	handler, err := mcpauth.NewAuthorizationCodeHandler(&mcpauth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &mcpauth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{ClientName: "Official SDK discovery", RedirectURIs: []string{"http://127.0.0.1:39399/callback"}, TokenEndpointAuthMethod: "none", GrantTypes: []string{"authorization_code", "refresh_token"}}},
		Client:                          h.client, RequestRefreshToken: true, ScopeFilter: func([]string) []string { return []string{"mcp:read"} },
		AuthorizationCodeFetcher: func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
			u, err := url.Parse(args.URL)
			if err != nil {
				return nil, err
			}
			if u.Query().Get("code_challenge_method") != "S256" {
				return nil, fmt.Errorf("SDK did not use S256")
			}
			status, raw, headers := h.do(t, "GET", u.RequestURI(), "", "", nil)
			if status != 302 {
				return nil, fmt.Errorf("authorize %d %s", status, raw)
			}
			location, _ := url.Parse(headers.Get("Location"))
			id := location.Query().Get("request_id")
			// A browser user explicitly reads and approves the pending request.
			status, raw, _ = h.do(t, "GET", "/api/oauth/consent?request_id="+id, "", "", nil)
			if status != 200 {
				return nil, fmt.Errorf("consent %d %s", status, raw)
			}
			status, raw, _ = h.do(t, "POST", "/api/oauth/consent", "application/json", `{"requestId":"`+id+`","approve":true}`, nil)
			if status != 200 {
				return nil, fmt.Errorf("approval %d %s", status, raw)
			}
			var response struct {
				Data struct {
					RedirectURI string `json:"redirectUri"`
				} `json:"data"`
			}
			if err := json.Unmarshal(raw, &response); err != nil {
				return nil, err
			}
			callback, err := url.Parse(response.Data.RedirectURI)
			if err != nil {
				return nil, err
			}
			consented = true
			return &mcpauth.AuthorizationResult{Code: callback.Query().Get("code"), State: callback.Query().Get("state"), Iss: callback.Query().Get("iss")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "Official SDK OAuth discovery", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: h.server.URL + "/mcp", OAuthHandler: handler}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if !consented {
		t.Fatal("SDK connection skipped explicit consent")
	}
	resource, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "capability://" + h.readName})
	if err != nil || len(resource.Contents) != 1 {
		t.Fatalf("OAuth resource %v %v", resource, err)
	}
	ts, err := handler.TokenSource(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := ts.Token()
	if err != nil || tok.RefreshToken == "" {
		t.Fatalf("SDK refresh token %v %v", tok, err)
	}
}

func TestConsentOwnerDenialAndReplay(t *testing.T) {
	h := setupOAuth(t)
	cl := client{ClientID: uuid.NewString(), ClientName: "Consent fixture", RedirectURIs: encoded([]string{"https://client.example/cb"}), CreateTime: time.Now()}
	if err := h.s.db.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	owner := int64(1)
	r := request{RequestID: uuid.NewString(), ClientID: cl.ClientID, RedirectURI: "https://client.example/cb", State: "owned-state", Challenge: strings.Repeat("a", 43), Scopes: encoded([]string{"mcp:read"}), Resource: h.s.Resource(), UserID: &owner, Status: "pending", ExpiresAt: time.Now().Add(time.Minute), CreateTime: time.Now()}
	if err := h.s.db.Create(&r).Error; err != nil {
		t.Fatal(err)
	}
	if status, raw, _ := h.do(t, "GET", "/api/oauth/consent?request_id="+r.RequestID, "", "", map[string]string{"Authorization": "Bearer other-user"}); status != 403 {
		t.Fatalf("other owner %d %s", status, raw)
	}
	status, raw, _ := h.do(t, "POST", "/api/oauth/consent", "application/json", `{"requestId":"`+r.RequestID+`","approve":false}`, nil)
	if status != 200 || !bytes.Contains(raw, []byte("access_denied")) {
		t.Fatalf("denial %d %s", status, raw)
	}
	if _, raw, _ := h.do(t, "POST", "/api/oauth/consent", "application/json", `{"requestId":"`+r.RequestID+`","approve":true}`, nil); !bytes.Contains(raw, []byte(`"code":400`)) {
		t.Fatalf("consent decision replay accepted: %s", raw)
	}
	var count int64
	h.s.db.Model(&grant{}).Count(&count)
	if count != 0 {
		t.Fatal("denied consent minted a grant")
	}
}

func (t tokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header = r.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+t.value)
	return t.base.RoundTrip(copy)
}

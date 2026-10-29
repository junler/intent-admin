package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/response"
)

type oauthError struct {
	Code        string
	Description string
}

func (e *oauthError) Error() string                { return e.Description }
func protocolError(code, description string) error { return &oauthError{code, description} }
func fail(c *gin.Context, err error) {
	var e *oauthError
	if errors.As(err, &e) {
		c.JSON(http.StatusBadRequest, gin.H{"error": e.Code, "error_description": e.Description})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error", "error_description": "授权服务暂时不可用"})
}
func (s *Service) MountPublic(r *gin.Engine) {
	metadata := func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Cache-Control", "public, max-age=300")
		c.JSON(200, gin.H{"resource": s.Resource(), "authorization_servers": []string{s.opts.Issuer}, "scopes_supported": []string{"mcp:read", "mcp:write", "offline_access"}, "bearer_methods_supported": []string{"header"}, "resource_name": "Intent MCP"})
	}
	r.GET("/.well-known/oauth-protected-resource", metadata)
	r.GET("/.well-known/oauth-protected-resource"+s.opts.ResourcePath, metadata)
	r.GET("/.well-known/oauth-authorization-server", func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Cache-Control", "public, max-age=300")
		c.JSON(200, gin.H{"issuer": s.opts.Issuer, "authorization_endpoint": s.opts.Issuer + "/oauth/authorize", "token_endpoint": s.opts.Issuer + "/oauth/token", "registration_endpoint": s.opts.Issuer + "/oauth/register", "revocation_endpoint": s.opts.Issuer + "/oauth/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}, "revocation_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": []string{"mcp:read", "mcp:write", "offline_access"}, "authorization_response_iss_parameter_supported": true})
	})
	g := r.Group("/oauth")
	g.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Pragma", "no-cache")
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})
	g.OPTIONS("/:endpoint", func(c *gin.Context) { c.Status(204) })
	g.POST("/register", s.limit("registration", 10), s.register)
	g.GET("/authorize", s.limit("authorization", 30), s.authorize)
	g.POST("/token", s.limit("token", 60), s.token)
	g.POST("/revoke", s.limit("revoke", 60), s.revoke)
}
func (s *Service) MountProtected(g *gin.RouterGroup) {
	g.GET("/oauth/consent", s.consent)
	g.POST("/oauth/consent", s.decide)
	g.GET("/oauth/grants", s.grants)
	g.DELETE("/oauth/grants/:id", s.revokeGrant)
}

var limitScript = redis.NewScript(`local n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('EXPIRE',KEYS[1],60) end;return n`)

func (s *Service) limit(name string, max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.cache == nil || s.cache.Client == nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "temporarily_unavailable"})
			return
		}
		n, err := limitScript.Run(c.Request.Context(), s.cache.Client, []string{s.cache.Key("oauth:rate", name, digest(c.ClientIP()))}).Int64()
		if err != nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "temporarily_unavailable"})
			return
		}
		if n > max {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(429, gin.H{"error": "slow_down"})
			return
		}
		c.Next()
	}
}
func (s *Service) register(c *gin.Context) {
	var in struct {
		ClientName    string   `json:"client_name"`
		RedirectURIs  []string `json:"redirect_uris"`
		AuthMethod    string   `json:"token_endpoint_auth_method"`
		GrantTypes    []string `json:"grant_types"`
		ResponseTypes []string `json:"response_types"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, protocolError("invalid_client_metadata", "客户端注册数据无效"))
		return
	}
	if in.AuthMethod != "" && in.AuthMethod != "none" || len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > 10 {
		fail(c, protocolError("invalid_client_metadata", "仅支持公开客户端和 1–10 个精确回调地址"))
		return
	}
	for _, v := range in.RedirectURIs {
		if _, err := validRedirect(v); err != nil {
			fail(c, protocolError("invalid_redirect_uri", "回调地址必须使用 HTTPS 或数字回环 HTTP，且不能包含凭据和片段"))
			return
		}
	}
	for _, v := range in.GrantTypes {
		if v != "authorization_code" && v != "refresh_token" {
			fail(c, protocolError("invalid_client_metadata", "不支持的授权类型"))
			return
		}
	}
	for _, v := range in.ResponseTypes {
		if v != "code" {
			fail(c, protocolError("invalid_client_metadata", "仅支持 code 响应类型"))
			return
		}
	}
	in.ClientName = strings.TrimSpace(in.ClientName)
	if in.ClientName == "" {
		in.ClientName = "MCP 客户端"
	}
	if len([]rune(in.ClientName)) > 100 {
		fail(c, protocolError("invalid_client_metadata", "客户端名称过长"))
		return
	}
	row := client{ClientID: uuid.NewString(), ClientName: in.ClientName, RedirectURIs: encoded(in.RedirectURIs), CreateTime: s.now()}
	if err := dbx.DBFrom(c.Request.Context(), s.db).Create(&row).Error; err != nil {
		fail(c, err)
		return
	}
	c.JSON(201, gin.H{"client_id": row.ClientID, "client_id_issued_at": row.CreateTime.Unix(), "client_name": row.ClientName, "redirect_uris": in.RedirectURIs, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}})
}
func (s *Service) authorize(c *gin.Context) {
	q := c.Request.URL.Query()
	for _, values := range q {
		if len(values) != 1 {
			fail(c, protocolError("invalid_request", "授权参数不能重复"))
			return
		}
	}
	var cl client
	if err := dbx.DBFrom(c.Request.Context(), s.db).Where("client_id=?", q.Get("client_id")).First(&cl).Error; err != nil {
		fail(c, protocolError("invalid_client", "客户端不存在"))
		return
	}
	redirect := q.Get("redirect_uri")
	if !slices.ContainsFunc(stringList(cl.RedirectURIs), func(r string) bool { return redirectMatches(r, redirect) }) {
		fail(c, protocolError("invalid_request", "回调地址与注册信息不一致"))
		return
	}
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || !challengePattern.MatchString(q.Get("code_challenge")) || len(q.Get("state")) > 1024 {
		fail(c, protocolError("invalid_request", "必须使用授权码和 PKCE S256"))
		return
	}
	list, err := parseScopes(q.Get("scope"))
	if err != nil {
		fail(c, err)
		return
	}
	if q.Get("resource") != "" && q.Get("resource") != s.Resource() {
		fail(c, protocolError("invalid_target", "请求的资源不受此授权服务保护"))
		return
	}
	r := request{RequestID: uuid.NewString(), ClientID: cl.ClientID, RedirectURI: redirect, State: q.Get("state"), Challenge: q.Get("code_challenge"), Scopes: encoded(list), Resource: s.Resource(), Status: "pending", ExpiresAt: s.now().Add(10 * time.Minute), CreateTime: s.now()}
	if err := dbx.DBFrom(c.Request.Context(), s.db).Create(&r).Error; err != nil {
		fail(c, err)
		return
	}
	c.Redirect(http.StatusFound, s.opts.ConsentPath+"?request_id="+url.QueryEscape(r.RequestID))
}
func form(c *gin.Context) (url.Values, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	if !strings.HasPrefix(c.ContentType(), "application/x-www-form-urlencoded") {
		return nil, protocolError("invalid_request", "请求必须使用表单编码")
	}
	if err := c.Request.ParseForm(); err != nil {
		return nil, protocolError("invalid_request", "表单无效")
	}
	for _, v := range c.Request.PostForm {
		if len(v) != 1 {
			return nil, protocolError("invalid_request", "表单参数不能重复")
		}
	}
	if _, err := uuid.Parse(c.Request.PostForm.Get("client_id")); err != nil {
		return nil, protocolError("invalid_client", "client_id 无效")
	}
	return c.Request.PostForm, nil
}
func (s *Service) token(c *gin.Context) {
	values, err := form(c)
	if err != nil {
		fail(c, err)
		return
	}
	out, err := s.exchange(c.Request.Context(), values)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, out)
}
func (s *Service) revoke(c *gin.Context) {
	values, err := form(c)
	if err != nil {
		fail(c, err)
		return
	}
	plain := values.Get("token")
	if len(plain) > 256 {
		c.Status(200)
		return
	}
	err = dbx.DBFrom(c.Request.Context(), s.db).Transaction(func(tx *gorm.DB) error {
		var t token
		if err := tx.Where("access_hash=? OR refresh_hash=?", digest(plain), digest(plain)).First(&t).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		return tx.Model(&grant{}).Where("grant_id=? AND client_id=?", t.GrantID, values.Get("client_id")).Update("revoked_at", s.now()).Error
	})
	if err != nil {
		fail(c, err)
		return
	}
	c.Status(200)
}
func human(ctx context.Context) (*capability.Actor, error) {
	a, ok := capability.ActorFrom(ctx)
	if !ok || a.Type != capability.ActorHuman || a.Source != capability.SourceHTTP {
		return nil, errcode.Unauthorizedf("请使用已登录的管理页面进行授权")
	}
	return a, nil
}
func (s *Service) pending(tx *gorm.DB, id string, userID int64) (*request, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, errcode.BadRequestf("授权请求无效")
	}
	var r request
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("request_id=?", id).First(&r).Error; err != nil {
		return nil, errcode.NotFoundf("授权请求不存在")
	}
	if r.Status != "pending" || !r.ExpiresAt.After(s.now()) {
		return nil, errcode.BadRequestf("授权请求已处理或过期，请重新发起")
	}
	if r.UserID != nil && *r.UserID != userID {
		return nil, errcode.Forbiddenf("授权请求不属于当前账号")
	}
	if r.UserID == nil {
		r.UserID = &userID
		if err := tx.Model(&r).Update("user_id", userID).Error; err != nil {
			return nil, err
		}
	}
	return &r, nil
}
func (s *Service) consent(c *gin.Context) {
	a, err := human(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	var out any
	err = dbx.DBFrom(c.Request.Context(), s.db).Transaction(func(tx *gorm.DB) error {
		r, err := s.pending(tx, c.Query("request_id"), a.UserID)
		if err != nil {
			return err
		}
		var cl client
		if err := tx.First(&cl, "client_id=?", r.ClientID).Error; err != nil {
			return err
		}
		allowed := []Scope{}
		for _, scope := range scopes {
			if slices.Contains(stringList(r.Scopes), scope.Name) {
				allowed = append(allowed, scope)
			}
		}
		out = gin.H{"requestId": r.RequestID, "clientName": cl.ClientName, "redirectUri": r.RedirectURI, "scopes": allowed, "expiresAt": r.ExpiresAt}
		return nil
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, out)
}
func (s *Service) decide(c *gin.Context) {
	a, err := human(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	var in struct {
		RequestID string   `json:"requestId"`
		Approve   bool     `json:"approve"`
		Scopes    []string `json:"scopes"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.BadRequestf("授权决定无效"))
		return
	}
	callback := ""
	auditSummary := ""
	err = dbx.DBFrom(c.Request.Context(), s.db).Transaction(func(tx *gorm.DB) error {
		r, err := s.pending(tx, in.RequestID, a.UserID)
		if err != nil {
			return err
		}
		auditSummary = fmt.Sprintf("clientId=%s requestId=%s approve=%t", r.ClientID, r.RequestID, in.Approve)
		u, _ := url.Parse(r.RedirectURI)
		q := u.Query()
		q.Set("state", r.State)
		q.Set("iss", s.opts.Issuer)
		if !in.Approve {
			q.Set("error", "access_denied")
			u.RawQuery = q.Encode()
			callback = u.String()
			return tx.Model(r).Update("status", "denied").Error
		}
		approved := in.Scopes
		if approved == nil {
			approved = stringList(r.Scopes)
		}
		for _, scope := range approved {
			if !slices.Contains(stringList(r.Scopes), scope) {
				return errcode.BadRequestf("不能扩大客户端请求的授权范围")
			}
		}
		if !slices.Contains(approved, "mcp:read") && !slices.Contains(approved, "mcp:write") {
			return errcode.BadRequestf("请至少选择一种 MCP 访问范围")
		}
		actor, err := s.resolve(c.Request.Context(), a.UserID, nil, capability.RiskHigh)
		if err != nil {
			return err
		}
		perms := permissions(actor, approved)
		if len(perms) == 0 {
			return errcode.Forbiddenf("当前账号没有可授予的 MCP 权限")
		}
		g := grant{ClientID: r.ClientID, UserID: a.UserID, Scopes: encoded(approved), Permissions: encoded(perms), Resource: r.Resource, ExpiresAt: s.now().Add(s.opts.RefreshTTL), CreateTime: s.now()}
		if err := tx.Create(&g).Error; err != nil {
			return err
		}
		code, err := secret("ioc_")
		if err != nil {
			return err
		}
		hash := digest(code)
		if err := tx.Model(r).Updates(map[string]any{"status": "approved", "code_hash": hash, "grant_id": g.GrantID, "expires_at": s.now().Add(90 * time.Second)}).Error; err != nil {
			return err
		}
		q.Set("code", code)
		u.RawQuery = q.Encode()
		callback = u.String()
		return nil
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	s.audit(c.Request.Context(), a, "oauth.consent", auditSummary)
	c.Header("Cache-Control", "no-store")
	response.OK(c, gin.H{"redirectUri": callback})
}
func (s *Service) grants(c *gin.Context) {
	a, err := human(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	var rows []struct {
		GrantID    int64           `json:"grantId"`
		ClientName string          `json:"clientName"`
		Scopes     json.RawMessage `json:"scopes"`
		CreateTime time.Time       `json:"createTime"`
		ExpiresAt  time.Time       `json:"expiresAt"`
		RevokedAt  *time.Time      `json:"revokedAt"`
	}
	err = dbx.DBFrom(c.Request.Context(), s.db).Table("sys_oauth_grant g").Select("g.grant_id,c.client_name,g.scopes,g.create_time,g.expires_at,g.revoked_at").Joins("JOIN sys_oauth_client c ON c.client_id=g.client_id").Where("g.user_id=?", a.UserID).Order("g.create_time DESC").Limit(200).Scan(&rows).Error
	if err != nil {
		response.Fail(c, err)
		return
	}
	if rows == nil {
		response.OK(c, []any{})
		return
	}
	response.OK(c, rows)
}
func (s *Service) revokeGrant(c *gin.Context) {
	a, err := human(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, errcode.BadRequestf("授权编号无效"))
		return
	}
	result := dbx.DBFrom(c.Request.Context(), s.db).Model(&grant{}).Where("grant_id=? AND user_id=?", id, a.UserID).Update("revoked_at", s.now())
	if result.Error != nil {
		response.Fail(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		response.Fail(c, errcode.NotFoundf("授权不存在"))
		return
	}
	s.audit(c.Request.Context(), a, "oauth.revoke", fmt.Sprintf("grantId=%d", id))
	response.OK(c, nil)
}
func (s *Service) audit(ctx context.Context, a *capability.Actor, name, summary string) {
	if s.Audit != nil {
		s.Audit(ctx, capability.AuditRecord{Capability: name, Title: "MCP OAuth 授权", BizType: capability.BizGrant, Risk: capability.RiskMedium, ActorType: a.Type, Source: a.Source, UserID: a.UserID, UserName: a.UserName, IP: a.IP, UserAgent: a.UserAgent, ApprovalMode: "manual", Param: summary, Success: true, OccurredAt: s.now()})
	}
}

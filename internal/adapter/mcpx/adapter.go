// Package mcpx projects the capability registry into the official MCP SDK.
// Every invocation revalidates its bearer grant and uses the same executor.
package mcpx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/redis/go-redis/v9"

	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/knowledge"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type Authenticator func(context.Context, string) (*capability.Actor, error)
type Knowledge interface {
	Get(context.Context, string) (knowledge.Knowledge, error)
}
type Adapter struct {
	cache              *cache.Cache
	cfg                *config.Config
	authenticate       Authenticator
	oauthAuthenticate  Authenticator
	resourceMetadata   string
	knowledge          Knowledge
	confirmationRunner func(context.Context, func(context.Context) error) error
}
type httpCheckedKey struct{}

func New(c *cache.Cache, cfg *config.Config, authenticator Authenticator, k Knowledge) *Adapter {
	registerResourceCapabilities()
	return &Adapter{cache: c, cfg: cfg, authenticate: authenticator, knowledge: k}
}

// SetOAuth enables opaque OAuth access tokens alongside existing PATs.
func (a *Adapter) SetOAuth(authenticator Authenticator, metadataURL string) {
	a.oauthAuthenticate, a.resourceMetadata = authenticator, metadataURL
}
func (a *Adapter) authenticateToken(ctx context.Context, token string) (*capability.Actor, error) {
	if strings.HasPrefix(token, "iao_") && a.oauthAuthenticate != nil {
		return a.oauthAuthenticate(ctx, token)
	}
	return a.authenticate(ctx, token)
}

// SetConfirmationRunner installs an outer database transaction and business
// locks covering the fresh preview and write. The runner must include predicate
// locks (or table locks) so inserts cannot change filtered affected sets.
// Without this guard confirmations fail closed.
func (a *Adapter) SetConfirmationRunner(run func(context.Context, func(context.Context) error) error) {
	a.confirmationRunner = run
}

// Handler uses stateless Streamable HTTP so credentials and visible tools are
// recomputed on every request. A cached protocol session cannot outlive a PAT.
func (a *Adapter) Handler() http.Handler {
	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		token := bearer(r.Header.Get("Authorization"))
		actor, _ := capability.ActorFrom(r.Context())
		return a.Server(actor, token)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 128 << 10})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.cfg.MCP.Enabled {
			http.NotFound(w, r)
			return
		}
		token := bearer(r.Header.Get("Authorization"))
		actor, err := a.authenticateToken(r.Context(), token)
		if err != nil {
			challenge := "Bearer"
			if a.resourceMetadata != "" {
				challenge += " resource_metadata=" + strconv.Quote(a.resourceMetadata) + ", error=\"invalid_token\""
			}
			w.Header().Set("WWW-Authenticate", challenge)
			http.Error(w, "无效或已过期的访问令牌", http.StatusUnauthorized)
			return
		}
		if err = a.rate(r.Context(), actor); err != nil {
			code := http.StatusServiceUnavailable
			if errcode.From(err).Code == 429 {
				code = 429
				w.Header().Set("Retry-After", "60")
			}
			http.Error(w, errcode.From(err).Msg, code)
			return
		}
		copy := *actor
		copy.IP = r.RemoteAddr
		copy.UserAgent = r.UserAgent()
		ctx := context.WithValue(capability.WithActor(r.Context(), &copy), httpCheckedKey{}, true)
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
func bearer(header string) string {
	parts := strings.Fields(header)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func (a *Adapter) ServeStdio(ctx context.Context, token string) error {
	if !a.cfg.MCP.Enabled {
		return errcode.Forbiddenf("MCP 已禁用")
	}
	if token == "" {
		return errcode.Unauthorizedf("stdio MCP 需要 INTENT_ADMIN_MCP_TOKEN 个人访问令牌")
	}
	actor, err := a.authenticate(ctx, token)
	if err != nil {
		return err
	}
	return a.Server(actor, token).Run(ctx, &mcp.StdioTransport{})
}

var rateScript = redis.NewScript(`local n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('PEXPIRE',KEYS[1],60000) end;return n`)

func (a *Adapter) rate(ctx context.Context, actor *capability.Actor) error {
	limit := a.cfg.MCP.RatePerMin
	if limit <= 0 {
		limit = 60
	}
	n, err := rateScript.Run(ctx, a.cache.Client, []string{a.cache.Key("ratelimit", "mcp", strconv.FormatInt(actor.TokenID, 10))}).Int()
	if err != nil {
		return err
	}
	if n > limit {
		return errcode.New(errcode.TooManyReq, "访问令牌调用频率超限")
	}
	return nil
}

func (a *Adapter) entry(ctx context.Context, name string) (*capability.Entry, *capability.Actor, error) {
	actor, ok := capability.ActorFrom(ctx)
	if !ok || actor.Source != capability.SourceMCP {
		return nil, nil, errcode.Unauthorizedf("缺少 MCP 主体")
	}
	e, ok := capability.Lookup(name)
	if !ok || !e.VisibleTo(capability.SourceMCP, actor.Perms, actor.MaxRisk) || !a.tagged(e) {
		return nil, nil, errcode.NotFoundf("未找到可用功能 %s", name)
	}
	if actor.ReadOnly && e.Mutating {
		return nil, nil, errcode.NotFoundf("未找到可用功能 %s", name)
	}
	return e, actor, nil
}
func (a *Adapter) tagged(e *capability.Entry) bool {
	if strings.HasPrefix(e.Name, "mcp.resource.") {
		return false
	}
	// Critical operations always require a human in the management UI. A
	// deployment or PAT ceiling cannot turn that boundary into a confirmation.
	if e.Risk == capability.RiskCritical {
		return false
	}
	if len(a.cfg.MCP.ToolTags) == 0 {
		return true
	}
	for _, tag := range e.Tags {
		if slices.Contains(a.cfg.MCP.ToolTags, tag) {
			return true
		}
	}
	return false
}

type invocation struct {
	Name   string          `json:"name"`
	Args   json.RawMessage `json:"args"`
	Intent string          `json:"intent,omitempty"`
}
type confirmation struct {
	Name        string          `json:"name"`
	Args        json.RawMessage `json:"args"`
	Hash        string          `json:"hash"`
	PreviewHash string          `json:"previewHash"`
	UserID      int64           `json:"userId"`
	TokenID     int64           `json:"tokenId"`
	Intent      string          `json:"intent"`
}
type writePreview struct {
	Preview      *capability.Preview `json:"preview"`
	ConfirmToken string              `json:"confirmToken"`
	ExpiresIn    int64               `json:"expiresIn"`
	Message      string              `json:"message"`
}

func hashJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var canonical any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err = dec.Decode(&canonical); err != nil {
		return "", err
	}
	raw, err = json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Invoke returns a preview for writes. The caller cannot opt out of the second
// confirmation step, even if its local MCP client automatically approves tools.
func (a *Adapter) Invoke(ctx context.Context, in invocation) (any, error) {
	e, actor, err := a.entry(ctx, in.Name)
	if err != nil {
		return nil, err
	}
	copy := *actor
	copy.Intent = in.Intent
	ctx = capability.WithActor(ctx, &copy)
	if len(in.Args) == 0 {
		in.Args = json.RawMessage(`{}`)
	}
	if !e.Mutating {
		return capability.InvokeJSON(ctx, in.Name, in.Args)
	}
	preview, err := capability.RunPreview(ctx, in.Name, in.Args)
	if err != nil {
		return nil, err
	}
	if preview.Truncated {
		return nil, errcode.BadRequestf("预演超出可逐项确认的上限，请缩小操作范围")
	}
	argHash, err := hashJSON(in.Args)
	if err != nil {
		return nil, err
	}
	previewHash, err := hashPreview(preview)
	if err != nil {
		return nil, err
	}
	record := confirmation{Name: in.Name, Args: in.Args, Hash: argHash, PreviewHash: previewHash, UserID: actor.UserID, TokenID: actor.TokenID, Intent: in.Intent}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	token := uuid.NewString()
	ttl := a.cfg.MCP.ConfirmTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if err = a.cache.Set(ctx, a.cache.Key("mcp:confirm", token), raw, ttl).Err(); err != nil {
		return nil, err
	}
	return &writePreview{Preview: preview, ConfirmToken: token, ExpiresIn: int64(ttl.Seconds()), Message: "尚未执行，请确认预览后调用 confirm_plan"}, nil
}

var consumeScript = redis.NewScript(`local v=redis.call('GET',KEYS[1]);if v then redis.call('DEL',KEYS[1]) end;return v`)

type confirmInput struct {
	ConfirmToken string          `json:"confirmToken"`
	Name         string          `json:"name,omitempty"`
	Args         json.RawMessage `json:"args,omitempty"`
}

func (a *Adapter) Confirm(ctx context.Context, in confirmInput) (any, error) {
	if _, err := uuid.Parse(in.ConfirmToken); err != nil {
		return nil, errcode.BadRequestf("确认令牌无效")
	}
	raw, err := consumeScript.Run(ctx, a.cache.Client, []string{a.cache.Key("mcp:confirm", in.ConfirmToken)}).Text()
	if errors.Is(err, redis.Nil) {
		return nil, errcode.BadRequestf("确认令牌已过期或已使用")
	}
	if err != nil {
		return nil, err
	}
	var stored confirmation
	if err = json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, err
	}
	e, actor, err := a.entry(ctx, stored.Name)
	if err != nil {
		return nil, err
	}
	if actor.UserID != stored.UserID || actor.TokenID != stored.TokenID {
		return nil, errcode.Forbiddenf("确认令牌不属于当前主体")
	}
	if in.Name != "" && in.Name != stored.Name {
		return nil, errcode.BadRequestf("确认功能与预览不一致")
	}
	if len(in.Args) > 0 {
		hash, err := hashJSON(in.Args)
		if err != nil || hash != stored.Hash {
			return nil, errcode.BadRequestf("确认参数与预览不一致")
		}
	}
	if !e.Mutating {
		return nil, errcode.BadRequestf("该确认令牌不是写操作")
	}
	copy := *actor
	copy.Intent = stored.Intent
	copy.ApprovalMode = "confirmed"
	if e.Risk.Level() >= capability.RiskHigh.Level() {
		copy.ApprovalMode = "diff_confirmed"
	}
	ctx = capability.WithActor(ctx, &copy)
	if a.confirmationRunner == nil {
		return nil, errcode.Internalf("MCP 写操作缺少事务确认保护")
	}
	var out any
	err = a.confirmationRunner(ctx, func(locked context.Context) error {
		preview, err := capability.RunPreview(locked, stored.Name, stored.Args)
		if err != nil {
			return err
		}
		if preview.Truncated {
			return errcode.BadRequestf("预演超出可逐项确认的上限，请缩小操作范围")
		}
		hash, err := hashPreview(preview)
		if err != nil {
			return err
		}
		if hash != stored.PreviewHash {
			return errcode.StaleDataf("数据已变化，请重新调用功能并确认最新预览")
		}
		out, err = capability.InvokeJSON(locked, stored.Name, stored.Args)
		return err
	})
	return out, err
}

func result(out any, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		e := errcode.From(err)
		data := map[string]any{"code": e.Code, "message": e.Msg, "detail": e.Detail}
		raw, _ := json.Marshal(data)
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}, StructuredContent: data}, nil
	}
	clean := capability.SanitizeValue(out)
	// This short-lived adapter-issued confirmation is a transport credential
	// that must be returned once. Business outputs still use full redaction.
	if preview, ok := out.(*writePreview); ok {
		if mapped, ok := clean.(map[string]any); ok {
			mapped["confirmToken"] = preview.ConfirmToken
		}
	}
	raw, e := json.Marshal(clean)
	if e != nil {
		return nil, e
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}, StructuredContent: clean}, nil
}
func boolPtr(v bool) *bool { return &v }
func object(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

// Server is exported for in-memory transport tests and local integrations.
// Handlers never trust its captured catalogue actor for execution: the grant is
// loaded again on each tool call, so revocation and grant changes take effect.
func (a *Adapter) Server(catalogue *capability.Actor, token string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "intent-admin", Version: "1.0.0"}, nil)
	a.mountResources(srv, token)
	add := func(t *mcp.Tool, fn func(context.Context, json.RawMessage) (any, error)) {
		srv.AddTool(t, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			previous, _ := capability.ActorFrom(ctx)
			actor, err := a.authenticateToken(ctx, token)
			if err != nil {
				return result(nil, errcode.Unauthorizedf("令牌已失效"))
			}
			if previous != nil {
				actor.IP = previous.IP
				actor.UserAgent = previous.UserAgent
			}
			if checked, _ := ctx.Value(httpCheckedKey{}).(bool); !checked {
				if err = a.rate(ctx, actor); err != nil {
					return result(nil, err)
				}
			}
			ctx = capability.WithActor(ctx, actor)
			raw := req.Params.Arguments
			if len(raw) == 0 {
				raw = json.RawMessage(`{}`)
			}
			out, err := fn(ctx, raw)
			return result(out, err)
		})
	}
	readHint := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
	if a.cfg.MCP.ToolMode == "flat" {
		if catalogue != nil {
			for _, e := range capability.Visible(capability.SourceMCP, catalogue.Perms, catalogue.MaxRisk) {
				if !a.tagged(e) || catalogue.ReadOnly && e.Mutating {
					continue
				}
				entry := e
				add(&mcp.Tool{Name: strings.ReplaceAll(e.Name, ".", "_"), Description: e.Summary, InputSchema: e.InputSchema, Annotations: &mcp.ToolAnnotations{Title: e.Summary, ReadOnlyHint: !e.Mutating, DestructiveHint: boolPtr(e.Risk.Level() >= capability.RiskHigh.Level()), IdempotentHint: e.Idempotent, OpenWorldHint: boolPtr(false)}}, func(ctx context.Context, args json.RawMessage) (any, error) {
					return a.Invoke(ctx, invocation{Name: entry.Name, Args: args})
				})
			}
		}
	} else {
		add(&mcp.Tool{Name: "list_capabilities", Description: "搜索当前令牌可以使用的后台功能", InputSchema: object(map[string]any{"keyword": str("关键词，留空列出功能")}), Annotations: readHint}, func(ctx context.Context, raw json.RawMessage) (any, error) {
			var in struct {
				Keyword string `json:"keyword"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, errcode.BadRequestf("参数格式错误")
			}
			actor, _ := capability.ActorFrom(ctx)
			out := []capability.Descriptor{}
			for _, e := range capability.Search(capability.SourceMCP, actor.Perms, actor.MaxRisk, in.Keyword, 100) {
				if a.tagged(e) && !(actor.ReadOnly && e.Mutating) {
					out = append(out, e.Brief())
				}
				if len(out) >= 30 {
					break
				}
			}
			return out, nil
		})
		add(&mcp.Tool{Name: "describe_capability", Description: "读取功能完整入参和示例", InputSchema: object(map[string]any{"name": str("功能名称")}, "name"), Annotations: readHint}, func(ctx context.Context, raw json.RawMessage) (any, error) {
			var in struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, errcode.BadRequestf("参数格式错误")
			}
			e, _, err := a.entry(ctx, in.Name)
			if err != nil {
				return nil, err
			}
			return e.Describe(), nil
		})
		add(&mcp.Tool{Name: "get_knowledge", Description: "查询字段字典、实体关系和系统规则", InputSchema: object(map[string]any{"topic": str("overview、rules、表名或字典类型")}, "topic"), Annotations: readHint}, func(ctx context.Context, raw json.RawMessage) (any, error) {
			var in struct {
				Topic string `json:"topic"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, errcode.BadRequestf("参数格式错误")
			}
			if a.knowledge == nil {
				return nil, errcode.Internalf("知识库不可用")
			}
			return a.knowledge.Get(ctx, in.Topic)
		})
		add(&mcp.Tool{Name: "invoke_capability", Description: "只读直接执行；写操作只返回预览与一次性确认令牌，确认后调用 confirm_plan", InputSchema: object(map[string]any{"name": str("功能名称"), "args": map[string]any{"type": "object"}, "intent": str("原始用户意图")}, "name", "args"), Annotations: &mcp.ToolAnnotations{OpenWorldHint: boolPtr(false)}}, func(ctx context.Context, raw json.RawMessage) (any, error) {
			var in invocation
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, errcode.BadRequestf("参数格式错误")
			}
			return a.Invoke(ctx, in)
		})
	}
	add(&mcp.Tool{Name: "confirm_plan", Description: "消费一次性令牌并执行已预览的写操作。可传原 name/args 验证绑定，参数或当前数据改变即拒绝", InputSchema: object(map[string]any{"confirmToken": str("先前返回的一次性令牌"), "name": str("可选，原功能名称"), "args": map[string]any{"type": "object"}}, "confirmToken"), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)}}, func(ctx context.Context, raw json.RawMessage) (any, error) {
		var in confirmInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, errcode.BadRequestf("参数格式错误")
		}
		return a.Confirm(ctx, in)
	})
	return srv
}

// Preview rows and changed-field lists are sets for confirmation purposes;
// database/map iteration order must not invalidate an unchanged proposal.
func hashPreview(preview *capability.Preview) (string, error) {
	p := *preview
	p.Changes = append([]capability.Change(nil), preview.Changes...)
	p.Warnings = append([]string(nil), preview.Warnings...)
	sort.Strings(p.Warnings)
	for i := range p.Changes {
		p.Changes[i].Fields = append([]capability.FieldDiff(nil), p.Changes[i].Fields...)
		sort.SliceStable(p.Changes[i].Fields, func(a, b int) bool { return p.Changes[i].Fields[a].Field < p.Changes[i].Fields[b].Field })
	}
	sort.SliceStable(p.Changes, func(i, j int) bool {
		a, b := p.Changes[i], p.Changes[j]
		if a.Entity != b.Entity {
			return a.Entity < b.Entity
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Op < b.Op
	})
	return hashJSON(&p)
}

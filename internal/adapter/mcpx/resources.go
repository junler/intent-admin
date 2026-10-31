package mcpx

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/knowledge"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type resourceInput struct {
	URI string `json:"uri,omitempty"`
}
type resourceActionKey struct{}
type resourceAction func(context.Context, resourceInput) (any, error)

var registerResources sync.Once

func registerResourceCapabilities() {
	registerResources.Do(func() {
		for _, op := range []string{"list", "read"} {
			capability.Register(capability.Capability[resourceInput, any]{Meta: capability.Meta{Name: "mcp.resource." + op, Summary: "读取 MCP 授权资源", Risk: capability.RiskLow, MCPEnabled: true, LogTitle: "MCP 资源", BizType: capability.BizOther}, Fn: func(ctx context.Context, in resourceInput) (any, error) {
				fn, ok := ctx.Value(resourceActionKey{}).(resourceAction)
				if !ok {
					return nil, errcode.Forbiddenf("资源只能通过 MCP 资源协议读取")
				}
				return fn(ctx, in)
			}})
		}
	})
}
func resourceAudit(ctx context.Context, op, uri string, fn resourceAction) (mcp.Result, error) {
	ctx = context.WithValue(ctx, resourceActionKey{}, fn)
	out, err := capability.InvokeTyped(ctx, "mcp.resource."+op, &resourceInput{URI: uri})
	if err != nil {
		return nil, err
	}
	return out.(mcp.Result), nil
}
func (a *Adapter) mountResources(srv *mcp.Server, token string) {
	// Registration advertises resources; middleware recomputes their catalogue
	// for every request, including long-lived stdio sessions.
	srv.AddResource(&mcp.Resource{URI: "knowledge://overview", Name: "知识概览", MIMEType: "application/json"}, nil)
	for _, t := range resourceTemplates() {
		srv.AddResourceTemplate(t, nil)
	}
	srv.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "resources/list" && method != "resources/read" && method != "resources/templates/list" && method != "tools/list" {
				return next(ctx, method, req)
			}
			actor, err := a.authenticateToken(ctx, token)
			if err != nil {
				return nil, errcode.Unauthorizedf("访问令牌已失效")
			}
			if previous, ok := capability.ActorFrom(ctx); ok {
				actor.IP = previous.IP
				actor.UserAgent = previous.UserAgent
			}
			if checked, _ := ctx.Value(httpCheckedKey{}).(bool); !checked {
				if err := a.rate(ctx, actor); err != nil {
					return nil, err
				}
			}
			ctx = capability.WithActor(ctx, actor)
			if method == "tools/list" {
				out, err := next(ctx, method, req)
				if err != nil {
					return nil, err
				}
				listed, ok := out.(*mcp.ListToolsResult)
				if !ok {
					return out, nil
				}
				allowed := map[string]bool{}
				for _, e := range a.visibleEntries(ctx) {
					allowed[strings.ReplaceAll(e.Name, ".", "_")] = true
				}
				tools := []*mcp.Tool{}
				for _, tool := range listed.Tools {
					if tool.Name == "confirm_plan" && actor.ReadOnly {
						continue
					}
					if a.cfg.MCP.ToolMode == "flat" && tool.Name != "confirm_plan" && !allowed[tool.Name] {
						continue
					}
					tools = append(tools, tool)
				}
				copy := *listed
				copy.Tools = tools
				return &copy, nil
			}
			if method == "resources/templates/list" {
				return resourceAudit(ctx, "list", "templates", func(context.Context, resourceInput) (any, error) {
					return &mcp.ListResourceTemplatesResult{ResourceTemplates: resourceTemplates()}, nil
				})
			}
			if method == "resources/list" {
				return resourceAudit(ctx, "list", "", func(ctx context.Context, _ resourceInput) (any, error) { return a.listResources(ctx) })
			}
			params, ok := req.GetParams().(*mcp.ReadResourceParams)
			if !ok {
				return nil, errcode.BadRequestf("资源参数无效")
			}
			return resourceAudit(ctx, "read", params.URI, func(ctx context.Context, in resourceInput) (any, error) { return a.readResource(ctx, in.URI) })
		}
	})
}
func resourceTemplates() []*mcp.ResourceTemplate {
	return []*mcp.ResourceTemplate{{URITemplate: "capability://{name}", Name: "有权使用的功能描述", MIMEType: "application/json"}, {URITemplate: "dict://{type}", Name: "授权实体使用的字典", MIMEType: "application/json"}, {URITemplate: "entity://{table}", Name: "授权实体结构", MIMEType: "application/json"}}
}

// entityPrefixes lives in the knowledge package, which owns entity metadata,
// so this filter and the startup coverage check cannot disagree.
var entityPrefixes = knowledge.CapabilityEntities

func (a *Adapter) visibleEntries(ctx context.Context) []*capability.Entry {
	actor, _ := capability.ActorFrom(ctx)
	out := []*capability.Entry{}
	for _, e := range capability.Visible(capability.SourceMCP, actor.Perms, actor.MaxRisk) {
		if a.tagged(e) && !(actor.ReadOnly && e.Mutating) {
			out = append(out, e)
		}
	}
	return out
}
func (a *Adapter) visibleKnowledge(ctx context.Context) (knowledge.Knowledge, error) {
	out := knowledge.Knowledge{Enums: []knowledge.EnumDef{}, Entities: []knowledge.EntityDef{}, Rules: []knowledge.RuleDef{}}
	if a.knowledge == nil {
		return out, nil
	}
	all, err := a.knowledge.Get(ctx, "all")
	if err != nil {
		return out, err
	}
	known := map[string]bool{}
	for _, e := range all.Entities {
		known[e.Table] = true
	}
	allowed := map[string]bool{}
	for _, e := range a.visibleEntries(ctx) {
		matched := false
		for prefix, entity := range entityPrefixes {
			if strings.HasPrefix(e.Name, prefix+".") {
				matched = true
				if entity != "" {
					allowed[entity] = true
				}
			}
		}
		// The system and monitor namespaces predate the naming convention, so
		// they need the explicit table above. A module added later is expected
		// to name its capabilities after its table — shop.order.list over
		// shop_order — and is resolved here without editing this file.
		if !matched {
			if table, ok := knowledge.ConventionTable(e.Name); ok && known[table] {
				allowed[table] = true
			}
		}
	}
	for _, e := range all.Entities {
		if allowed[e.Table] {
			relations := []knowledge.Relation{}
			for _, r := range e.Relations {
				if allowed[r.Target] {
					relations = append(relations, r)
				}
			}
			e.Relations = relations
			out.Entities = append(out.Entities, e)
		}
	}
	for _, d := range all.Enums {
		used := []string{}
		for _, field := range d.UsedBy {
			if allowed[strings.SplitN(field, ".", 2)[0]] {
				used = append(used, field)
			}
		}
		if len(used) > 0 || allowed["sys_dict_data"] {
			d.UsedBy = used
			out.Enums = append(out.Enums, d)
		}
	}
	for _, r := range all.Rules {
		if r.Scope == "*" || allowed[r.Scope] {
			out.Rules = append(out.Rules, r)
		}
	}
	return out, nil
}
func resource(uri, name string) *mcp.Resource {
	return &mcp.Resource{URI: uri, Name: name, MIMEType: "application/json"}
}
func (a *Adapter) listResources(ctx context.Context) (*mcp.ListResourcesResult, error) {
	actor, _ := capability.ActorFrom(ctx)
	out := &mcp.ListResourcesResult{Resources: []*mcp.Resource{resource("context://me", "当前授权主体")}}
	if len(actor.Perms) == 0 {
		return out, nil
	}
	k, err := a.visibleKnowledge(ctx)
	if err != nil {
		return nil, err
	}
	out.Resources = append(out.Resources, resource("knowledge://overview", "授权知识概览"), resource("knowledge://rules", "授权业务规则"))
	for _, e := range a.visibleEntries(ctx) {
		out.Resources = append(out.Resources, resource("capability://"+e.Name, e.Summary))
	}
	for _, d := range k.Enums {
		out.Resources = append(out.Resources, resource("dict://"+d.DictType, d.Summary))
	}
	for _, e := range k.Entities {
		out.Resources = append(out.Resources, resource("entity://"+e.Table, e.Summary))
	}
	sort.Slice(out.Resources, func(i, j int) bool { return out.Resources[i].URI < out.Resources[j].URI })
	return out, nil
}
func (a *Adapter) readResource(ctx context.Context, uri string) (*mcp.ReadResourceResult, error) {
	actor, _ := capability.ActorFrom(ctx)
	var value any
	if uri == "context://me" {
		value = map[string]any{"userId": actor.UserID, "userName": actor.UserName, "permissions": actor.Perms, "maxRisk": actor.MaxRisk, "source": actor.Source}
	} else if strings.HasPrefix(uri, "capability://") {
		e, _, err := a.entry(ctx, strings.TrimPrefix(uri, "capability://"))
		if err != nil {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		value = e.Describe()
	} else {
		if len(actor.Perms) == 0 {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		k, err := a.visibleKnowledge(ctx)
		if err != nil {
			return nil, err
		}
		switch uri {
		case "knowledge://overview":
			value = k
		case "knowledge://rules":
			value = k.Rules
		}
		for _, d := range k.Enums {
			if uri == "dict://"+d.DictType {
				value = d
			}
		}
		for _, e := range k.Entities {
			if uri == "entity://"+e.Table {
				value = e
			}
		}
	}
	if value == nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	raw, err := json.Marshal(capability.SanitizeValue(value))
	if err != nil {
		return nil, err
	}
	if len(raw) > 64<<10 {
		return nil, errcode.BadRequestf("资源超过 64KiB，请读取更具体的字典、实体或功能")
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(raw)}}}, nil
}

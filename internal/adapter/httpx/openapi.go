package httpx

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/junler/intent-admin/internal/capability"
)

// OpenAPI builds an OpenAPI 3.1 document from the registry.
//
// The document is generated rather than annotated so it cannot drift from the
// code. 3.1 is used because its schema dialect is JSON Schema 2020-12, which
// is exactly what the capability registry already holds — no lossy conversion.
func OpenAPI(entries []*capability.Entry, basePath, version string) map[string]any {
	paths := map[string]any{}

	for _, e := range entries {
		if e.HTTP == nil {
			continue
		}
		add(paths, basePath+normalizePath(e.HTTP.Path), strings.ToLower(e.HTTP.Method), operation(e))

		if e.Mutating && e.HasDryRun() {
			add(paths, basePath+normalizePath(PreviewPath(e)), "post", previewOperation(e))
		}
		if e.Export != nil {
			add(paths, basePath+normalizePath(e.HTTP.Path)+"/export", "post", exportOperation(e))
			add(paths, basePath+normalizePath(e.HTTP.Path)+"/template", "get", templateOperation(e))
		}
	}

	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "intent-admin API",
			"version":     version,
			"description": "由 Capability 注册表自动生成。每个写操作另有 {path}/preview 返回结构化 diff。",
		},
		"servers": []any{map[string]any{"url": "/"}},
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type": "http", "scheme": "bearer", "bearerFormat": "JWT",
				},
			},
		},
		"security": []any{map[string]any{"bearerAuth": []any{}}},
	}
}

func add(paths map[string]any, path, method string, op map[string]any) {
	item, ok := paths[path].(map[string]any)
	if !ok {
		item = map[string]any{}
		paths[path] = item
	}
	item[method] = op
}

// gin uses :param, OpenAPI uses {param}.
var pathParamRe = regexp.MustCompile(`:([A-Za-z0-9_]+)`)

func normalizePath(p string) string {
	return pathParamRe.ReplaceAllString(p, "{$1}")
}

func operation(e *capability.Entry) map[string]any {
	op := map[string]any{
		"operationId": e.Name,
		"summary":     e.Summary,
		"description": describe(e),
		"tags":        tagsOf(e),
		"responses":   responses(e.OutputSchema),
	}
	if params := pathParams(e.HTTP.Path); len(params) > 0 {
		op["parameters"] = params
	}
	// Inputs are described as a request body for verbs that carry one, and
	// left to the (already documented) query binding otherwise. Enumerating
	// every query field would duplicate the input schema without adding
	// information a reader does not already have.
	if e.HTTP.Method != http.MethodGet && e.HTTP.Method != http.MethodDelete {
		op["requestBody"] = requestBody(e.InputSchema)
	}
	return op
}

func previewOperation(e *capability.Entry) map[string]any {
	return map[string]any{
		"operationId": e.Name + ".preview",
		"summary":     e.Summary + "（预览）",
		"description": "只读预演：返回将要发生的结构化 diff、影响条数与是否可回滚，不修改任何数据。",
		"tags":        tagsOf(e),
		"requestBody": requestBody(e.InputSchema),
		"responses":   responses(nil),
	}
}

func exportOperation(e *capability.Entry) map[string]any {
	return map[string]any{
		"operationId": e.Name + ".export",
		"summary":     e.Summary + "（导出 Excel）",
		"description": "入参与列表接口完全一致，分页参数被忽略。仅限管理页面调用。",
		"tags":        tagsOf(e),
		"requestBody": requestBody(e.InputSchema),
		"responses": map[string]any{
			"200": map[string]any{
				"description": "xlsx 文件",
				"content": map[string]any{
					xlsxContentType: map[string]any{
						"schema": map[string]any{"type": "string", "format": "binary"},
					},
				},
			},
		},
	}
}

func templateOperation(e *capability.Entry) map[string]any {
	return map[string]any{
		"operationId": e.Name + ".template",
		"summary":     e.Summary + "（导入模板）",
		"description": "只含表头的空 xlsx，列定义与导出一致。",
		"tags":        tagsOf(e),
		"responses": map[string]any{
			"200": map[string]any{
				"description": "xlsx 文件",
				"content": map[string]any{
					xlsxContentType: map[string]any{
						"schema": map[string]any{"type": "string", "format": "binary"},
					},
				},
			},
		},
	}
}

// describe surfaces the properties a reader of the API needs but that no
// OpenAPI field expresses: the permission, the risk level, and whether the
// operation can be undone.
func describe(e *capability.Entry) string {
	var b strings.Builder
	b.WriteString(e.Summary)
	b.WriteString("\n\n- Capability：`" + e.Name + "`")
	if e.Perm != "" {
		b.WriteString("\n- 所需权限：`" + e.Perm + "`")
	} else {
		b.WriteString("\n- 所需权限：登录即可")
	}
	b.WriteString("\n- 风险等级：" + e.Risk.Label() + "（`" + string(e.Risk) + "`）")
	if e.Mutating {
		if e.Reversible {
			b.WriteString("\n- 可回滚：是（变更历史里可一键撤销）")
		} else {
			b.WriteString("\n- 可回滚：**否**")
		}
	}
	if e.AIEnabled {
		b.WriteString("\n- AI 可见：是")
	}
	if e.MCPEnabled {
		b.WriteString("\n- MCP 可见：是")
	}
	return b.String()
}

func tagsOf(e *capability.Entry) []string {
	if len(e.Tags) > 0 {
		return e.Tags
	}
	return []string{"其它"}
}

func pathParams(path string) []any {
	matches := pathParamRe.FindAllStringSubmatch(path, -1)
	out := make([]any, 0, len(matches))
	for _, m := range matches {
		out = append(out, map[string]any{
			"name":     m[1],
			"in":       "path",
			"required": true,
			"schema":   map[string]any{"type": "string"},
		})
	}
	return out
}

func requestBody(schema *jsonschema.Schema) map[string]any {
	return map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{"schema": schemaOrEmpty(schema)},
		},
	}
}

func responses(out *jsonschema.Schema) map[string]any {
	data := schemaOrEmpty(out)
	return map[string]any{
		"200": map[string]any{
			"description": "统一信封；code=200 为成功",
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"code": map[string]any{"type": "integer", "examples": []any{200}},
							"msg":  map[string]any{"type": "string"},
							"data": data,
						},
						"required": []any{"code", "msg"},
					},
				},
			},
		},
	}
}

func schemaOrEmpty(s *jsonschema.Schema) any {
	if s == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return map[string]any{}
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// MountDocs serves the generated document and a Swagger UI page.
func MountDocs(r gin.IRouter, basePath, version string) {
	r.GET("/openapi.json", func(c *gin.Context) {
		c.JSON(http.StatusOK, OpenAPI(capability.All(), basePath, version))
	})
	r.GET("/swagger", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerHTML))
	})
}

// swaggerHTML loads Swagger UI from a CDN. Vendoring the bundle would add
// ~2 MB to the binary for a developer-only page; an offline build can swap
// this for the embedded asset.
const swaggerHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>intent-admin API</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: '/openapi.json',
      dom_id: '#ui',
      deepLinking: true,
      docExpansion: 'none',
      tagsSorter: 'alpha',
      operationsSorter: 'alpha'
    });
  </script>
</body>
</html>`

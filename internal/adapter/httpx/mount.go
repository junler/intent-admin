// Package httpx turns registered capabilities into HTTP endpoints.
//
// Handlers are generated, not written by hand. Besides removing boilerplate
// this is what guarantees that the HTTP surface, the OpenAPI document, the AI
// tool catalogue and the MCP tool list all describe the same thing: they are
// projections of one registry rather than four hand-maintained lists.
//
// Each mutating capability additionally gets a {path}/preview endpoint, which
// is how the human-facing UI obtains the same structured diff the AI sees.
// Pages therefore get dry-run and undo without writing any preview logic.
package httpx

import (
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/response"
)

// Options configures endpoint generation.
type Options struct {
	// ExportMaxRows caps a synchronous export; above it the request is
	// refused with a hint to use background export.
	ExportMaxRows int
	// ExportStreamThreshold switches the renderer to excelize's StreamWriter.
	ExportStreamThreshold int
	// DictLookup renders a dictionary value as display text for exports.
	// Optional: without it the raw value is written.
	DictLookup func(dictType, value string) string
}

// Mount registers every capability that declares an HTTP binding.
func Mount(g *gin.RouterGroup, entries []*capability.Entry, opts Options) {
	var mounted, previews, exports int
	for _, e := range entries {
		if e.HTTP == nil {
			continue
		}
		g.Handle(e.HTTP.Method, e.HTTP.Path, invokeHandler(e))
		mounted++

		// Preview endpoint. Always POST: the filter payload can exceed what a
		// query string comfortably carries, and it is read-only regardless of
		// the verb the underlying capability uses.
		if e.Mutating && e.HasDryRun() {
			g.POST(previewPath(e.HTTP.Path), previewHandler(e))
			previews++
		}

		if e.Export != nil {
			g.POST(e.HTTP.Path+"/export", exportHandler(e, opts))
			g.GET(e.HTTP.Path+"/template", templateHandler(e))
			exports++
		}
	}
	slog.Info("已挂载 HTTP 端点",
		"capabilities", mounted, "preview", previews, "export", exports)
}

// previewPath derives the preview route. Path parameters are dropped: a
// preview describes a set of rows selected by the body, so a preview of
// "DELETE /system/user/:ids" lives at "POST /system/user/preview" and takes
// the ids in the body.
func previewPath(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	kept := make([]string, 0, len(segments))
	for _, s := range segments {
		if strings.HasPrefix(s, ":") || strings.HasPrefix(s, "*") {
			continue
		}
		kept = append(kept, s)
	}
	return "/" + strings.Join(kept, "/") + "/preview"
}

func invokeHandler(e *capability.Entry) gin.HandlerFunc {
	return func(c *gin.Context) {
		in := e.NewInput()
		if err := Bind(c, in); err != nil {
			response.Fail(c, err)
			return
		}
		out, err := capability.InvokeTyped(c.Request.Context(), e.Name, in)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, out)
	}
}

func previewHandler(e *capability.Entry) gin.HandlerFunc {
	return func(c *gin.Context) {
		in := e.NewInput()
		if err := Bind(c, in); err != nil {
			response.Fail(c, err)
			return
		}
		p, err := capability.RunPreviewTyped(c.Request.Context(), e.Name, in)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, p)
	}
}

func templateHandler(e *capability.Entry) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := capability.ActorFrom(c.Request.Context())
		if !ok {
			response.Fail(c, errcode.Unauthorizedf("请先登录"))
			return
		}
		if !actor.Can(e.ExportPerm()) {
			response.Fail(c, errcode.Forbiddenf("没有权限导出%s", e.Summary))
			return
		}
		writeTemplate(c, e)
	}
}

package httpx

import (
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/paginate"
)

// DisableGinValidation turns off gin's built-in struct validation.
//
// Validation must happen exactly once, in the capability executor, for two
// reasons: the AI and MCP entry points never go through gin at all, and gin
// validates after each partial bind — so a `validate:"required"` field that
// arrives in the JSON body would fail while binding the URI. One validator,
// one error format, one place to change.
func DisableGinValidation() { binding.Validator = nil }

// Bind fills a capability input from the request: path params, then query,
// then JSON body.
//
// The order matters. Body is applied last so that for a PUT the body is
// authoritative, while absent body fields keep whatever the query supplied.
func Bind(c *gin.Context, in any) error {
	if len(c.Params) > 0 {
		if err := c.ShouldBindUri(in); err != nil {
			return errcode.BadRequestf("路径参数错误：%v", err)
		}
	}
	if err := c.ShouldBindQuery(in); err != nil {
		return errcode.BadRequestf("查询参数错误：%v", err)
	}
	if hasJSONBody(c) {
		if err := c.ShouldBindJSON(in); err != nil {
			return errcode.BadRequestf("请求体格式错误：%v", err)
		}
	}
	return nil
}

func hasJSONBody(c *gin.Context) bool {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead:
		return false
	}
	if c.Request.Body == nil || c.Request.ContentLength == 0 {
		return false
	}
	ct := c.ContentType()
	return ct == "" || strings.HasPrefix(ct, binding.MIMEJSON)
}

// ErrNoPagination is returned when an export is requested for a capability
// whose input has no pagination to widen.
var ErrNoPagination = errors.New("该功能的入参没有分页字段，无法导出")

// widenForExport raises the page size to the export limit.
//
// Export reuses the list capability unchanged, which is what guarantees the
// spreadsheet and the screen agree. The only adjustment is that the user's
// current page must not limit the file: exporting page 2 of 10 would surprise
// everyone.
func widenForExport(in any, maxRows int) error {
	q := findPaginateQuery(reflect.ValueOf(in))
	if q == nil {
		return ErrNoPagination
	}
	limit := maxRows
	if limit <= 0 || limit > paginate.ExportPageSize {
		limit = paginate.ExportPageSize
	}
	q.PageNum = 1
	q.PageSize = limit
	q.Unbounded = true
	return nil
}

// findPaginateQuery locates an embedded or named paginate.Query inside the
// input struct, at any nesting depth.
func findPaginateQuery(v reflect.Value) *paginate.Query {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	target := reflect.TypeOf(paginate.Query{})
	if v.Type() == target {
		if !v.CanAddr() {
			return nil
		}
		return v.Addr().Interface().(*paginate.Query)
	}
	for i := range v.NumField() {
		field := v.Field(i)
		switch field.Kind() {
		case reflect.Struct, reflect.Pointer:
			if q := findPaginateQuery(field); q != nil {
				return q
			}
		}
	}
	return nil
}


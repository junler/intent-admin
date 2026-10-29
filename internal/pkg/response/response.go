// Package response defines the single response envelope used by every HTTP
// endpoint: {code, msg, data}. Business failures return HTTP 200 with a
// non-200 `code`; a small set of codes also surface as real HTTP statuses so
// the frontend can handle them generically (see errcode.HTTPStatus).
package response

import (
	"github.com/gin-gonic/gin"

	"github.com/junler/intent-admin/internal/pkg/errcode"
)

// Body is the response envelope.
type Body struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

// Page is the shape of `data` for paginated list results.
type Page struct {
	Rows     any   `json:"rows"`
	Total    int64 `json:"total"`
	PageNum  int   `json:"pageNum"`
	PageSize int   `json:"pageSize"`
}

// OK writes a success envelope.
func OK(c *gin.Context, data any) {
	c.JSON(errcode.HTTPStatus(errcode.OK), Body{Code: errcode.OK, Msg: "操作成功", Data: data})
}

// Fail writes an error envelope, deriving the code and HTTP status from err.
func Fail(c *gin.Context, err error) {
	e := errcode.From(err)
	c.JSON(errcode.HTTPStatus(e.Code), Body{Code: e.Code, Msg: e.Msg, Data: e.Detail})
}

// Abort writes an error envelope and stops the middleware chain.
func Abort(c *gin.Context, err error) {
	e := errcode.From(err)
	c.AbortWithStatusJSON(errcode.HTTPStatus(e.Code), Body{Code: e.Code, Msg: e.Msg, Data: e.Detail})
}

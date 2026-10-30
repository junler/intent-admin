package importer

import (
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/response"
)

func (s *Service) Mount(g *gin.RouterGroup) {
	for _, sp := range specs {
		g.DELETE(sp.Base+"/import/:confirmationId", func(c *gin.Context) {
			if err := s.discard(c.Request.Context(), sp, c.Param("confirmationId")); err != nil {
				response.Fail(c, err)
				return
			}
			response.OK(c, nil)
		})
		g.GET(sp.Base+"/import/template", func(c *gin.Context) {
			if _, err := s.authorize(c.Request.Context(), sp); err != nil {
				response.Fail(c, err)
				return
			}
			headers := []string{}
			for _, col := range sp.Columns {
				label := col.Label
				if c.Query("locale") == "en-US" {
					label = englishColumns[col.Field]
				}
				if col.Required {
					label += "*"
				}
				headers = append(headers, label)
			}
			data, err := workbook(headers, nil)
			if err != nil {
				response.Fail(c, err)
				return
			}
			writeXLSX(c, sp.Title+"导入模板.xlsx", data)
		})
		g.POST(sp.Base+"/import/preview", func(c *gin.Context) {
			if _, err := s.authorize(c.Request.Context(), sp); err != nil {
				response.Fail(c, err)
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxFileBytes+(64<<10))
			if err := c.Request.ParseMultipartForm(MaxFileBytes); err != nil {
				response.Fail(c, errcode.BadRequestf("请选择不超过 5 MB 的 XLSX 文件"))
				return
			}
			if c.Request.MultipartForm != nil {
				defer c.Request.MultipartForm.RemoveAll()
			}
			file, header, err := c.Request.FormFile("file")
			if err != nil {
				response.Fail(c, errcode.BadRequestf("请选择 XLSX 文件"))
				return
			}
			defer file.Close()
			if len(c.Request.MultipartForm.File["file"]) != 1 || len(c.Request.MultipartForm.File) != 1 || header.Size > MaxFileBytes || !strings.HasSuffix(strings.ToLower(header.Filename), ".xlsx") {
				response.Fail(c, errcode.BadRequestf("一次只接受一个不超过 5 MB 的 XLSX 文件"))
				return
			}
			report, err := s.Preview(c.Request.Context(), sp, io.LimitReader(file, MaxFileBytes+1))
			if err != nil {
				response.Fail(c, err)
				return
			}
			response.OK(c, report)
		})
		g.GET(sp.Base+"/import/:confirmationId/errors", func(c *gin.Context) {
			v, _, err := s.load(c.Request.Context(), sp, c.Param("confirmationId"))
			if err != nil {
				response.Fail(c, err)
				return
			}
			rows := [][]any{}
			for _, e := range v.Errors {
				message := e.Message
				if c.Query("locale") == "en-US" {
					message = englishError(message)
				}
				rows = append(rows, []any{e.Row, message})
			}
			headers := []string{"文件行号", "校验错误"}
			if c.Query("locale") == "en-US" {
				headers = []string{"Spreadsheet row", "Validation error"}
			}
			data, err := workbook(headers, rows)
			if err != nil {
				response.Fail(c, err)
				return
			}
			writeXLSX(c, sp.Title+"导入错误.xlsx", data)
		})
		g.POST(sp.Base+"/import/confirm", func(c *gin.Context) {
			var in ConfirmInput
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
			if err := c.ShouldBindJSON(&in); err != nil {
				response.Fail(c, errcode.BadRequestf("确认参数无效"))
				return
			}
			ctx := capability.WithCallResult(c.Request.Context(), &capability.CallResult{})
			if actor, ok := capability.ActorFrom(ctx); ok {
				confirmed := *actor
				confirmed.ApprovalMode = "diff_confirmed"
				ctx = capability.WithActor(ctx, &confirmed)
			}
			out, err := capability.InvokeTyped(ctx, importName(sp), &in)
			if err != nil {
				response.Fail(c, err)
				return
			}
			result := out.(Result)
			c.Header("X-Changeset-Id", result.ChangesetID)
			response.OK(c, result)
		})
	}
}
func writeXLSX(c *gin.Context, name string, data []byte) {
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

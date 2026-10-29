package plan

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/response"
)

// RegisterApprovalHTTP mounts inside the authenticated /api/ai router group.
// The service additionally enforces browser-human identity, participant binding
// and uncached authorization, so AI/MCP callers cannot approve through it.
func (s *Service) RegisterApprovalHTTP(ai gin.IRoutes) {
	write := func(c *gin.Context, out any, err error) {
		c.Header("Cache-Control", "no-store")
		if err != nil {
			response.Fail(c, err)
		} else {
			response.OK(c, out)
		}
	}
	ai.GET("/approvals", func(c *gin.Context) {
		var in ApprovalQuery
		if c.ShouldBindQuery(&in) != nil || in.PageNum > 1_000_000 {
			write(c, nil, errcode.BadRequestf("分页参数无效"))
			return
		}
		in.Normalize()
		rows, total, err := s.ListApprovals(c.Request.Context(), in)
		write(c, response.Page{Rows: rows, Total: total, PageNum: in.PageNum, PageSize: in.PageSize}, err)
	})
	ai.GET("/approval-reviewers", func(c *gin.Context) {
		out, err := s.Reviewers(c.Request.Context(), c.Query("planId"))
		write(c, out, err)
	})
	ai.GET("/approvals/:id", func(c *gin.Context) {
		out, err := s.GetApproval(c.Request.Context(), c.Param("id"))
		write(c, out, err)
	})
	ai.POST("/plans/:id/approval", func(c *gin.Context) {
		var in RequestApprovalIn
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		if c.ShouldBindJSON(&in) != nil {
			write(c, nil, errcode.BadRequestf("审批参数无效"))
			return
		}
		out, err := s.RequestApproval(c.Request.Context(), c.Param("id"), in)
		write(c, out, err)
	})
	for _, decision := range []string{"approve", "reject", "revoke"} {
		ai.POST("/approvals/:id/"+decision, func(c *gin.Context) {
			var in ApprovalDecisionIn
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
			if c.ShouldBindJSON(&in) != nil {
				write(c, nil, errcode.BadRequestf("审批意见无效"))
				return
			}
			out, err := s.DecideApproval(c.Request.Context(), c.Param("id"), decision, in)
			write(c, out, err)
		})
	}
	ai.POST("/approvals/:id/apply", func(c *gin.Context) {
		out, err := s.ApplyApproval(c.Request.Context(), c.Param("id"))
		write(c, out, err)
	})
}

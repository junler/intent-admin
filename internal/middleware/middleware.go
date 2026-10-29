// Package middleware holds the HTTP concerns that run before the capability
// executor: request identity, panic recovery, logging and CORS.
//
// Permission checks, the risk ceiling, auditing and changeset recording are
// deliberately NOT here. They live in the capability executor so that the AI
// and MCP entry points — which never pass through gin — are subject to exactly
// the same rules.
package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/response"
)

// RequestIDHeader is the header carrying the correlation id.
const RequestIDHeader = "X-Request-Id"

// RequestID assigns or propagates a correlation id.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(string(RequestIDHeader), id)
		c.Writer.Header().Set(RequestIDHeader, id)
		c.Request = c.Request.WithContext(
			withRequestID(c.Request.Context(), id))
		c.Next()
	}
}

// Recover converts a panic into a 500 envelope and logs the stack.
func Recover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("HTTP handler panic",
					"panic", r,
					"path", c.Request.URL.Path,
					"method", c.Request.Method,
					"stack", string(debug.Stack()),
				)
				if !c.Writer.Written() {
					response.Abort(c, errcode.Internalf("服务异常，请稍后重试"))
					return
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

// Logger emits one structured line per request.
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		level := slog.LevelInfo
		status := c.Writer.Status()
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		} else if status >= http.StatusBadRequest {
			level = slog.LevelWarn
		}
		attrs := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", status,
			"cost_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
			"request_id", RequestIDFrom(c),
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "errors", c.Errors.String())
		}
		slog.Log(c.Request.Context(), level, "http", attrs...)
	}
}

// CORSConfig configures cross-origin access.
type CORSConfig struct {
	// AllowOrigins lists permitted origins. Empty disables CORS entirely,
	// which is the right default for the single-binary deployment where the
	// frontend is served from the same origin.
	AllowOrigins []string
}

// CORS applies an allowlist-based policy. Credentials are allowed only for
// explicitly listed origins — never with a wildcard, which browsers reject
// anyway and which would be a mistake if they did not.
func CORS(cfg CORSConfig) gin.HandlerFunc {
	allowed := make(map[string]bool, len(cfg.AllowOrigins))
	for _, o := range cfg.AllowOrigins {
		allowed[strings.TrimRight(o, "/")] = true
	}
	return func(c *gin.Context) {
		origin := strings.TrimRight(c.GetHeader("Origin"), "/")
		if origin == "" || !allowed[origin] {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.Next()
			return
		}
		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Allow-Credentials", "true")
		h.Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Authorization,Content-Type,"+RequestIDHeader)
		h.Set("Access-Control-Expose-Headers", "Content-Disposition,"+RequestIDHeader)
		h.Set("Access-Control-Max-Age", "600")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// RequestIDFrom reads the correlation id off the gin context.
func RequestIDFrom(c *gin.Context) string {
	v, _ := c.Get(string(RequestIDHeader))
	s, _ := v.(string)
	return s
}

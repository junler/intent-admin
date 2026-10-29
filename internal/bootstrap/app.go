// Package bootstrap assembles the application: open the infrastructure, wire
// the capability executor's hooks, register features, build the HTTP router.
//
// Every cobra subcommand shares this code, so `serve`, `worker` and `mcp`
// cannot drift apart in how they configure the executor.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"gorm.io/gorm"

	"github.com/junler/intent-admin/internal/adapter/httpx"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/feature/demo"
	"github.com/junler/intent-admin/internal/middleware"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/response"
	"github.com/junler/intent-admin/internal/pkg/secret"
	"github.com/junler/intent-admin/internal/web"
)

// Build info, injected via -ldflags.
var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

// App holds the assembled dependencies.
type App struct {
	Cfg    *config.Config
	DB     *gorm.DB
	Cache  *cache.Cache
	Secret *secret.Cipher
}

// New opens the infrastructure and wires the capability executor.
func New(cfg *config.Config) (*App, error) {
	for _, w := range cfg.Warnings() {
		slog.Warn("配置提醒", "detail", w)
	}

	cipher, err := secret.New(cfg.App.EncryptKey)
	if err != nil {
		return nil, err
	}
	db, err := dbx.Open(cfg)
	if err != nil {
		return nil, err
	}
	rdb, err := cache.Open(cfg)
	if err != nil {
		dbx.Close(db)
		return nil, err
	}

	app := &App{Cfg: cfg, DB: db, Cache: rdb, Secret: cipher}

	if err := app.installExecutor(); err != nil {
		app.Close()
		return nil, err
	}
	app.registerFeatures()

	// The self-check enforces the invariants a linter cannot see — above all
	// that configuration capabilities are closed to the AI and MCP. Refusing
	// to start is correct: the failure mode it prevents is silent data
	// exfiltration, which nobody would notice in time.
	if err := capability.SelfCheck(); err != nil {
		app.Close()
		return nil, err
	}
	for _, w := range capability.Warnings() {
		slog.Warn("Capability 提醒", "detail", w)
	}
	slog.Info("Capability 注册完成", "count", capability.Count())

	return app, nil
}

func (a *App) installExecutor() error {
	maxRisk, err := capability.ParseRisk(a.Cfg.AI.MaxRisk)
	if err != nil {
		return fmt.Errorf("ai.max_risk 无效: %w", err)
	}
	_ = maxRisk // consumed by the AI adapter in a later phase

	v := validator.New(validator.WithRequiredStructEnabled())

	capability.Install(capability.Hooks{
		Tx:                dbx.NewTxRunner(a.DB),
		ReadOnly:          dbx.NewReadOnlyRunner(),
		Validate:          func(in any) error { return v.Struct(in) },
		MaxChangesetItems: a.Cfg.Changeset.MaxItemsPerSet,
		// PersistChanges and WriteAudit are installed in Phase 2/3 together
		// with the changeset and audit tables. Until then a mutating
		// capability still runs inside a transaction and still collects
		// Recorder entries — they are simply not persisted yet.
		PersistChanges: nil,
		WriteAudit:     a.logAudit,
	})
	return nil
}

// logAudit writes the behaviour trail to the log until the sys_oper_log table
// exists (Phase 3). Keeping the hook live from day one means every mutating
// capability is already producing a complete audit record, so switching the
// sink to the database later is a one-line change rather than a retrofit.
func (a *App) logAudit(_ context.Context, rec capability.AuditRecord) {
	slog.Info("audit",
		"capability", rec.Capability,
		"actor_type", rec.ActorType,
		"source", rec.Source,
		"user", rec.UserName,
		"risk", rec.Risk,
		"success", rec.Success,
		"cost_ms", rec.CostMS,
		"changeset", rec.ChangesetID,
		"intent", rec.Intent,
		"param", rec.Param,
		"error", rec.ErrorMsg,
	)
}

func (a *App) registerFeatures() {
	demo.Register(a.DB)
}

// Router builds the HTTP handler: middleware, capability endpoints, docs and
// the embedded frontend.
func (a *App) Router() *gin.Engine {
	if a.Cfg.App.IsProd() {
		gin.SetMode(gin.ReleaseMode)
	}
	httpx.DisableGinValidation()

	r := gin.New()
	r.Use(
		middleware.RequestID(),
		middleware.Recover(),
		middleware.Logger(),
		middleware.CORS(middleware.CORSConfig{}),
	)

	r.GET("/healthz", a.health)
	r.GET("/version", func(c *gin.Context) {
		response.OK(c, map[string]string{
			"version": Version, "commit": Commit, "buildTime": BuildTime,
		})
	})
	httpx.MountDocs(r, a.Cfg.App.BasePath, Version)

	api := r.Group(a.Cfg.App.BasePath)
	// Until Phase 1 lands JWT authentication, a development-only actor is
	// injected so the capability layer can be exercised. It is refused in
	// production: an unauthenticated superuser is not a thing that should ever
	// be reachable by accident.
	api.Use(a.devActor())
	httpx.Mount(api, capability.All(), httpx.Options{
		ExportMaxRows:         a.Cfg.Export.MaxRows,
		ExportStreamThreshold: a.Cfg.Export.StreamThreshold,
	})

	// The SPA handler is mounted last so API routes always win.
	r.NoRoute(gin.WrapH(web.Handler(a.Cfg.App.BasePath)))
	return r
}

// devActor is a placeholder until Phase 1 implements JWT authentication.
func (a *App) devActor() gin.HandlerFunc {
	if a.Cfg.App.IsProd() {
		return func(c *gin.Context) {
			response.Abort(c, errcode.Unauthorizedf("认证模块尚未实现（Phase 1），生产环境拒绝访问"))
		}
	}
	slog.Warn("开发模式：注入临时超管身份，认证模块将在 Phase 1 实现")
	return func(c *gin.Context) {
		actor := &capability.Actor{
			Type:         capability.ActorHuman,
			Source:       capability.SourceHTTP,
			UserID:       1,
			UserName:     "dev",
			Perms:        []string{capability.SuperPerm},
			MaxRisk:      capability.RiskCritical,
			IP:           c.ClientIP(),
			UserAgent:    c.Request.UserAgent(),
			ApprovalMode: "manual",
		}
		c.Request = c.Request.WithContext(capability.WithActor(c.Request.Context(), actor))
		c.Next()
	}
}

func (a *App) health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	status := map[string]any{"version": Version, "db": "ok", "redis": "ok"}
	code := errcode.OK

	if sqlDB, err := a.DB.DB(); err != nil || sqlDB.PingContext(ctx) != nil {
		status["db"] = "down"
		code = errcode.InternalError
	}
	if err := a.Cache.Ping(ctx).Err(); err != nil {
		status["redis"] = "down"
		code = errcode.InternalError
	}
	if code != errcode.OK {
		response.Fail(c, errcode.New(code, "依赖不可用").WithDetail(status))
		return
	}
	response.OK(c, status)
}

// Serve runs the HTTP server until ctx is cancelled, then shuts down
// gracefully so in-flight requests and SSE streams can finish.
func (a *App) Serve(ctx context.Context) error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", a.Cfg.App.Port),
		Handler:           a.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: SSE streams are long-lived by design and a global
		// write deadline would cut them off mid-conversation.
		IdleTimeout: 120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("HTTP 服务已启动",
			"addr", srv.Addr, "env", a.Cfg.App.Env,
			"basePath", a.Cfg.App.BasePath, "version", Version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("收到退出信号，正在优雅关闭")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// Close releases the infrastructure.
func (a *App) Close() {
	if a == nil {
		return
	}
	a.Cache.Close()
	dbx.Close(a.DB)
}

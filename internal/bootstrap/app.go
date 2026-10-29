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
	"github.com/junler/intent-admin/internal/adapter/mcpx"
	"github.com/junler/intent-admin/internal/agent"
	"github.com/junler/intent-admin/internal/auth"
	"github.com/junler/intent-admin/internal/cache"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/changeset"
	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/feature/dashboard"
	"github.com/junler/intent-admin/internal/feature/demo"
	"github.com/junler/intent-admin/internal/feature/importer"
	"github.com/junler/intent-admin/internal/feature/monitor"
	"github.com/junler/intent-admin/internal/feature/resources"
	"github.com/junler/intent-admin/internal/feature/system"
	"github.com/junler/intent-admin/internal/knowledge"
	"github.com/junler/intent-admin/internal/middleware"
	"github.com/junler/intent-admin/internal/notification"
	"github.com/junler/intent-admin/internal/oauth"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/response"
	"github.com/junler/intent-admin/internal/pkg/secret"
	"github.com/junler/intent-admin/internal/plan"
	"github.com/junler/intent-admin/internal/task"
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
	Cfg           *config.Config
	DB            *gorm.DB
	Cache         *cache.Cache
	Secret        *secret.Cipher
	Auth          *auth.Service
	System        *system.Service
	Resources     *resources.Service
	Changesets    *changeset.Service
	Plans         *plan.Service
	Knowledge     *knowledge.Service
	Tasks         *task.Runtime
	Dashboard     *dashboard.Service
	Agent         *agent.Service
	MCP           *mcpx.Adapter
	Imports       *importer.Service
	Notifications *notification.Service
	OAuth         *oauth.Service
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
	app.Auth = auth.New(db, rdb, cfg)
	app.Changesets = changeset.New(db)
	app.Knowledge = knowledge.New(db)
	app.Tasks = task.New(db, rdb, cfg)

	if err := app.installExecutor(); err != nil {
		app.Close()
		return nil, err
	}
	if err := app.registerFeatures(); err != nil {
		app.Close()
		return nil, err
	}

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
	mcpRisk, err := capability.ParseRisk(a.Cfg.MCP.MaxRisk)
	if err != nil {
		return fmt.Errorf("mcp.max_risk 无效: %w", err)
	}

	v := validator.New(validator.WithRequiredStructEnabled())

	capability.Install(capability.Hooks{
		MaxAIRisk:         maxRisk,
		MaxMCPRisk:        mcpRisk,
		Tx:                dbx.NewTxRunner(a.DB),
		ReadOnly:          dbx.NewReadOnlyRunner(),
		Validate:          func(in any) error { return v.Struct(in) },
		MaxChangesetItems: a.Cfg.Changeset.MaxItemsPerSet,
		PersistChanges:    a.Changesets.Persist,
		WriteAudit:        a.Changesets.WriteAudit,
	})
	return nil
}

func (a *App) registerFeatures() error {
	demo.Register(a.DB)
	a.System = system.Register(a.DB, a.Cache, a.Cfg)
	a.System.OnDictionaryChange = a.Knowledge.Invalidate
	a.Resources = resources.Register(a.DB, a.Cache, a.Cfg, a.Secret)
	if err := a.Resources.Bootstrap(context.Background()); err != nil {
		return err
	}
	a.Changesets.Invalidate = a.System.Invalidate
	a.Changesets.AuthorizeUndo = system.AuthorizeUndo(a.System)
	a.Changesets.Register()
	maxRisk, _ := capability.ParseRisk(a.Cfg.AI.MaxRisk)
	a.Plans = plan.New(a.DB, plan.Options{TTL: a.Cfg.AI.PlanTTL, MaxSteps: a.Cfg.AI.MaxPlanSteps, MaxAffected: a.Cfg.AI.MaxAffected, MaxRisk: maxRisk, Changesets: a.Changesets})
	a.Imports = importer.New(a.DB, a.Cache, a.Changesets, a.Cfg.Changeset.MaxItemsPerSet)
	a.Notifications = notification.New(a.DB)
	a.Plans.ActorLoader = a.Auth.CurrentAIActor
	a.Plans.Notify = func(ctx context.Context, event plan.ApprovalNotification) error {
		titles := map[string]string{"pending": "收到计划审批", "approved": "计划审批已通过", "rejected": "计划审批已拒绝", "revoked": "计划审批已撤销", "applied": "审批计划已执行", "stale": "计划审批需要更新"}
		title := titles[event.Status]
		if title == "" {
			title = "计划审批状态已更新"
		}
		return a.Notifications.Publish(ctx, event.UserID, title, "请进入审批中心查看计划及最新审批状态。", "/ai/approvals?id="+event.ApprovalID)
	}
	monitor.Register(a.DB, a.Cache, a.Cfg, a.Tasks)
	a.Dashboard = dashboard.Register(a.DB, a.Cache, a.Cfg, a.Tasks)
	a.Tasks.FileWriter = a.Resources.StoreReader
	a.Tasks.FileCleaner = a.Resources.CleanupExports
	a.Tasks.RefreshDashboard = a.Dashboard.Warm
	a.Tasks.RefreshKnowledge = func(context.Context) error { a.Knowledge.Invalidate(); return nil }
	a.Agent = agent.New(a.DB, a.Cache, a.Cfg, a.Resources, a.Plans, a.Knowledge)
	a.MCP = mcpx.New(a.Cache, a.Cfg, a.Auth.AuthenticatePAT, a.Knowledge)
	a.MCP.SetConfirmationRunner(a.Plans.ConfirmedRun)
	if a.Cfg.MCP.Enabled && a.Cfg.MCP.OAuthIssuer != "" {
		var err error
		a.OAuth, err = oauth.New(a.DB, a.Cache, oauth.Options{Issuer: a.Cfg.MCP.OAuthIssuer, ResourcePath: a.Cfg.MCP.Path, ConsentPath: "/oauth/consent"}, a.Auth.CurrentMCPActor)
		if err != nil {
			return err
		}
		a.OAuth.Audit = a.Changesets.WriteAudit
		a.MCP.SetOAuth(a.OAuth.Authenticate, a.OAuth.ResourceMetadataURL())
	}
	return nil
}

// Router builds the HTTP handler: middleware, capability endpoints, docs and
// the embedded frontend.
func (a *App) Router() *gin.Engine {
	if a.Cfg.App.IsProd() {
		gin.SetMode(gin.ReleaseMode)
	}
	httpx.DisableGinValidation()

	r := gin.New()
	// Trust the direct peer by default so clients cannot forge X-Forwarded-For
	// to evade authentication throttles. Explicit proxy configuration can be
	// added when a deployment needs a trusted ingress.
	_ = r.SetTrustedProxies(nil)
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

	a.Auth.MountPublic(r.Group(a.Cfg.App.BasePath))
	a.Resources.MountPublic(r.Group(a.Cfg.App.BasePath))
	if a.OAuth != nil {
		a.OAuth.MountPublic(r)
	}
	api := r.Group(a.Cfg.App.BasePath)
	api.Use(a.Auth.Middleware())
	a.Auth.MountProtected(api)
	a.Resources.Mount(api)
	a.Agent.Mount(api)
	a.Plans.RegisterApprovalHTTP(api.Group("/ai"))
	a.Imports.Mount(api)
	a.Notifications.Mount(api)
	if a.OAuth != nil {
		a.OAuth.MountProtected(api)
	}
	httpx.Mount(api, capability.All(), httpx.Options{
		ExportMaxRows:         a.Cfg.Export.MaxRows,
		ExportStreamThreshold: a.Cfg.Export.StreamThreshold,
		AsyncExport:           a.Tasks.EnqueueExport,
		DictLookup:            a.dictionaryLabel,
	})
	if a.Cfg.MCP.Enabled {
		r.Any(a.Cfg.MCP.Path, gin.WrapH(a.MCP.Handler()))
	}

	// The SPA handler is mounted last so API routes always win.
	r.NoRoute(gin.WrapH(web.Handler(a.Cfg.App.BasePath)))
	return r
}

func (a *App) dictionaryLabel(dictType, value string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	items, err := a.System.Dictionary(ctx, dictType)
	if err != nil {
		return value
	}
	for _, item := range items {
		if fmt.Sprint(item["dictValue"]) == value {
			return fmt.Sprint(item["dictLabel"])
		}
	}
	return value
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
	if err := a.Tasks.Start(ctx); err != nil {
		return err
	}
	if err := a.Dashboard.Warm(ctx); err != nil {
		slog.Warn("大盘预热失败，将按需加载", "error", err)
	}
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
	if a.Tasks != nil {
		a.Tasks.Close()
	}
	a.Cache.Close()
	dbx.Close(a.DB)
}

package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/junler/intent-admin/internal/bootstrap"
)

func newServeCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "启动 HTTP 服务（含 MCP Server 与内嵌 worker）",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if port > 0 {
				cfg.App.Port = port
			}
			app, err := bootstrap.New(cfg)
			if err != nil {
				return err
			}
			defer app.Close()

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return app.Serve(ctx)
		},
	}
	cmd.Flags().IntVarP(&port, "port", "p", 0, "监听端口（覆盖配置文件）")
	return cmd
}

func newWorkerCmd() *cobra.Command {
	var concurrency int
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "启动异步任务 worker（可多副本）",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if concurrency > 0 {
				cfg.Task.Concurrency = concurrency
			}
			app, err := bootstrap.New(cfg)
			if err != nil {
				return err
			}
			defer app.Close()
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return app.Tasks.RunWorker(ctx)
		},
	}
	cmd.Flags().IntVar(&concurrency, "concurrency", 0, "并发处理数（覆盖配置文件）")
	return cmd
}

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "以 stdio 方式启动 MCP Server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			token := os.Getenv("INTENT_ADMIN_MCP_TOKEN")
			if token == "" {
				return fmt.Errorf("请通过 INTENT_ADMIN_MCP_TOKEN 提供个人访问令牌")
			}
			app, err := bootstrap.New(cfg)
			if err != nil {
				return err
			}
			defer app.Close()
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return app.MCP.ServeStdio(ctx, token)
		},
	}
}

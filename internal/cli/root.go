// Package cli defines the cobra command tree.
//
// One binary, several roles: serve, worker, mcp, migrate. They share the same
// configuration loading and the same bootstrap assembly, which is what keeps
// the HTTP process and the worker process from configuring the capability
// executor differently.
package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/junler/intent-admin/internal/config"
)

var (
	cfgFile  string
	logLevel string
	cfg      *config.Config
)

// Execute runs the command tree.
func Execute() {
	if err := newRoot().Execute(); err != nil {
		// cobra has already printed the message; exit non-zero for the shell.
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "intent-admin",
		Short: "intent-admin —— AI 原生的后台管理系统",
		Long: `intent-admin 是一个 AI 原生的后台管理系统。

人与 AI 双入口等权操作，所有写操作可预览、可追溯、可一键回滚，
并可作为 MCP Server 被外部 AI 客户端调用。

子命令：
  serve      启动 HTTP 服务（含 MCP Server）
  worker     启动异步任务 worker
  mcp        以 stdio 方式启动 MCP Server
  migrate    数据库迁移
  version    版本信息`,
		SilenceUsage: true,
		// Usage noise on a runtime failure buries the actual error.
		SilenceErrors: false,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// version needs neither configuration nor a database.
			if cmd.Name() == "version" {
				return nil
			}
			setupLogger(logLevel)
			loaded, err := config.Load(cfgFile)
			if err != nil {
				return err
			}
			cfg = loaded
			setupLogger(pick(logLevel, cfg.Log.Level))
			return nil
		},
	}

	root.PersistentFlags().StringVarP(&cfgFile, "config", "c", "",
		"配置文件路径（默认依次查找 ./configs/config.yaml、./config.yaml）")
	root.PersistentFlags().StringVar(&logLevel, "log-level", "",
		"日志级别：debug|info|warn|error（覆盖配置文件）")

	root.AddCommand(newServeCmd(), newWorkerCmd(), newMCPCmd(), newMigrateCmd(), newDemoCmd(), newVersionCmd())
	return root
}

func setupLogger(level string) {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	format := "json"
	if cfg != nil {
		format = cfg.Log.Format
	}
	opts := &slog.HandlerOptions{Level: lv}
	var h slog.Handler
	if format == "text" {
		h = slog.NewTextHandler(os.Stderr, opts)
	} else {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
}

func pick(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

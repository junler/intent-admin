package cli

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
	"github.com/spf13/cobra"

	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/migrations"
)

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "数据库迁移",
		Long: `数据库迁移。迁移脚本以 SQL 形式嵌入二进制，不依赖外部文件。

本项目不使用 AutoMigrate：结构变更应当是可 review、有明确顺序、有回退路径的 SQL。`,
	}
	cmd.AddCommand(
		migrateActionCmd("up", "升级到最新版本", func(db *sql.DB) error {
			return goose.Up(db, migrations.Dir)
		}),
		migrateActionCmd("down", "回退一个版本", func(db *sql.DB) error {
			return goose.Down(db, migrations.Dir)
		}),
		migrateActionCmd("status", "查看迁移状态", func(db *sql.DB) error {
			return goose.Status(db, migrations.Dir)
		}),
		migrateActionCmd("version", "查看当前数据库版本", func(db *sql.DB) error {
			return goose.Version(db, migrations.Dir)
		}),
		newSeedCmd(),
		newRekeyCmd(),
	)
	return cmd
}

func migrateActionCmd(name, short string, action func(*sql.DB) error) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: short,
		RunE: func(*cobra.Command, []string) error {
			db, err := openSQL(cfg)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			goose.SetBaseFS(migrations.FS)
			goose.SetTableName("schema_migrations")
			if err := goose.SetDialect("postgres"); err != nil {
				return fmt.Errorf("设置迁移方言失败: %w", err)
			}
			return action(db)
		},
	}
}

func newSeedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed",
		Short: "写入初始数据（管理员、角色、菜单、字典、参数）",
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := openSQL(cfg)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			if err := migrations.Seed(cmd.Context(), db); err != nil {
				return err
			}
			cmd.Println("初始数据已补齐；已有密码、配置和菜单保持不变")
			return nil
		},
	}
}

func newRekeyCmd() *cobra.Command {
	var oldKey string
	cmd := &cobra.Command{
		Use:   "rekey",
		Short: "轮换 app.encrypt_key（重新加密库中的模型 Key 与存储 Secret）",
		Long: `用旧密钥解密、用配置中的新密钥重新加密 sys_ai_provider 与 sys_storage_config
里的凭证。必须在旧密钥仍在手上时执行；密钥一旦丢失，凭证无法恢复，只能重新填写。`,
		RunE: func(*cobra.Command, []string) error {
			if oldKey == "" {
				return fmt.Errorf("必须通过 --old-key 提供旧密钥")
			}
			return notImplemented("Phase 3", "密钥轮换")
		},
	}
	cmd.Flags().StringVar(&oldKey, "old-key", "", "旧的 app.encrypt_key")
	return cmd
}

// openSQL opens a plain database/sql handle. goose needs one, and using it
// here instead of GORM keeps migrations independent of the ORM.
func openSQL(c *config.Config) (*sql.DB, error) {
	db, err := sql.Open("pgx", c.DB.DSN())
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("数据库不可用: %w", err)
	}
	return db, nil
}

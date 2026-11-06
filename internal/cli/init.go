package cli

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pressly/goose/v3"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"

	"github.com/junler/intent-admin/internal/config"
	"github.com/junler/intent-admin/migrations"
)

func newInitCmd() *cobra.Command {
	root := &cobra.Command{Use: "init", Short: "初始化新项目配置和数据库"}
	var name string
	conf := &cobra.Command{Use: "config", Short: "生成配置和随机密钥（不覆盖已有文件）", RunE: func(cmd *cobra.Command, _ []string) error {
		path := cfgFile
		if path == "" {
			path = "configs/config.yaml"
		}
		if strings.TrimSpace(name) == "" {
			return errors.New("项目名称不能为空")
		}
		raw, err := config.Initial(name)
		if err != nil {
			return err
		}
		if err := writePrivate(path, raw); err != nil {
			return err
		}
		cmd.Printf("配置已写入 %s；请设置数据库与 Redis 连接，然后运行 init database\n", path)
		return nil
	}}
	conf.Flags().StringVar(&name, "name", "Intent", "项目名称")
	var demo bool
	var credentials string
	database := &cobra.Command{Use: "database", Short: "迁移空数据库并初始化管理员", RunE: func(cmd *cobra.Command, _ []string) error {
		db, err := openSQL(cfg)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		var existing sql.NullString
		if err := db.QueryRowContext(cmd.Context(), "SELECT to_regclass('sys_user')::text").Scan(&existing); err != nil {
			return err
		}
		if existing.Valid {
			return errors.New("数据库已有账号表；请使用 migrate up 升级，初始化不会重置已有账号")
		}
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		password := base64.RawURLEncoding.EncodeToString(b)
		// Reserve the credential file before mutating a database. Passwords never
		// appear in command-line arguments, console output or application logs.
		if err := writePrivate(credentials, []byte("username: admin\npassword: "+password+"\n")); err != nil {
			return err
		}
		goose.SetBaseFS(migrations.FS)
		goose.SetTableName("schema_migrations")
		if err := goose.SetDialect("postgres"); err != nil {
			return err
		}
		if err := goose.Up(db, migrations.Dir); err != nil {
			return err
		}
		if err := initializeAdmin(cmd.Context(), db, password, demo); err != nil {
			return err
		}
		cmd.Printf("初始化完成。管理员凭据保存在 %s，首次登录必须修改密码\n", credentials)
		return nil
	}}
	database.Flags().BoolVar(&demo, "with-demo", false, "启用示例菜单并填充 20 条示例数据")
	database.Flags().StringVar(&credentials, "credentials-file", "configs/initial-admin.txt", "初始管理员凭据文件（权限 0600，不覆盖）")
	root.AddCommand(conf, database)
	return root
}

func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("创建文件失败（不会覆盖已有文件）: %w", err)
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func initializeAdmin(ctx context.Context, db *sql.DB, password string, demo bool) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "UPDATE sys_user SET password=$1,must_change_password=true WHERE user_name='admin' AND del_flag='0'", string(hash)); err != nil {
		return err
	}
	// Empty removes the shared default for accounts created by imports. An
	// operator must explicitly choose an initial password before enabling them.
	if _, err = tx.ExecContext(ctx, "UPDATE sys_config SET config_value='' WHERE config_key='sys.user.initPassword'"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM demo_item"); err != nil {
		return err
	}
	if !demo {
		if _, err = tx.ExecContext(ctx, "UPDATE sys_menu SET status='1' WHERE path='/demo/item' OR perms LIKE 'demo:%'"); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE sys_job SET status='1' WHERE invoke_target LIKE 'demo:%'"); err != nil {
			return err
		}
	} else {
		for i := range 20 {
			if _, err = tx.ExecContext(ctx, "INSERT INTO demo_item(name,category,amount,create_by) VALUES($1,$2,$3,'init')", fmt.Sprintf("示例条目 %03d", i+1), demoCategories[i%3], (i+1)*1250); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

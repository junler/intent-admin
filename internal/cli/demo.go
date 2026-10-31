package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// The example module ships no seed rows, because a real deployment should not
// grow sample records on first start. These commands let someone evaluating
// the template fill the table to exercise previews, changesets and rollback,
// and then clear it before building the real thing.
func newDemoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "示例模块的测试数据",
		Long: `填充或清空示例模块（demo_item）的测试数据。

示例模块用于体验 Capability 层的预览、变更集与回滚。正式项目可以先执行
demo clear 清空数据，再按 CONTRIBUTING.md 的「Remove the example module」删除代码。`,
	}
	cmd.AddCommand(newDemoFillCmd(), newDemoClearCmd())
	return cmd
}

var demoCategories = []string{"alpha", "beta", "gamma"}

func newDemoFillCmd() *cobra.Command {
	var count int
	cmd := &cobra.Command{
		Use:   "fill",
		Short: "写入示例条目测试数据",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if count < 1 || count > 1000 {
				return errors.New("--count 需要在 1 到 1000 之间")
			}
			db, err := openSQL(cfg)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			tx, err := db.BeginTx(cmd.Context(), nil)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback() }()
			for i := range count {
				// Vary category, amount and status so list filters, exports and
				// diff previews all have something to act on.
				category := demoCategories[i%len(demoCategories)]
				status := "0"
				if i%4 == 3 {
					status = "1"
				}
				if _, err = tx.ExecContext(cmd.Context(),
					`INSERT INTO demo_item (name, category, amount, status, remark, create_by)
					 VALUES ($1, $2, $3, $4, $5, 'demo-fill')`,
					fmt.Sprintf("示例条目 %03d", i+1), category, int64(i+1)*1250, status,
					fmt.Sprintf("%s 分类的测试数据", category),
				); err != nil {
					return err
				}
			}
			if err = tx.Commit(); err != nil {
				return err
			}
			cmd.Printf("已写入 %d 条示例条目；执行 intent-admin demo clear --yes 可清空\n", count)
			return nil
		},
	}
	cmd.Flags().IntVar(&count, "count", 20, "写入条数（1-1000）")
	return cmd
}

func newDemoClearCmd() *cobra.Command {
	var confirmed bool
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "清空示例条目数据（不可恢复）",
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := openSQL(cfg)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			var total int64
			if err = db.QueryRowContext(cmd.Context(), `SELECT count(*) FROM demo_item`).Scan(&total); err != nil {
				return err
			}
			if total == 0 {
				cmd.Println("示例条目表已经是空的")
				return nil
			}
			// This is a plain DELETE, not the soft delete the capability uses:
			// the point is to leave a clean table for a real project.
			if !confirmed {
				return fmt.Errorf("将永久删除 %d 条示例条目；确认后请执行 intent-admin demo clear --yes", total)
			}
			res, err := db.ExecContext(cmd.Context(), `DELETE FROM demo_item`)
			if err != nil {
				return err
			}
			removed, _ := res.RowsAffected()
			cmd.Printf("已删除 %d 条示例条目\n", removed)
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirmed, "yes", false, "确认删除")
	return cmd
}

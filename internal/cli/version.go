package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/junler/intent-admin/internal/bootstrap"
)

func newVersionCmd() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "版本信息",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if short {
				_, err := fmt.Fprintln(out, bootstrap.Version)
				return err
			}
			_, err := fmt.Fprintf(out,
				"intent-admin %s\n  commit:     %s\n  build time: %s\n  go:         %s\n  platform:   %s/%s\n",
				bootstrap.Version, bootstrap.Commit, bootstrap.BuildTime,
				runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return err
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "只输出版本号")
	return cmd
}

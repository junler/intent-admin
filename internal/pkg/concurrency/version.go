// Package concurrency contains the shared optimistic concurrency contract.
package concurrency

import "github.com/junler/intent-admin/internal/pkg/errcode"

// Check preserves compatibility with clients without version support. Editors
// send the version read when opening the form; a conflict never overwrites data.
func Check(expected *int64, current int64) error {
	if expected != nil && (*expected < 1 || *expected != current) {
		return errcode.Conflictf("记录已被其他操作修改，请刷新后重新编辑")
	}
	return nil
}

// Command intent-admin is an AI-native admin system.
//
// Humans and AI operate the same capabilities through the same executor, so
// permissions, the risk ceiling, auditing and rollback apply identically to
// both. See docs/implementation-plan.md for the full specification.
package main

import (
	// pgx registers the "pgx" database/sql driver used by the migrate command.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/junler/intent-admin/internal/cli"
)

func main() {
	cli.Execute()
}

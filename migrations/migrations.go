// Package migrations embeds the versioned SQL migrations into the binary, so
// a single executable can bring up its own schema with no external files.
//
// AutoMigrate is deliberately not used. Schema changes are reviewable SQL with
// an explicit order and a down path; letting an ORM infer DDL from structs is
// how production schemas end up differing from every developer's.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

// Dir is the directory name goose uses inside FS.
const Dir = "."

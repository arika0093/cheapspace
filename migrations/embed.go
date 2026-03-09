package migrations

import "embed"

// Files contains the embedded SQLite migration set used by the built-in migrate command.
//
//go:embed *.sql
var Files embed.FS

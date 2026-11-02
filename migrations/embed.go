// Package migrations embeds the SQL migrations so binaries (server, migrate tool, Lambda)
// carry them and never depend on a working-directory-relative path.
package migrations

import "embed"

// FS contains the versioned *.up.sql / *.down.sql files and the local-only auth shim.
//
//go:embed *.up.sql *.down.sql local/*.sql
var FS embed.FS

// Package migrations embeds the SQL schema migrations so the server can apply
// them itself at startup.
//
// The files stay where they are rather than moving under internal/, so the
// golang-migrate CLI invoked by `make migrate-up` and the embedded copy read
// exactly the same directory. There is no second source of truth to drift.
package migrations

import "embed"

// FS holds every migration, keyed by the golang-migrate naming convention
// NNNNNN_description.{up,down}.sql.
//
//go:embed *.sql
var FS embed.FS

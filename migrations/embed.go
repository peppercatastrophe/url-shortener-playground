// Package migrations embeds the versioned SQL migration files so the
// API and worker binaries are self-contained: golang-migrate reads them
// via an io/fs source without needing migration files on disk at runtime.
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed *.up.sql *.down.sql
var rawFS embed.FS

// FS is a filesystem rooted at the migrations directory containing the
// up/down SQL files. golang-migrate's iofs source reads from here.
var FS fs.FS = rawFS
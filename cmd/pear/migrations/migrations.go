// Package migrations holds pear's database migrations, which [FS] exposes to
// goose.
//
// db.Migrate applies them: it first runs GORM's AutoMigrate over the store
// models, then replays these in version order. Go migrations that only need SQL
// register themselves with goose.AddMigrationContext, as goose create generates.
package migrations

import "embed"

// FS holds the migrations in this directory, for goose to replay.
//
// The Go files are embedded alongside the SQL because goose globs *.go to
// resolve their versions; the migration bodies themselves come from the
// registrations their init functions make.
//
//go:embed *.go *.sql
var FS embed.FS

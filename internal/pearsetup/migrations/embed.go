package migrations

import "embed"

// FS holds the goose migration files. It lives beside the migrations because a
// //go:embed directive can only reference files in its own directory, and
// internal/pearsetup needs to hand these to db.New.
//
//go:embed *.go *.sql
var FS embed.FS

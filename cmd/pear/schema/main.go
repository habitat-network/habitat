// Command schema prints the DDL for pear's GORM models in the given dialect
// ("postgres" or "sqlite"), followed by the objects GORM models can't express.
// Atlas runs it as the desired state when generating schema migrations; see
// atlas.hcl.
package main

import (
	"fmt"
	"os"

	"ariga.io/atlas-provider-gorm/gormschema"

	"github.com/habitat-network/habitat/cmd/pear/migrations"
)

// extraSchema is the DDL, per dialect, of objects that hang off the models'
// tables but that GORM models can't express. Listing them here makes them part
// of the desired state, so migration-diff and the drift check expect them
// rather than dropping them.
//
// SQLite has none: the search index's FTS5 table is created by a Go migration
// instead, since the SQLite built into Atlas can't replay FTS5 statements (see
// migrations/20260929180100_search_fts_sqlite.go).
var extraSchema = map[string]string{
	// The search index's full-text index; see
	// migrations/postgres/20260929180100_search_fts.sql.
	"postgres": `CREATE INDEX "search_documents_body_fts" ON "search_documents" ` +
		`USING GIN (to_tsvector('simple', "body"));
`,
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: schema <postgres|sqlite>")
		os.Exit(2)
	}
	stmts, err := gormschema.New(os.Args[1]).Load(migrations.Models()...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load gorm schema: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.WriteString(stmts + extraSchema[os.Args[1]]); err != nil {
		fmt.Fprintf(os.Stderr, "write schema: %v\n", err)
		os.Exit(1)
	}
}

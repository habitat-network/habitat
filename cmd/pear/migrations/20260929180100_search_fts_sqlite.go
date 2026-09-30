package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

// sqliteSearchFTSMigration creates the search index's full-text structures on
// SQLite. Postgres gets its own as a SQL migration with the same version
// (postgres/20260929180100_search_fts.sql), but this one can't be SQL: Atlas
// replays the SQL migrations to check them for drift, and the SQLite built into
// Atlas has no FTS5 (https://github.com/ariga/atlas/issues/2118). go-sqlite3
// needs the sqlite_fts5 build tag to run it.
func sqliteSearchFTSMigration() *goose.Migration {
	return goose.NewGoMigration(
		20260929180100,
		&goose.GoFunc{RunTx: execAll(sqliteSearchFTSUp)},
		&goose.GoFunc{RunTx: execAll(sqliteSearchFTSDown)},
	)
}

// sqliteSearchFTSUp keeps an FTS5 index in step with search_documents. The
// index is an external-content table: it stores only the tokens and reads text
// back from search_documents by rowid, so the text isn't stored twice. Triggers
// mirror every insert, update and delete of search_documents into it.
var sqliteSearchFTSUp = []string{
	`CREATE VIRTUAL TABLE search_documents_fts USING fts5(
		body,
		content='search_documents',
		content_rowid='rowid',
		tokenize='unicode61 remove_diacritics 2'
	)`,
	`CREATE TRIGGER search_documents_ai AFTER INSERT ON search_documents BEGIN
		INSERT INTO search_documents_fts(rowid, body) VALUES (new.rowid, new.body);
	END`,
	`CREATE TRIGGER search_documents_ad AFTER DELETE ON search_documents BEGIN
		INSERT INTO search_documents_fts(search_documents_fts, rowid, body)
		VALUES ('delete', old.rowid, old.body);
	END`,
	`CREATE TRIGGER search_documents_au AFTER UPDATE ON search_documents BEGIN
		INSERT INTO search_documents_fts(search_documents_fts, rowid, body)
		VALUES ('delete', old.rowid, old.body);
		INSERT INTO search_documents_fts(rowid, body) VALUES (new.rowid, new.body);
	END`,
}

var sqliteSearchFTSDown = []string{
	`DROP TRIGGER search_documents_au`,
	`DROP TRIGGER search_documents_ad`,
	`DROP TRIGGER search_documents_ai`,
	`DROP TABLE search_documents_fts`,
}

// execAll returns a goose migration function that runs stmts in order.
func execAll(stmts []string) func(context.Context, *sql.Tx) error {
	return func(ctx context.Context, tx *sql.Tx) error {
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("exec %q: %w", stmt, err)
			}
		}
		return nil
	}
}

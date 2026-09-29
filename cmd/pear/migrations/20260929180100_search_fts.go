package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/search"
)

// searchFTSMigration returns the search full-text index migration. It is added
// to goMigrations rather than registered globally with goose.AddMigrationContext
// because goose's global registry is shared by every goose run in the process,
// including OpenFGA's own migrations of its separate database, which have no
// search_documents table for it to act on.
func searchFTSMigration() *goose.Migration {
	return goose.NewGoMigration(
		20260929180100,
		&goose.GoFunc{RunTx: upSearchFTS},
		&goose.GoFunc{RunTx: downSearchFTS},
	)
}

// upSearchFTS creates the full-text structures over search_documents. They
// aren't in the Atlas-generated schema migrations because Atlas can't express
// them (see search.FTSSchema), so they must not appear in the GORM models.
func upSearchFTS(ctx context.Context, tx *sql.Tx) error {
	postgres, err := isPostgres(ctx, tx)
	if err != nil {
		return err
	}
	dialect := db.Sqlite
	if postgres {
		dialect = db.Postgres
	}
	stmts, err := search.FTSSchema(dialect)
	if err != nil {
		return err
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create search full-text index: %w", err)
		}
	}
	return nil
}

func downSearchFTS(ctx context.Context, tx *sql.Tx) error {
	postgres, err := isPostgres(ctx, tx)
	if err != nil {
		return err
	}
	stmts := []string{
		`DROP TRIGGER IF EXISTS search_documents_ai`,
		`DROP TRIGGER IF EXISTS search_documents_ad`,
		`DROP TRIGGER IF EXISTS search_documents_au`,
		`DROP TABLE IF EXISTS search_documents_fts`,
	}
	if postgres {
		stmts = []string{`DROP INDEX IF EXISTS search_documents_body_fts`}
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("drop search full-text index: %w", err)
		}
	}
	return nil
}

package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func init() {
	goose.AddMigrationContext(upRewriteLegacySpaceUris, downRewriteLegacySpaceUris)
}

// spaceURITables are the tables whose `space` column stores a SpaceURI, so may
// hold pre-0016 (ats://...) URIs: internal/spaces' records and repos, plus
// internal/notify's syncer registrations.
var spaceURITables = []string{"space_records", "space_repos", "registrations"}

func upRewriteLegacySpaceUris(ctx context.Context, tx *sql.Tx) error {
	return rewriteSpaceURIs(ctx, tx, false)
}

func downRewriteLegacySpaceUris(ctx context.Context, tx *sql.Tx) error {
	return rewriteSpaceURIs(ctx, tx, true)
}

// rewriteSpaceURIs converts the `space` column of the spaces tables between the
// pre-0016 format (ats://<did>/<type>/<skey>) and the current format
// (at://<did>/space/<type>/<skey>), in place. The tables are created by GORM's
// AutoMigrate after migrations run, so missing tables (fresh databases) are
// skipped.
func rewriteSpaceURIs(ctx context.Context, tx *sql.Tx, toLegacy bool) error {
	postgres, err := isPostgres(ctx, tx)
	if err != nil {
		return err
	}
	// Up matches the legacy URIs it rewrites; down matches the current ones.
	match := "ats://%"
	if toLegacy {
		match = "at://%"
	}
	for _, table := range spaceURITables {
		if err := rewriteSpaceColumn(ctx, tx, table, match, postgres, toLegacy); err != nil {
			return err
		}
	}
	return nil
}

// rewriteSpaceColumn rewrites every row of table's `space` column that matches
// matchPrefix (either "at://%" or "ats://%"), validating and converting each
// URI with the syntax package.
func rewriteSpaceColumn(
	ctx context.Context,
	tx *sql.Tx,
	table, match string,
	postgres, toLegacy bool,
) error {
	exists, err := tableExists(ctx, tx, table, postgres)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	rows, err := tx.QueryContext(ctx,
		"SELECT space FROM "+table+" WHERE space LIKE "+bind(postgres, 1), match)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	type update struct{ old, new string }
	var updates []update
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		rewritten, err := convertSpaceURI(raw, toLegacy)
		if err != nil {
			return fmt.Errorf("rewrite %s.space %q: %w", table, raw, err)
		}
		updates = append(updates, update{old: raw, new: rewritten})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, u := range updates {
		if _, err := tx.ExecContext(ctx,
			"UPDATE "+table+" SET space = "+bind(postgres, 1)+" WHERE space = "+bind(postgres, 2),
			u.new, u.old); err != nil {
			return err
		}
	}
	return nil
}

// convertSpaceURI parses a stored space URI and returns it in the target
// format: the current at://<did>/space/<type>/<skey> form when toLegacy is
// false, or the pre-0016 ats://<did>/<type>/<skey> form when toLegacy is true.
func convertSpaceURI(raw string, toLegacy bool) (string, error) {
	uri, err := habitat_syntax.ParseSpaceURI(raw)
	if err != nil {
		return "", err
	}
	if toLegacy {
		return uri.Legacy().String(), nil
	}
	return uri.Canonical().String(), nil
}

// bind returns the placeholder for the n-th (1-indexed) bind argument for the
// active driver.
func bind(postgres bool, n int) string {
	if postgres {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// isPostgres reports whether the migration is running against Postgres rather
// than SQLite. The probe uses information_schema, which Postgres always has and
// SQLite lacks; a failed query on SQLite does not abort its transaction, so the
// probe is safe to run before any statements that would poison a Postgres
// transaction.
func isPostgres(ctx context.Context, tx *sql.Tx) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx,
		"SELECT count(*) FROM information_schema.tables").Scan(&one)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// tableExists reports whether name exists in the database.
func tableExists(ctx context.Context, tx *sql.Tx, name string, postgres bool) (bool, error) {
	var query string
	if postgres {
		query = "SELECT count(*) FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = " + bind(postgres, 1)
	} else {
		query = "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?"
	}
	var n int
	if err := tx.QueryRowContext(ctx, query, name).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

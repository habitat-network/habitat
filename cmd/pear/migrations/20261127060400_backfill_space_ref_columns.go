package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func init() {
	goose.AddMigrationContext(upBackfillSpaceRefColumns, downBackfillSpaceRefColumns)
}

// spaceRefTables are the tables with a `space` URI column and the
// space_owner/space_type/space_skey columns that hold its parts: see the
// space_ref_columns schema migration.
var spaceRefTables = []string{
	"space_records",
	"blob_refs",
	"space_repos",
	"space_remote_records",
	"space_remote_syncs",
	"registrations",
	"space_webhooks",
	"space_deleted_deliveries",
}

// upBackfillSpaceRefColumns fills the space_owner, space_type and space_skey
// of every row that has none, by parsing its `space` URI. Every accepted URI
// form parses (at://, at:space/ and the pre-0016 ats://), so the parts are the
// same whichever form a row was stored in. A row's `space` is left as it is.
//
// It only touches rows whose parts are all unset, so it is idempotent and safe
// to run again, which is the point: run it once more before the `space`
// columns are dropped to cover rows written by instances that did not yet
// write the parts.
func upBackfillSpaceRefColumns(ctx context.Context, tx *sql.Tx) error {
	postgres, err := isPostgres(ctx, tx)
	if err != nil {
		return err
	}
	for _, table := range spaceRefTables {
		// Go migrations also run in tests that apply no schema migrations first.
		exists, err := tableExists(ctx, tx, table, postgres)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if err := backfillSpaceRefTable(ctx, tx, table, postgres); err != nil {
			return err
		}
	}
	return nil
}

// backfillSpaceRefTable updates the table one distinct space at a time, since
// a table has far fewer spaces than rows.
func backfillSpaceRefTable(ctx context.Context, tx *sql.Tx, table string, postgres bool) error {
	rows, err := tx.QueryContext(ctx,
		"SELECT DISTINCT space FROM "+table+" WHERE space_owner IS NULL")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var spaces []string
	for rows.Next() {
		var space string
		if err := rows.Scan(&space); err != nil {
			return err
		}
		spaces = append(spaces, space)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_ = rows.Close()

	update := "UPDATE " + table + " SET space_owner = " + bind(postgres, 1) +
		", space_type = " + bind(postgres, 2) +
		", space_skey = " + bind(postgres, 3) +
		" WHERE space = " + bind(postgres, 4) + " AND space_owner IS NULL"
	for _, space := range spaces {
		uri, err := habitat_syntax.ParseSpaceURI(space)
		if err != nil {
			return fmt.Errorf("backfill %s.space %q: %w", table, space, err)
		}
		ref := uri.Ref()
		if _, err := tx.ExecContext(ctx, update,
			ref.Owner.String(), ref.Type.String(), ref.Skey.String(), space); err != nil {
			return fmt.Errorf("backfill %s.space %q: %w", table, space, err)
		}
	}
	return nil
}

// downBackfillSpaceRefColumns does nothing: `space` is still stored, and the
// columns' own migration drops them on the way down.
func downBackfillSpaceRefColumns(context.Context, *sql.Tx) error { return nil }

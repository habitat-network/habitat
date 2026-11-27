package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upRebackfillSpaceRefColumns, downRebackfillSpaceRefColumns)
}

// upRebackfillSpaceRefColumns runs the space_ref_columns backfill again, to
// fill the parts of rows written since by instances that did not yet write
// them. It must run before the schema migration that makes the part columns
// the primary key, which needs every row to have them.
func upRebackfillSpaceRefColumns(ctx context.Context, tx *sql.Tx) error {
	return upBackfillSpaceRefColumns(ctx, tx)
}

// downRebackfillSpaceRefColumns does nothing: the backfill only fills columns.
func downRebackfillSpaceRefColumns(context.Context, *sql.Tx) error { return nil }

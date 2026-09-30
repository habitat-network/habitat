package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"

	"github.com/habitat-network/habitat/internal/spaces"
)

func init() {
	goose.AddMigrationContext(upBackfillBlobRefs, downBackfillBlobRefs)
}

// upBackfillBlobRefs records the blobs referenced by records written before
// blob_refs existed. Without it getBlob would 404 for their blobs, since it
// only serves blobs some record in the space references. It runs after the
// schema migration that creates the table. Databases without the spaces
// tables (as in tests that run only a migration) have nothing to backfill.
func upBackfillBlobRefs(ctx context.Context, tx *sql.Tx) error {
	mc, err := GetPearMigrationContext(ctx, tx)
	if err != nil {
		return err
	}
	postgres, err := isPostgres(ctx, tx)
	if err != nil {
		return err
	}
	exists, err := tableExists(ctx, tx, "space_records", postgres)
	if err != nil || !exists {
		return err
	}
	return spaces.BackfillBlobRefs(mc.DB)
}

// downBackfillBlobRefs does nothing: the schema migration's down drops the
// table the backfill filled.
func downBackfillBlobRefs(context.Context, *sql.Tx) error {
	return nil
}

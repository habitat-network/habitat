package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(
		upBackfillSpaceRecordValueJSONAgain,
		func(context.Context, *sql.Tx) error { return nil },
	)
}

// upBackfillSpaceRecordValueJSONAgain re-runs the backfill right before the
// CBOR column is dropped, so rows written by an older binary between the first
// backfill and this release (which left value_json NULL) don't lose their value.
func upBackfillSpaceRecordValueJSONAgain(ctx context.Context, tx *sql.Tx) error {
	return upBackfillSpaceRecordValueJSON(ctx, tx)
}

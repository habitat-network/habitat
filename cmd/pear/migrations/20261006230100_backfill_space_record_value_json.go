package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upBackfillSpaceRecordValueJSON, downBackfillSpaceRecordValueJSON)
}

// backfillBatchSize bounds how many rows are held in memory at once.
const backfillBatchSize = 500

// upBackfillSpaceRecordValueJSON fills space_records.value_json from the CBOR
// in space_records.value for rows written before the column existed. Soft
// deleted rows are included, since reads that include them need the value too.
// Each pass converts the next batch of rows still missing a JSON value, so
// the migration is idempotent and rows written since the column was added
// are left alone.
func upBackfillSpaceRecordValueJSON(ctx context.Context, tx *sql.Tx) error {
	postgres, err := isPostgres(ctx, tx)
	if err != nil {
		return err
	}
	set := "?"
	if postgres {
		set = "$1::jsonb"
	}
	for {
		n, err := backfillBatch(ctx, tx, set, postgres)
		if err != nil {
			return err
		}
		if n < backfillBatchSize {
			return nil
		}
	}
}

// backfillBatch converts one batch of rows and returns how many it converted.
func backfillBatch(ctx context.Context, tx *sql.Tx, set string, postgres bool) (int, error) {
	type row struct {
		space, repo, collection, rkey string
		value                         []byte
	}
	rows, err := tx.QueryContext(ctx,
		"SELECT space, repo, collection, rkey, value FROM space_records "+
			"WHERE value_json IS NULL AND value IS NOT NULL LIMIT "+fmt.Sprint(backfillBatchSize))
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.space, &r.repo, &r.collection, &r.rkey, &r.value); err != nil {
			return 0, err
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	_ = rows.Close()

	update := "UPDATE space_records SET value_json = " + set + " WHERE space = " + bind(postgres, 2) +
		" AND repo = " + bind(postgres, 3) + " AND collection = " + bind(postgres, 4) +
		" AND rkey = " + bind(postgres, 5)
	for _, r := range batch {
		record, err := atdata.UnmarshalCBOR(r.value)
		if err != nil {
			return 0, fmt.Errorf("decode cbor of %s/%s/%s/%s: %w",
				r.space, r.repo, r.collection, r.rkey, err)
		}
		out, err := json.Marshal(record)
		if err != nil {
			return 0, fmt.Errorf("encode json of %s/%s/%s/%s: %w",
				r.space, r.repo, r.collection, r.rkey, err)
		}
		if _, err := tx.ExecContext(ctx, update,
			string(out), r.space, r.repo, r.collection, r.rkey); err != nil {
			return 0, err
		}
	}
	return len(batch), nil
}

// downBackfillSpaceRecordValueJSON does nothing: the CBOR value is still
// stored, and the column's own migration drops value_json on the way down.
func downBackfillSpaceRecordValueJSON(context.Context, *sql.Tx) error { return nil }

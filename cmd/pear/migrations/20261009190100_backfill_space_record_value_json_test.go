package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestBackfillSpaceRecordValueJSONSqlite(t *testing.T) {
	requireBackfill(
		t,
		newDB(t, "sqlite://"+t.TempDir()+"/test.db"),
		"text",
		"json_extract(value_json, '$.text')",
	)
}

func TestBackfillSpaceRecordValueJSONPostgres(t *testing.T) {
	ctx := context.Background()
	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("pear"),
		postgres.WithUsername("pear"),
		postgres.WithPassword("pear"),
		tc.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })
	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	requireBackfill(t, newDB(t, connStr), "jsonb", "value_json->>'text'")
}

// requireBackfill seeds more rows than one batch, including a soft-deleted one
// and one that already has JSON, and checks the migration fills in exactly the
// missing ones. jsonType and textExpr are the dialect's JSON column type and
// an expression extracting $.text from it.
func requireBackfill(t *testing.T, sqlDB *sql.DB, jsonType, textExpr string) {
	t.Helper()
	ctx := context.Background()
	postgres := jsonType == "jsonb"
	blob := "BLOB"
	if postgres {
		blob = "BYTEA"
	}
	_, err := sqlDB.ExecContext(ctx, "CREATE TABLE space_records ("+
		"space TEXT, repo TEXT, collection TEXT, rkey TEXT, value "+blob+", value_json "+jsonType+", "+
		"deleted_at TIMESTAMP, PRIMARY KEY (space, repo, collection, rkey))")
	require.NoError(t, err)

	insert := func(rkey string, value []byte, valueJSON any, deleted bool) {
		var deletedAt any
		if deleted {
			deletedAt = time.Now()
		}
		_, err := sqlDB.ExecContext(
			ctx,
			"INSERT INTO space_records (space, repo, collection, rkey, value, value_json, deleted_at) "+
				"VALUES ("+placeholders(
				postgres,
				7,
			)+")",
			"at://did:plc:o/space/t/k",
			"did:plc:r",
			"network.habitat.note",
			rkey,
			value,
			valueJSON,
			deletedAt,
		)
		require.NoError(t, err, rkey)
	}
	cbor := func(text string) []byte {
		b, err := atdata.MarshalCBOR(map[string]any{"text": text})
		require.NoError(t, err)
		return b
	}
	n := backfillBatchSize + 10
	for i := range n {
		insert(fmt.Sprintf("r%04d", i), cbor(fmt.Sprintf("v%d", i)), nil, false)
	}
	insert("deleted", cbor("gone"), nil, true)
	insert("existing", cbor("stale-cbor"), `{"text":"already-json"}`, false)
	insert("nullvalue", nil, nil, false)

	tx, err := sqlDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, upBackfillSpaceRecordValueJSON(ctx, tx))
	require.NoError(t, tx.Commit())

	text := func(rkey string) (got sql.NullString) {
		require.NoError(t, sqlDB.QueryRowContext(ctx,
			"SELECT "+textExpr+" FROM space_records WHERE rkey = "+bind(postgres, 1),
			rkey).Scan(&got))
		return got
	}
	for i := range n {
		require.Equal(t, fmt.Sprintf("v%d", i), text(fmt.Sprintf("r%04d", i)).String)
	}
	require.Equal(t, "gone", text("deleted").String)
	require.Equal(t, "already-json", text("existing").String, "existing JSON is left alone")
	require.False(t, text("nullvalue").Valid, "rows without a value stay NULL")

	// Running again is a no-op.
	tx, err = sqlDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, upBackfillSpaceRecordValueJSON(ctx, tx))
	require.NoError(t, tx.Commit())
}

// placeholders returns n comma-separated bind placeholders starting at 1.
func placeholders(postgres bool, n int) string {
	out := make([]string, n)
	for i := range out {
		out[i] = bind(postgres, i+1)
	}
	return strings.Join(out, ", ")
}

// TestBackfillSpaceRecordValueJSONSkipsMissingTable covers databases where the
// schema migrations haven't created space_records.
func TestBackfillSpaceRecordValueJSONSkipsMissingTable(t *testing.T) {
	requireInTx(t, newDB(t, "sqlite://"+t.TempDir()+"/test.db"), upBackfillSpaceRecordValueJSON)
}

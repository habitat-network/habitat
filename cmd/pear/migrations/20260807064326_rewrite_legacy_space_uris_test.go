package migrations

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/habitat-network/habitat/internal/db"
)

// Two distinct spaces, seeded in opposite formats so a single pass exercises
// both a URI that needs rewriting and one that is already in the target format.
const (
	legacyA  = "ats://did:plc:abc123/network.habitat.space/3lm2n"
	currentA = "at://did:plc:abc123/space/network.habitat.space/3lm2n"
	legacyB  = "ats://did:plc:def456/network.habitat.organization/self"
	currentB = "at://did:plc:def456/space/network.habitat.organization/self"
)

func TestRewriteLegacySpaceUrisSqlite(t *testing.T) {
	requireRewriteRoundTrip(t, newSQLite(t))
}

func TestRewriteLegacySpaceUrisPostgres(t *testing.T) {
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
	requireRewriteRoundTrip(t, newDB(t, connStr))
}

// requireRewriteRoundTrip seeds each migrated table with one legacy and one
// already-current row, then asserts that up normalizes both to the current
// format and down returns both to the legacy format. The mixed seed also covers
// idempotency: rows already in the target format must be left untouched.
func requireRewriteRoundTrip(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	ctx := context.Background()

	for _, table := range spaceURITables {
		_, err := sqlDB.ExecContext(ctx,
			"CREATE TABLE "+table+" (space TEXT PRIMARY KEY, repo TEXT)")
		require.NoError(t, err, "create %s", table)
		for _, space := range []string{legacyA, currentB} {
			_, err := sqlDB.ExecContext(ctx,
				"INSERT INTO "+table+" (space, repo) VALUES ('"+space+"', 'did:plc:repo')")
			require.NoError(t, err, "seed %s", table)
		}
	}

	requireInTx(t, sqlDB, upRewriteLegacySpaceUris)
	for _, table := range spaceURITables {
		require.ElementsMatch(t, []string{currentA, currentB}, spaceColumn(t, sqlDB, table),
			"%s after up", table)
	}

	requireInTx(t, sqlDB, downRewriteLegacySpaceUris)
	for _, table := range spaceURITables {
		require.ElementsMatch(t, []string{legacyA, legacyB}, spaceColumn(t, sqlDB, table),
			"%s after down", table)
	}
}

// TestRewriteLegacySpaceUrisSkipsMissingTables covers a fresh database, where the
// GORM-managed tables don't exist yet because AutoMigrate runs after migrations.
func TestRewriteLegacySpaceUrisSkipsMissingTables(t *testing.T) {
	sqlDB := newSQLite(t)
	requireInTx(t, sqlDB, upRewriteLegacySpaceUris)
	requireInTx(t, sqlDB, downRewriteLegacySpaceUris)
}

// TestRewriteLegacySpaceUrisRejectsUnparseableURI checks that a row we cannot
// parse fails the migration rather than being silently skipped or corrupted.
func TestRewriteLegacySpaceUrisRejectsUnparseableURI(t *testing.T) {
	ctx := context.Background()
	sqlDB := newSQLite(t)

	_, err := sqlDB.ExecContext(ctx, "CREATE TABLE space_records (space TEXT PRIMARY KEY)")
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, "INSERT INTO space_records (space) VALUES ('ats://nope')")
	require.NoError(t, err)

	tx, err := sqlDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	require.Error(t, upRewriteLegacySpaceUris(ctx, tx))
}

func newSQLite(t *testing.T) *sql.DB {
	t.Helper()
	return newDB(t, "sqlite://"+t.TempDir()+"/test.db")
}

// newDB opens a database through internal/db so the tests run against the same
// drivers the server uses, and unwraps the *sql.DB the migrations need.
func newDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	gormDB, err := db.New(dsn)
	require.NoError(t, err)
	sqlDB, err := gormDB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

func requireInTx(t *testing.T, sqlDB *sql.DB, fn func(context.Context, *sql.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := sqlDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, fn(ctx, tx))
	require.NoError(t, tx.Commit())
}

func spaceColumn(t *testing.T, sqlDB *sql.DB, table string) []string {
	t.Helper()
	rows, err := sqlDB.QueryContext(context.Background(),
		"SELECT space FROM "+table+" ORDER BY space")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var spaces []string
	for rows.Next() {
		var space string
		require.NoError(t, rows.Scan(&space))
		spaces = append(spaces, space)
	}
	require.NoError(t, rows.Err())
	return spaces
}

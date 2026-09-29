package migrations

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

// seedLegacyRegistrations creates the registrations table as it looked before
// the audience migration: keyed on `endpoint`, with no `audience` column.
func seedLegacyRegistrations(t *testing.T, sqlDB *sql.DB, endpoints ...string) {
	t.Helper()
	ctx := context.Background()
	_, err := sqlDB.ExecContext(ctx, `CREATE TABLE registrations (
		space text, repo text, endpoint text,
		expires_at datetime, created_at datetime, updated_at datetime,
		PRIMARY KEY (space, repo, endpoint))`)
	require.NoError(t, err)
	for _, ep := range endpoints {
		_, err := sqlDB.ExecContext(
			ctx,
			"INSERT INTO registrations (space, repo, endpoint) VALUES ('at://did:plc:org/space/network.habitat.group/s1','',?)",
			ep,
		)
		require.NoError(t, err, "seed %s", ep)
	}
}

// TestNotifyRegistrationAudienceMigratesKey is the core case: existing rows keep
// their data and gain an audience equal to the endpoint they were registered
// with, and the table's key becomes `audience`.
func TestNotifyRegistrationAudienceMigratesKey(t *testing.T) {
	sqlDB := newSQLite(t)
	seedLegacyRegistrations(t, sqlDB, "https://a.example", "https://b.example")

	requireInTx(t, sqlDB, upNotifyRegistrationAudience)

	// Keyed on audience now.
	require.Equal(
		t,
		[]string{"https://a.example", "https://b.example"},
		notifyColumn(t, sqlDB, "audience"),
	)
	// And the endpoint is preserved as the delivery address.
	require.Equal(
		t,
		[]string{"https://a.example", "https://b.example"},
		notifyColumn(t, sqlDB, "endpoint"),
	)
	require.Contains(
		t,
		notifyTableSQL(t, sqlDB),
		"PRIMARY KEY (space, repo, audience)",
		"table should be keyed on audience after the migration",
	)
}

// TestNotifyRegistrationAudienceIsIdempotent covers a database where the column
// already exists (a newer binary's AutoMigrate ran first) and where rows already
// carry an audience: re-running must not re-copy the endpoint over it.
func TestNotifyRegistrationAudienceIsIdempotent(t *testing.T) {
	sqlDB := newSQLite(t)
	seedLegacyRegistrations(t, sqlDB, "https://a.example")
	_, err := sqlDB.ExecContext(context.Background(),
		`ALTER TABLE registrations ADD audience text`)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(context.Background(),
		`UPDATE registrations SET audience='did:web:sap.example#habitat_space_syncer'`)
	require.NoError(t, err)

	requireInTx(t, sqlDB, upNotifyRegistrationAudience)

	require.Equal(
		t,
		[]string{"did:web:sap.example#habitat_space_syncer"},
		notifyColumn(t, sqlDB, "audience"),
		"an existing audience must be preserved, not overwritten by the endpoint",
	)
}

// TestNotifyRegistrationAudienceSkipsMissingTable covers a fresh database, where
// GORM's AutoMigrate has not created the table yet.
func TestNotifyRegistrationAudienceSkipsMissingTable(t *testing.T) {
	sqlDB := newSQLite(t)
	requireInTx(t, sqlDB, upNotifyRegistrationAudience)
	requireInTx(t, sqlDB, downNotifyRegistrationAudience)
}

// TestNotifyRegistrationAudienceDownRestoresEndpointKey checks the down
// migration returns the table to keying on `endpoint`, which is what a rollback
// to the previous binary expects.
func TestNotifyRegistrationAudienceDownRestoresEndpointKey(t *testing.T) {
	sqlDB := newSQLite(t)
	seedLegacyRegistrations(t, sqlDB, "https://a.example", "https://b.example")

	requireInTx(t, sqlDB, upNotifyRegistrationAudience)
	requireInTx(t, sqlDB, downNotifyRegistrationAudience)

	require.Contains(
		t, notifyTableSQL(t, sqlDB), "PRIMARY KEY (space, repo, endpoint)",
	)
	require.Equal(
		t, []string{"https://a.example", "https://b.example"},
		notifyColumn(t, sqlDB, "endpoint"),
	)
}

// TestNotifyRegistrationAudienceKeysOnAudience checks what moving the key buys:
// re-registering the same service with a different delivery address updates the
// existing registration, which the old endpoint-keyed table could not express.
func TestNotifyRegistrationAudienceKeysOnAudience(t *testing.T) {
	sqlDB := newSQLite(t)
	seedLegacyRegistrations(t, sqlDB, "https://a.example")
	ctx := context.Background()
	_, err := sqlDB.ExecContext(ctx, `ALTER TABLE registrations ADD audience text`)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(
		ctx,
		`UPDATE registrations SET audience='did:web:sap.example#habitat_space_syncer' WHERE endpoint='https://a.example'`,
	)
	require.NoError(t, err)

	requireInTx(t, sqlDB, upNotifyRegistrationAudience)

	// The service now resolves somewhere else: one row, with the new address.
	_, err = sqlDB.ExecContext(
		ctx,
		`UPDATE registrations SET endpoint='https://moved.example' WHERE audience='did:web:sap.example#habitat_space_syncer'`,
	)
	require.NoError(t, err)

	rows, err := sqlDB.QueryContext(
		ctx,
		"SELECT endpoint FROM registrations WHERE audience='did:web:sap.example#habitat_space_syncer'",
	)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var endpoints []string
	for rows.Next() {
		var ep string
		require.NoError(t, rows.Scan(&ep))
		endpoints = append(endpoints, ep)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"https://moved.example"}, endpoints)
}

func notifyColumn(t *testing.T, sqlDB *sql.DB, col string) []string {
	t.Helper()
	rows, err := sqlDB.QueryContext(context.Background(),
		"SELECT "+col+" FROM registrations ORDER BY "+col)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		require.NoError(t, rows.Scan(&v))
		out = append(out, v)
	}
	require.NoError(t, rows.Err())
	return out
}

func notifyTableSQL(t *testing.T, sqlDB *sql.DB) string {
	t.Helper()
	var ddl string
	err := sqlDB.QueryRowContext(context.Background(),
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='registrations'",
	).Scan(&ddl)
	require.NoError(t, err)
	return ddl
}

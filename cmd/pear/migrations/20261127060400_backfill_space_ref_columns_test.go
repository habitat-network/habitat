package migrations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackfillSpaceRefColumnsSqlite(t *testing.T) {
	sqlDB := newSQLite(t)
	ctx := context.Background()
	for _, table := range spaceRefTables {
		_, err := sqlDB.ExecContext(ctx, "CREATE TABLE "+table+" ("+
			"space TEXT, repo TEXT, space_owner TEXT, space_type TEXT, space_skey TEXT)")
		require.NoError(t, err)
	}

	const (
		current = "at://did:plc:o/space/network.habitat.group/k1"
		atSpace = "at:space/did:plc:o/network.habitat.group/k2"
		legacy  = "ats://did:plc:o/network.habitat.group/k3"
	)
	for _, table := range spaceRefTables {
		for i, space := range []string{current, current, atSpace, legacy} {
			_, err := sqlDB.ExecContext(
				ctx,
				"INSERT INTO "+table+" (space, repo) VALUES (?, ?)",
				space,
				"did:plc:r"+string(rune('a'+i)),
			)
			require.NoError(t, err)
		}
		// A row that already has its parts keeps them.
		_, err := sqlDB.ExecContext(ctx, "INSERT INTO "+table+
			" (space, repo, space_owner, space_type, space_skey) VALUES (?, ?, ?, ?, ?)",
			current, "did:plc:keep", "did:plc:kept", "network.habitat.kept", "kept")
		require.NoError(t, err)
	}

	requireInTx(t, sqlDB, upBackfillSpaceRefColumns)
	// Running it again changes nothing.
	requireInTx(t, sqlDB, upBackfillSpaceRefColumns)

	for _, table := range spaceRefTables {
		got := map[string][3]string{}
		rows, err := sqlDB.QueryContext(ctx,
			"SELECT repo, space_owner, space_type, space_skey FROM "+table)
		require.NoError(t, err)
		for rows.Next() {
			var repo string
			var p [3]string
			require.NoError(t, rows.Scan(&repo, &p[0], &p[1], &p[2]))
			got[repo] = p
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())

		require.Equal(
			t,
			[3]string{"did:plc:o", "network.habitat.group", "k1"},
			got["did:plc:ra"],
			table,
		)
		require.Equal(
			t,
			[3]string{"did:plc:o", "network.habitat.group", "k1"},
			got["did:plc:rb"],
			table,
		)
		require.Equal(
			t,
			[3]string{"did:plc:o", "network.habitat.group", "k2"},
			got["did:plc:rc"],
			table,
		)
		require.Equal(
			t,
			[3]string{"did:plc:o", "network.habitat.group", "k3"},
			got["did:plc:rd"],
			table,
		)
		require.Equal(
			t,
			[3]string{"did:plc:kept", "network.habitat.kept", "kept"},
			got["did:plc:keep"],
			table,
		)
	}
}

func TestBackfillSpaceRefColumnsRejectsInvalidURI(t *testing.T) {
	sqlDB := newSQLite(t)
	ctx := context.Background()
	_, err := sqlDB.ExecContext(ctx, "CREATE TABLE space_records ("+
		"space TEXT, space_owner TEXT, space_type TEXT, space_skey TEXT)")
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, "INSERT INTO space_records (space) VALUES ('not a uri')")
	require.NoError(t, err)

	tx, err := sqlDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	require.ErrorContains(t, upBackfillSpaceRefColumns(ctx, tx), "not a uri")
}

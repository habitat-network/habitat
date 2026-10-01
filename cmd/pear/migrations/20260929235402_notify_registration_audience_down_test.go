package migrations_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
)

// downSection returns the statements under a migration's "-- +goose Down"
// marker, so the hand-written Down section is exercised as written rather than
// retyped here.
func downSection(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	_, after, found := strings.Cut(string(raw), "-- +goose Down")
	require.True(t, found, "no Down section in %s", path)
	var stmts []string
	for _, stmt := range strings.Split(after, ";") {
		stmt = strings.TrimSpace(stmt)
		// Drop the comment lines Atlas leaves in the Down section.
		var kept []string
		for _, line := range strings.Split(stmt, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			kept = append(kept, line)
		}
		if joined := strings.TrimSpace(strings.Join(kept, "\n")); joined != "" {
			stmts = append(stmts, joined)
		}
	}
	return stmts
}

// TestAudienceDownRestoresEndpointKeyOnSQLite round-trips the audience migration
// on SQLite: up over a legacy row, then the migration's own Down statements. The
// Down section is hand-written (Atlas's is a no-op comment for a table rebuild),
// so it is read from the file rather than retyped, and applied the way goose
// applies a migration: in one transaction.
func TestAudienceDownRestoresEndpointKeyOnSQLite(t *testing.T) {
	// Forward direction, through the same path pear uses at startup.
	db := pear_testutil.NewPearDB(t)
	require.NoError(t, db.Exec(
		"INSERT INTO registrations (space, repo, audience, endpoint) VALUES (?,?,?,?)",
		"at://did:plc:org/space/network.habitat.group/s1", "",
		"did:web:sync.example.com#habitat_space_syncer", "https://sync.example",
	).Error)

	// Reverse direction, from the migration file itself. goose runs each
	// migration in one transaction, so the steps have to run together rather
	// than one Exec each.
	down := strings.Join(
		downSection(t, "sqlite/20260929235402_notify_registration_audience.sql"), ";\n",
	)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return tx.Exec(down).Error
	}), "down statements:\n%s", down)

	var ddl string
	require.NoError(
		t,
		db.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name='registrations'").
			Row().Scan(&ddl),
	)
	require.Contains(t, ddl, "PRIMARY KEY (`space`, `repo`, `endpoint`)")

	var endpoint string
	require.NoError(t, db.Raw("SELECT endpoint FROM registrations").Row().Scan(&endpoint))
	require.Equal(t, "https://sync.example", endpoint, "endpoint should survive the round trip")
}

// TestDownSectionsExistForBothDialects guards against a hand-edited migration
// losing its Down section, which goose needs to roll back.
func TestDownSectionsExistForBothDialects(t *testing.T) {
	for _, path := range []string{
		"sqlite/20260929235402_notify_registration_audience.sql",
		"postgres/20260929235420_notify_registration_audience.sql",
	} {
		require.NotEmpty(t, downSection(t, path), "%s has no Down statements", path)
	}
}

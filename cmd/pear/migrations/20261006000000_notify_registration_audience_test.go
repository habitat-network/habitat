package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/cmd/pear/migrations"
	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	audienceSpace = "at://did:plc:org/space/network.habitat.group/s1"
	legacyURL     = "https://sync.example"
	syncerRef     = "did:web:sync.example.com#habitat_space_syncer"
)

// schemaColumn reads a column of every row, ordered, for comparison.
func schemaColumn(t *testing.T, db *gorm.DB, table, col string) []string {
	t.Helper()
	var out []string
	require.NoError(t, db.Table(table).Order(col).Pluck(col, &out).Error)
	return out
}

// primaryKeyColumns reports which columns the registrations table is keyed on,
// by reading the key order back out of the schema.
func primaryKeyColumns(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var ddl string
	require.NoError(
		t,
		db.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name='registrations'").
			Row().Scan(&ddl),
	)
	start := strings.Index(ddl, "PRIMARY KEY (")
	require.NotEqual(t, -1, start, "no primary key in: %s", ddl)
	inner := ddl[start+len("PRIMARY KEY ("):]
	end := strings.Index(inner, ")")
	require.NotEqual(t, -1, end, "unterminated primary key in: %s", ddl)
	parts := strings.Split(inner[:end], ", ")
	for i, p := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(p), "`\"")
	}
	return parts
}

// TestSchemaMigrationsProduceAudienceKey covers the shape the SQL migrations
// leave behind: registrations keyed on `audience`, with `endpoint` a plain
// column carrying the delivery address.
func TestSchemaMigrationsProduceAudienceKey(t *testing.T) {
	db := pear_testutil.NewPearDB(t)

	require.Equal(
		t, []string{"space", "repo", "audience"}, primaryKeyColumns(t, db),
		"registrations should be keyed on audience",
	)

	// The table is usable as the model expects.
	space := habitat_syntax.SpaceURI(audienceSpace)
	now := time.Now().Add(time.Hour)
	require.NoError(t, db.Table("registrations").Create(map[string]any{
		"space":      space,
		"repo":       "",
		"audience":   syncerRef,
		"endpoint":   legacyURL,
		"expires_at": now,
	}).Error)

	require.Equal(t, []string{syncerRef}, schemaColumn(t, db, "registrations", "audience"))
	require.Equal(t, []string{legacyURL}, schemaColumn(t, db, "registrations", "endpoint"))
}

// TestSchemaMigrationsKeepDistinctAudiencesAtOneEndpoint covers what keying on
// audience buys: two subscribers resolving to the same address are separate
// registrations, which an endpoint-keyed table could not hold.
func TestSchemaMigrationsKeepDistinctAudiencesAtOneEndpoint(t *testing.T) {
	db := pear_testutil.NewPearDB(t)
	space := habitat_syntax.SpaceURI(audienceSpace)
	now := time.Now().Add(time.Hour)

	other := "did:web:other.example.com#habitat_space_syncer"
	for _, ref := range []string{syncerRef, other} {
		require.NoError(t, db.Table("registrations").Create(map[string]any{
			"space": space, "repo": "", "audience": ref,
			"endpoint": legacyURL, "expires_at": now,
		}).Error)
	}

	require.ElementsMatch(
		t, []string{syncerRef, other}, schemaColumn(t, db, "registrations", "audience"),
	)
	require.Equal(
		t, []string{legacyURL, legacyURL}, schemaColumn(t, db, "registrations", "endpoint"),
	)
}

// TestSchemaMigrationsRejectDuplicateAudience pins that re-registering the same
// service is an update, not a second row — which is what stops a subscriber being
// notified twice.
func TestSchemaMigrationsRejectDuplicateAudience(t *testing.T) {
	db := pear_testutil.NewPearDB(t)
	space := habitat_syntax.SpaceURI(audienceSpace)

	row := map[string]any{
		"space": space, "repo": "", "audience": syncerRef,
		"endpoint": legacyURL, "expires_at": time.Now().Add(time.Hour),
	}
	require.NoError(t, db.Table("registrations").Create(row).Error)

	dup := map[string]any{
		"space": space, "repo": "", "audience": syncerRef,
		"endpoint": "https://other.example", "expires_at": time.Now().Add(time.Hour),
	}
	require.Error(t, db.Table("registrations").Create(dup).Error, "audience must be unique")
}

// TestSchemaMigrationsAcceptLegacyBackfillShape mirrors what the migration does
// to existing rows: a registration that only ever recorded an endpoint has that
// URL as its audience, so it keeps being delivered to exactly as before.
func TestSchemaMigrationsAcceptLegacyBackfillShape(t *testing.T) {
	db := pear_testutil.NewPearDB(t)
	space := habitat_syntax.SpaceURI(audienceSpace)

	require.NoError(t, db.Table("registrations").Create(map[string]any{
		"space": space, "repo": "", "audience": legacyURL,
		"endpoint": legacyURL, "expires_at": time.Now().Add(time.Hour),
	}).Error)

	var audience string
	require.NoError(
		t,
		db.Table("registrations").Where("audience = ?", legacyURL).
			Pluck("audience", &audience).Error,
	)
	require.Equal(t, legacyURL, audience)
}

// TestMigrationsRunAppliesSchema is a smoke test on the entry point the server
// calls, so a broken migration file fails here rather than at pear startup.
func TestMigrationsRunAppliesSchema(t *testing.T) {
	db := pear_testutil.NewPearDB(t)
	// Already applied by NewPearDB, so re-running is a no-op rather than an error.
	require.NoError(t, migrations.Run(
		t.Context(), migrations.PearMigrationContext{DB: db},
	))
}

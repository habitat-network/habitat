package migrations_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
)

func TestForeignInviteDeliveriesMigrationOnSQLite(t *testing.T) {
	db := pear_testutil.NewPearDB(t)
	const table = "foreign_invite_deliveries"
	require.True(t, db.Migrator().HasTable(table))
	require.True(t, db.Migrator().HasIndex(table, "idx_foreign_invite_deliveries_ready_at"))
	require.True(t, db.Migrator().HasIndex(table, "idx_foreign_invite_deliveries_expires_at"))
	down := strings.Join(
		downSection(t, "sqlite/20261127231500_foreign_invite_deliveries.sql"),
		";\n",
	)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(down).Error }))
	require.False(t, db.Migrator().HasTable(table))
	for _, related := range []string{"foreign_invite_records", "foreign_invite_repos", "spaces", "registrations"} {
		require.True(t, db.Migrator().HasTable(related))
	}
}

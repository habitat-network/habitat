package migrations_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/foreignrepo"
)

func TestForeignInviteMigrationOnSQLite(t *testing.T) {
	// Use the same Goose migrations as Pear, not just GORM AutoMigrate.
	db := pear_testutil.NewPearDB(t)
	for _, table := range foreignrepo.Models() {
		require.True(t, db.Migrator().HasTable(table))
	}
	down := strings.Join(downSection(t, "sqlite/20261127230000_foreign_invite_repos.sql"), ";\n")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return tx.Exec(down).Error }))
	for _, table := range foreignrepo.Models() {
		require.False(t, db.Migrator().HasTable(table))
	}
	for _, table := range []string{"spaces", "space_repos", "space_records", "space_remote_records"} {
		require.True(t, db.Migrator().HasTable(table))
	}
}

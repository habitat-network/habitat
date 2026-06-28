package sap

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db?_journal_mode=WAL"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, autoMigrate(db))
	return db
}

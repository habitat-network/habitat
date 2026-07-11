// Package testutil provides helpers for creating throwaway databases in tests.
package testutil

import (
	"path/filepath"
	"testing"

	"github.com/habitat-network/habitat/internal/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// NewDB returns a gorm DB backed by a temporary SQLite file living in the
// test's temp dir (removed automatically when the test finishes).
//
// The file is opened in WAL journal mode with a busy timeout, so the tests can
// use gorm's connection pool for concurrent reads and writes without hitting
// "database is locked" errors — unlike a ":memory:" database, where each pooled
// connection would see a separate, empty database.
func NewDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := db.New(
		"sqlite://" + path,
	)
	require.NoError(t, err)
	return db
}

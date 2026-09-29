// Package testutil provides helpers for creating throwaway databases in tests.
//
// It deliberately imports no store package, so a store's own tests can use it
// while still testing that package from the inside. For a database with the
// whole pear schema, use cmd/pear/testutil.NewPearDB instead.
package testutil

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/habitat-network/habitat/internal/db"
)

// NewDB returns a gorm DB backed by a temporary SQLite file living in the
// test's temp dir (removed automatically when the test finishes), with tables
// created for every model in the given sets. Pass the Models function of each
// store the test constructs; a store's tables are no longer created by its
// constructor.
//
// The file is opened in WAL journal mode with a busy timeout, so the tests can
// use gorm's connection pool for concurrent reads and writes without hitting
// "database is locked" errors — unlike a ":memory:" database, where each pooled
// connection would see a separate, empty database.
//
// gorm logs are routed through t.Logf so they only appear when the test runs
// verbosely or fails.
func NewDB(t *testing.T, modelSets ...[]any) *gorm.DB {
	t.Helper()
	d := NewUnmigratedDB(t)
	// No goose migrations: the tables come from the models, and pear's own
	// migrations are applied by cmd/pear/testutil.NewPearDB.
	require.NoError(t, db.Migrate(t.Context(), d, nil, modelSets...))
	return d
}

// NewUnmigratedDB is like [NewDB] but creates no tables, for tests whose
// stores migrate themselves.
func NewUnmigratedDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.New("sqlite://" + path)
	require.NoError(t, err)
	d.Logger = logger.New(testLog{t: t}, logger.Config{
		LogLevel:                  logger.Info,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      false,
		Colorful:                  true,
	})
	return d
}

// testLog routes gorm's log output through t.Logf.
type testLog struct {
	t *testing.T
}

func (w testLog) Printf(format string, args ...any) {
	w.t.Logf(format, args...)
}

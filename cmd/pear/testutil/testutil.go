// Package testutil provides helpers for creating throwaway databases that hold
// pear's full schema.
package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/cmd/pear/migrations"
	"github.com/habitat-network/habitat/internal/db"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
)

// NewPearDB returns a gorm DB backed by a temporary SQLite file living in the
// test's temp dir (removed automatically when the test finishes), migrated for
// every store pear persists to. Any store a test builds will find its tables
// already created.
//
// It lives here rather than in internal/db/testutil because it goes through
// cmd/pear/migrations.Models, which imports all 13 pear stores — and a store's
// own in-package tests cannot import a package that imports them.
// internal/db/testutil itself imports no store, so those tests can still use it.
func NewPearDB(t *testing.T) *gorm.DB {
	t.Helper()
	d := db_testutil.NewDB(t)
	require.NoError(t, db.Migrate(t.Context(), d, migrations.FS, migrations.Models()))
	return d
}

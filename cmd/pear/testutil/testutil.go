// Package testutil provides helpers for creating throwaway databases that hold
// pear's full schema.
package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/cmd/pear/migrations"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
)

// NewPearDB returns a gorm DB backed by a temporary SQLite file living in the
// test's temp dir (removed automatically when the test finishes), with all of
// pear's migrations applied. Any store a test builds will find its tables
// already created, exactly as in production.
//
// It lives here rather than in internal/db/testutil because cmd/pear/migrations
// imports all 13 pear stores — and a store's own in-package tests cannot import
// a package that imports them.
// internal/db/testutil itself imports no store, so those tests can still use it.
func NewPearDB(t *testing.T) *gorm.DB {
	t.Helper()
	d := db_testutil.NewDB(t)
	require.NoError(t, migrations.Run(t.Context(), migrations.PearMigrationContext{DB: d}))
	return d
}

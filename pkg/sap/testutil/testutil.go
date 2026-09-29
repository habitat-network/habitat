// Package testutil provides helpers for creating throwaway databases that hold
// sap's schema.
package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/db"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/pkg/sap/schema"
)

// NewSapDB returns a gorm DB backed by a temporary SQLite file living in the
// test's temp dir (removed automatically when the test finishes), migrated for
// every store sap persists to. It is the sap counterpart to
// cmd/pear/testutil.NewPearDB, and migrates the same list cmd/sap does at
// startup, via [schema.Models].
//
// It uses the default table names, where cmd/sap prefixes its tables with
// `sap_`. Tests that assert on table names, or that share a database with a
// pear server, should account for that difference.
func NewSapDB(t *testing.T) *gorm.DB {
	t.Helper()
	d := db_testutil.NewDB(t)
	// sap has no goose migrations, so Migrate only creates the tables.
	require.NoError(t, db.Migrate(t.Context(), d, nil, schema.Models()))
	return d
}

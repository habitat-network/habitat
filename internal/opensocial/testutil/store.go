// Package testutil provides helpers for constructing an opensocial.Store
// backed by throwaway storage, for use in tests.
package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

// NewTestStore returns an opensocial.Store and the spaces.Store it shares a
// DB with, so callers can seed or inspect repo records directly.
func NewTestStore(t *testing.T) (opensocial.Store, spaces.Store) {
	t.Helper()
	db := db_testutil.NewDB(t)
	return NewTestStoreWithDB(t, db)
}

// NewTestStoreWithDB is like NewTestStore, but shares the given DB rather
// than creating a new one.
func NewTestStoreWithDB(t *testing.T, db *gorm.DB) (opensocial.Store, spaces.Store) {
	t.Helper()
	spacesStore := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db))
	blobStore := spaces_testutil.NewTestBlobStore(t)
	hve, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)
	s, err := opensocial.NewStore(db, spacesStore, blobStore, hve)
	require.NoError(t, err)
	return s, spacesStore
}

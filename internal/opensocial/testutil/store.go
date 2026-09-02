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
	"github.com/habitat-network/habitat/internal/utils"
)

type TestStore struct {
	*opensocial.Store
	SpaceStore spaces.Store
	DB         *gorm.DB
	Hive       hive.Hive
	BlobStore  spaces.BlobStore
}

func WithDB(db *gorm.DB) utils.Opt[TestStore] {
	return func(o *TestStore) {
		o.DB = db
	}
}

func WithSpaceStore(store spaces.Store) utils.Opt[TestStore] {
	return func(o *TestStore) {
		o.SpaceStore = store
	}
}

func WithHive(hive hive.Hive) utils.Opt[TestStore] {
	return func(o *TestStore) {
		o.Hive = hive
	}
}

func WithBlobStore(store spaces.BlobStore) utils.Opt[TestStore] {
	return func(o *TestStore) {
		o.BlobStore = store
	}
}

// NewTestStore returns an opensocial.Store and the spaces.Store it shares a
// DB with, so callers can seed or inspect repo records directly.
func NewTestStore(t *testing.T, opts ...utils.Opt[TestStore]) *TestStore {
	t.Helper()
	testStore := utils.ResolveOptions(TestStore{
		BlobStore: spaces_testutil.NewTestBlobStore(t),
	}, opts)
	if testStore.DB == nil {
		testStore.DB = db_testutil.NewDB(t)
	}
	if testStore.SpaceStore == nil {
		testStore.SpaceStore = spaces_testutil.NewTestStore(
			t,
			spaces_testutil.WithDB(testStore.DB),
		)
	}
	if testStore.Hive == nil {
		hve, err := hive.NewHive("example.com", "pear.example.com", testStore.DB)
		require.NoError(t, err)
		testStore.Hive = hve
	}
	store, err := opensocial.NewStore(
		testStore.DB,
		testStore.SpaceStore,
		testStore.BlobStore,
		testStore.Hive,
	)
	require.NoError(t, err)
	testStore.Store = store
	return &testStore
}

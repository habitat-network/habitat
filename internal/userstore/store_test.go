package userstore

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEnsureUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewUserStore(db)
	require.NoError(t, err)

	did := syntax.DID("did:example:alice")

	exists, err := store.CheckUserExists(did)
	require.NoError(t, err)
	require.False(t, exists)

	// First call should create the user
	err = store.EnsureUser(did)
	require.NoError(t, err)

	// Verify user exists
	exists, err = store.CheckUserExists(did)
	require.NoError(t, err)
	require.True(t, exists)

	// Second call should be idempotent (no error, user still exists)
	err = store.EnsureUser(did)
	require.NoError(t, err)

	exists, err = store.CheckUserExists(did)
	require.NoError(t, err)
	require.True(t, exists)
}

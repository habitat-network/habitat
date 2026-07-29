package pdsclient

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/stretchr/testify/require"
)

func TestCurrentSessionStore_SetAndGet(t *testing.T) {
	store, err := newCurrentSessionStore(testutil.NewDB(t))
	require.NoError(t, err)

	did := syntax.DID("did:web:example.com")
	require.NoError(t, store.Set(context.Background(), did, "sess1"))

	got, err := store.Get(context.Background(), did)
	require.NoError(t, err)
	require.Equal(t, "sess1", got)
}

func TestCurrentSessionStore_SetOverwritesPrevious(t *testing.T) {
	store, err := newCurrentSessionStore(testutil.NewDB(t))
	require.NoError(t, err)

	did := syntax.DID("did:web:example.com")
	require.NoError(t, store.Set(context.Background(), did, "sess1"))
	require.NoError(t, store.Set(context.Background(), did, "sess2"))

	got, err := store.Get(context.Background(), did)
	require.NoError(t, err)
	require.Equal(t, "sess2", got)
}

func TestCurrentSessionStore_GetNotFound(t *testing.T) {
	store, err := newCurrentSessionStore(testutil.NewDB(t))
	require.NoError(t, err)

	_, err = store.Get(context.Background(), "did:web:missing.com")
	require.Error(t, err)
}

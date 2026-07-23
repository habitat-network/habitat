package pdscred

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitatdb "github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/stretchr/testify/require"
)

func TestGetDpopClient_Success(t *testing.T) {
	db := testutil.NewDB(t)
	store, err := NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)
	require.NoError(t, habitatdb.AutoMigrate(db, store))

	// Generate test dpop key
	dpopKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create test DID
	did := syntax.DID("did:plc:test123")

	err = store.UpsertCredentials(t.Context(), did, &Credentials{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		DpopKey:      dpopKey,
	})
	require.NoError(t, err)

	// handles updates
	err = store.UpsertCredentials(t.Context(), did, &Credentials{
		AccessToken:  "test-access-token-2",
		RefreshToken: "test-refresh-token-2",
		DpopKey:      dpopKey,
	})
	require.NoError(t, err)

	// Get DPoP client
	credentials, err := store.GetCredentials(t.Context(), did)
	require.NoError(t, err)
	require.Equal(t, "test-access-token-2", credentials.AccessToken)
	require.Equal(t, "test-refresh-token-2", credentials.RefreshToken)
	require.Equal(t, dpopKey, credentials.DpopKey)
}

func TestGetDpopClient_NotFound(t *testing.T) {
	db := testutil.NewDB(t)
	store, err := NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)
	require.NoError(t, habitatdb.AutoMigrate(db, store))

	credentials, err := store.GetCredentials(t.Context(), syntax.DID("did:plc:nonexistent"))
	require.Error(t, err)
	require.Nil(t, credentials)
	require.Contains(t, err.Error(), "user credentials not found")
}

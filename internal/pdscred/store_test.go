package pdscred

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/eagraf/habitat-new/internal/encrypt"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGetDpopClient_Success(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)

	// Generate test dpop key
	dpopKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create test DID
	did, err := syntax.ParseDID("did:plc:test123")
	require.NoError(t, err)

	err = store.UpsertCredentials(did, &Credentials{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		DpopKey:      dpopKey,
	})
	require.NoError(t, err)

	// handles updates
	err = store.UpsertCredentials(did, &Credentials{
		AccessToken:  "test-access-token-2",
		RefreshToken: "test-refresh-token-2",
		DpopKey:      dpopKey,
	})
	require.NoError(t, err)

	// Get DPoP client
	credentials, err := store.GetCredentials(did)
	require.NoError(t, err)
	require.Equal(t, "test-access-token-2", credentials.AccessToken)
	require.Equal(t, "test-refresh-token-2", credentials.RefreshToken)
	require.Equal(t, dpopKey, credentials.DpopKey)
}

func TestGetDpopClient_NotFound(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)

	// Try to get client for non-existent DID
	did, err := syntax.ParseDID("did:plc:nonexistent")
	require.NoError(t, err)

	credentials, err := store.GetCredentials(did)
	require.Error(t, err)
	require.Nil(t, credentials)
	require.Contains(t, err.Error(), "user credentials not found")
}

package pdscred

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/eagraf/habitat-new/internal/encrypt"
	"github.com/eagraf/habitat-new/internal/oauthclient"
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
	dpopKeyBytes, err := dpopKey.Bytes()
	require.NoError(t, err)

	// Create test DID
	did, err := syntax.ParseDID("did:plc:test123")
	require.NoError(t, err)

	err = store.UpsertCredentials(did, dpopKeyBytes, &oauthclient.TokenResponse{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		TokenType:    "DPoP",
		Scope:        "atproto",
	})
	require.NoError(t, err)

	// handles updates
	err = store.UpsertCredentials(did, dpopKeyBytes, &oauthclient.TokenResponse{
		AccessToken:  "test-access-token-2",
		RefreshToken: "test-refresh-token-2",
		TokenType:    "DPoP",
		Scope:        "atproto",
	})
	require.NoError(t, err)

	// Get DPoP client
	client, err := store.GetDpopClient(did)
	require.NoError(t, err)
	require.NotNil(t, client)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "DPoP test-access-token-2", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	req, err := http.NewRequest("GET", server.URL, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestGetDpopClient_NotFound(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	store, err := NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)

	// Try to get client for non-existent DID
	did, err := syntax.ParseDID("did:plc:nonexistent")
	require.NoError(t, err)

	client, err := store.GetDpopClient(did)
	require.Error(t, err)
	require.Nil(t, client)
	require.Contains(t, err.Error(), "user credentials not found")
}

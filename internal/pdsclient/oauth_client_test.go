package pdsclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newTestClient(t *testing.T, db *gorm.DB, secret string) PdsOAuthClient {
	t.Helper()
	client, err := NewClient(
		db,
		"https://app.example.com/client-metadata.json",
		"https://app.example.com",
		"https://app.example.com/oauth-callback",
		secret,
	)
	require.NoError(t, err)
	return client
}

func TestNewClient(t *testing.T) {
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)

	client := newTestClient(t, testutil.NewDB(t), secret)
	require.NotNil(t, client)

	meta := client.ClientMetadata()
	assert.Equal(t, "https://app.example.com/client-metadata.json", meta.ClientID)
	assert.True(t, meta.DPoPBoundAccessTokens)
	assert.Contains(t, meta.RedirectURIs, "https://app.example.com/oauth-callback")
	require.NotNil(t, meta.ClientName)
	assert.Equal(t, "Habitat", *meta.ClientName)
}

func TestNewClientInvalidSecret(t *testing.T) {
	_, err := NewClient(
		testutil.NewDB(t),
		"https://app.example.com/client-metadata.json",
		"https://app.example.com",
		"https://app.example.com/oauth-callback",
		"not-valid-base64!!!",
	)
	require.Error(t, err)
}

func TestAuthorizeInvalidIdentifier(t *testing.T) {
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	client := newTestClient(t, testutil.NewDB(t), secret)

	_, err = client.Authorize(context.Background(), "not a valid handle!!!")
	require.Error(t, err)
}

func TestExchangeCodeMissingState(t *testing.T) {
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	client := newTestClient(t, testutil.NewDB(t), secret)

	// No pending auth request matches an empty state, so the callback fails.
	_, err = client.ExchangeCode(context.Background(), "code", "https://pds.example.com", "")
	require.Error(t, err)
}

func TestDoNoCurrentSession(t *testing.T) {
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	client := newTestClient(t, testutil.NewDB(t), secret)

	req, err := http.NewRequest(http.MethodGet, "/xrpc/com.atproto.repo.getRecord", nil)
	require.NoError(t, err)
	resp, err := client.Do(context.Background(), "did:web:example.com", req)
	require.Error(t, err)
	if resp != nil {
		_ = resp.Body.Close()
	}
}

func TestDo(t *testing.T) {
	db := testutil.NewDB(t)
	secretStr, err := encrypt.GenerateKey()
	require.NoError(t, err)

	// Seed a session and mark it as the current one for the account, mirroring
	// what ExchangeCode does on a real callback. The store and pointer table
	// share the db with the client created below.
	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)
	currentSessions, err := newCurrentSessionStore(db)
	require.NoError(t, err)
	dpopKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)

	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	did := syntax.DID("did:web:example.com")
	require.NoError(t, store.SaveSession(context.Background(), oauth.ClientSessionData{
		AccountDID:              did,
		SessionID:               "sess1",
		HostURL:                 srv.URL,
		AccessToken:             "dummy-access-token",
		DPoPPrivateKeyMultibase: dpopKey.Multibase(),
	}))
	require.NoError(t, currentSessions.Set(context.Background(), did, "sess1"))

	client := newTestClient(t, db, secretStr)
	req, err := http.NewRequest(http.MethodGet, "/xrpc/com.atproto.repo.getRecord", nil)
	require.NoError(t, err)
	resp, err := client.Do(context.Background(), did, req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "/xrpc/com.atproto.repo.getRecord", gotPath)
	// The client authenticates the request with a DPoP-bound token.
	assert.Contains(t, gotAuth, "DPoP")
}

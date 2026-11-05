package pdsoauth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/stretchr/testify/require"
)

const testDID syntax.DID = "did:plc:abcdefghijklmnopqrstuvwx"

func newTestApp(t *testing.T, store oauth.ClientAuthStore) *App {
	t.Helper()
	app, err := NewApp(Config{
		ClientID:    "https://pear.example.com/client-metadata.json",
		ClientURI:   "https://pear.example.com",
		CallbackURL: "https://pear.example.com/oauth-callback",
		Secret:      testSecret(t),
	}, store, identity.DefaultDirectory())
	require.NoError(t, err)
	return app
}

func saveSession(t *testing.T, s *Store, host, sessionID, accessToken string) {
	t.Helper()
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	require.NoError(t, s.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:              testDID,
		SessionID:               sessionID,
		HostURL:                 host,
		AccessToken:             accessToken,
		RefreshToken:            "refresh",
		DPoPPrivateKeyMultibase: key.Multibase(),
	}))
}

func TestClientFactorySignsRequestsForPDS(t *testing.T) {
	var gotAuth, gotDPoP, gotPath, gotBody string
	pds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotDPoP, gotPath = r.Header.Get("Authorization"), r.Header.Get("DPoP"), r.URL.RequestURI()
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer pds.Close()

	store, _ := newTestStore(t)
	saveSession(t, store, pds.URL, "s1", "token-1")
	factory, err := NewHttpClientFactory(newTestApp(t, store))
	require.NoError(t, err)

	do := func() {
		client, err := factory.NewClient(t.Context(), testDID)
		require.NoError(t, err)
		// Relative URLs resolve against the member's PDS, as the forwarding
		// paths send them.
		req, err := http.NewRequestWithContext(
			t.Context(), http.MethodPost, "/xrpc/com.example.thing?a=b", strings.NewReader("payload"),
		)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}

	do()
	require.Equal(t, "DPoP token-1", gotAuth)
	require.NotEmpty(t, gotDPoP)
	require.Equal(t, "/xrpc/com.example.thing?a=b", gotPath)
	require.Equal(t, "payload", gotBody)

	// Signing in again replaces the session; the factory must not keep using
	// the cached one.
	saveSession(t, store, pds.URL, "s2", "token-2")
	do()
	require.Equal(t, "DPoP token-2", gotAuth)
}

func TestClientFactoryNoSession(t *testing.T) {
	store, _ := newTestStore(t)
	factory, err := NewHttpClientFactory(newTestApp(t, store))
	require.NoError(t, err)
	_, err = factory.NewClient(t.Context(), testDID)
	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestAppClientMetadata(t *testing.T) {
	store, _ := newTestStore(t)
	m := newTestApp(t, store).ClientMetadata()
	require.NoError(t, m.Validate(m.ClientID))
	require.True(t, m.IsConfidential())
	require.Equal(t, "private_key_jwt", m.TokenEndpointAuthMethod)
	require.Equal(t, "atproto transition:generic", m.Scope)
	require.Equal(t, []string{"https://pear.example.com/oauth-callback"}, m.RedirectURIs)
	require.Len(t, m.JWKS.Keys, 1)
	require.True(t, m.DPoPBoundAccessTokens)
}

func testSecret(t *testing.T) string {
	t.Helper()
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	return secret
}

package oauthserver

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	dbtestutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	login_testutil "github.com/habitat-network/habitat/internal/login/testutil"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

func registerTestClient(t *testing.T, srv *OAuthServer, clientID string, redirectURIs []string) {
	t.Helper()
	uris, err := json.Marshal(redirectURIs)
	require.NoError(t, err)
	grants, err := json.Marshal([]string{"authorization_code"})
	require.NoError(t, err)
	require.NoError(t, srv.storage.createRegisteredClient(t.Context(), &RegisteredClient{
		ClientID: clientID, RedirectURIs: uris, GrantTypes: grants,
	}))
}

func TestNormalizeLoopbackRedirect(t *testing.T) {
	key, err := encrypt.GenerateKey()
	require.NoError(t, err)
	secretBytes, err := encrypt.ParseKey(key)
	require.NoError(t, err)
	oauthSrv, err := NewOAuthServer(
		secretBytes, &org.LoginRouter{Pds: login_testutil.NewPassthroughProvider(t)},
		pdsclient.NewDummyDirectory("http://pds.url"), dbtestutil.NewDB(t), noop.Meter{}, testStore(t),
		"https://habitat.example", NewJWTBearerStore(), testOpensocialStore(t),
		nil, testBroker(t), oauth.ClientMetadata{},
	)
	require.NoError(t, err)

	// Mirrors Claude Code's client metadata: portless localhost + 127.0.0.1.
	registerTestClient(t, oauthSrv, "native", []string{"http://localhost/callback", "http://127.0.0.1/callback"})
	registerTestClient(t, oauthSrv, "exact", []string{"http://localhost:3000/cb"})

	form := url.Values{"client_id": {"native"}, "redirect_uri": {"http://localhost:56393/callback"}}
	oauthSrv.normalizeLoopbackRedirect(t.Context(), form)
	require.Equal(t, "http://127.0.0.1:56393/callback", form.Get("redirect_uri"))

	form = url.Values{"client_id": {"exact"}, "redirect_uri": {"http://localhost:3000/cb"}}
	oauthSrv.normalizeLoopbackRedirect(t.Context(), form)
	require.Equal(t, "http://localhost:3000/cb", form.Get("redirect_uri"))
}

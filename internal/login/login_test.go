package login

import (
	"context"
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	oauthtestutil "github.com/habitat-network/habitat/pkg/oauthclient/testutil"
	"github.com/stretchr/testify/require"
)

// --- pdsProvider ---

// newTestApp builds an *oauth.ClientApp wired to a FakePDS at
// "https://pds.example.com", so StartAuthFlow/ProcessCallback can run a real
// round trip against it.
func newTestApp(t *testing.T) (*oauth.ClientApp, *oauthtestutil.FakePDS) {
	t.Helper()
	cfg := oauth.NewPublicConfig(
		"https://app.example.com/client-metadata.json",
		"https://app.example.com/oauth-callback",
		[]string{"atproto", "transition:generic"},
	)
	app := oauth.NewClientApp(&cfg, oauth.NewMemStore())
	fakePDS := oauthtestutil.NewFakePDS(t, "https://pds.example.com")
	fakePDS.Configure(app)
	return app, fakePDS
}

func TestPDSProvider_Authorize(t *testing.T) {
	app, _ := newTestApp(t)
	p := NewPDSProvider(app)

	redirect, _, err := p.Authorize(
		context.Background(),
		oauthtestutil.FakeDID.String(),
	)
	require.NoError(t, err)
	require.Contains(t, redirect, "/oauth/authorize")
}

func TestPDSProvider_Authorize_EmptyHint(t *testing.T) {
	p := NewPDSProvider(nil)

	_, _, err := p.Authorize(context.Background(), "")
	require.Error(t, err)
}

func TestPDSProvider_Exchange(t *testing.T) {
	app, _ := newTestApp(t)
	p := NewPDSProvider(app)

	redirect, _, err := p.Authorize(context.Background(), oauthtestutil.FakeDID.String())
	require.NoError(t, err)

	// Follow the auth server's redirect (simulating user approval) to obtain
	// the callback query params, without following it all the way to our
	// (nonexistent, in this test) callback handler.
	app.Client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := app.Client.Get(redirect)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc, err := resp.Location()
	require.NoError(t, err)

	loginID, err := p.Exchange(t.Context(), loc.Query(), nil)
	require.NoError(t, err)
	require.Equal(t, oauthtestutil.FakeDID.String(), loginID)
}

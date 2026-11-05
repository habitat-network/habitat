package login

import (
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/keyval"
	keyval_testutil "github.com/habitat-network/habitat/internal/keyval/testutil"
	"github.com/habitat-network/habitat/internal/pdsoauth"
	"github.com/stretchr/testify/require"
)

func newTestPDSProvider(t *testing.T) Provider {
	t.Helper()
	kv := keyval_testutil.New(t)
	enc, err := keyval.NewEncrypted(kv, encrypt.TestKey)
	require.NoError(t, err)
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	app, err := pdsoauth.NewApp(
		pdsoauth.Config{
			ClientID:    "https://pear.example.com/client-metadata.json",
			ClientURI:   "https://pear.example.com",
			CallbackURL: "https://pear.example.com/oauth-callback",
			Secret:      secret,
		},
		pdsoauth.NewStore(enc, kv, kv.Namespace("requests")),
		identity.NewMockDirectory(),
	)
	require.NoError(t, err)
	return NewPDSProvider(app)
}

func TestPDSProviderAuthorizeUnknownIdentity(t *testing.T) {
	p := newTestPDSProvider(t)
	_, _, _, err := p.Authorize(t.Context(), "did:plc:abcdefghijklmnopqrstuvwx")
	require.Error(t, err)
}

// The callback of a flow that was never started, or was already completed,
// has no pending authorization to redeem.
func TestPDSProviderExchangeUnknownState(t *testing.T) {
	p := newTestPDSProvider(t)
	_, _, err := p.Exchange(
		t.Context(),
		url.Values{"state": {"unknown"}, "code": {"c"}, "iss": {"https://pds.example.com"}},
		"",
	)
	require.Error(t, err)
}

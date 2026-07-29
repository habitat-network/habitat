package forwarding

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/stretchr/testify/require"
)

// newTestSessionGetter returns a SessionGetter with a session pre-seeded for
// did, resolvable against pdsURL.
func newTestSessionGetter(t *testing.T, did syntax.DID, pdsURL string) *oauthclient.SessionGetter {
	t.Helper()
	store, err := oauthclient.NewGormStore(testutil.NewDB(t), oauthclient.WithSingleSessionPerDID())
	require.NoError(t, err)
	dpopKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	require.NoError(t, store.SaveSession(context.Background(), oauth.ClientSessionData{
		AccountDID:              did,
		SessionID:               "sess1",
		HostURL:                 pdsURL,
		AccessToken:             "dummy-access-token",
		DPoPPrivateKeyMultibase: dpopKey.Multibase(),
	}))
	cfg := oauth.NewPublicConfig(
		"https://app.example.com/client-metadata.json",
		"https://app.example.com/oauth-callback",
		[]string{"atproto"},
	)
	return oauthclient.NewSessionGetter(oauth.NewClientApp(&cfg, store))
}

// fakePDSServer returns a test server that records the last request path it received.
func fakePDSServer(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var lastPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &lastPath
}

func newTestForwarding(t *testing.T, fakePDS *httptest.Server) *PDSForwarding {
	t.Helper()
	dir := pdsclient.NewDummyDirectory(fakePDS.URL)
	return &PDSForwarding{
		dir:             dir,
		plainHTTPClient: fakePDS.Client(),
	}
}

func TestServeHTTP_MissingTargetParam(t *testing.T) {
	fakePDS, _ := fakePDSServer(t)
	p := newTestForwarding(t, fakePDS)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.repo.getRecord", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeHTTP_InvalidAtIdentifier(t *testing.T) {
	fakePDS, _ := fakePDSServer(t)
	p := newTestForwarding(t, fakePDS)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/com.atproto.repo.getRecord?repo=not-a-valid-did-or-handle!!!",
		nil,
	)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeHTTP_ForwardsToTargetPDS(t *testing.T) {
	fakePDS, lastPath := fakePDSServer(t)
	p := newTestForwarding(t, fakePDS)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/com.atproto.repo.getRecord?repo=did:plc:abc123&collection=app.bsky.feed.post&rkey=abc",
		nil,
	)
	// Strip Authorization to confirm it isn't forwarded (security check)
	req.Header.Set("Authorization", "Bearer secret-token")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body, err := io.ReadAll(w.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(body))
	require.Equal(t, "/xrpc/com.atproto.repo.getRecord", *lastPath)
}

func TestServeHTTP_ForwardsToCallerPDS(t *testing.T) {
	fakePDS, lastPath := fakePDSServer(t)

	callerDID := syntax.DID("did:plc:caller123")
	p := &PDSForwarding{
		oauth:           authntest.NewSuccessMethod(callerDID),
		sessionGetter:   newTestSessionGetter(t, callerDID, fakePDS.URL),
		plainHTTPClient: fakePDS.Client(),
	}

	req := httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.repo.uploadBlob", nil)
	req.Header.Set("Authorization", "Bearer caller-token")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body, err := io.ReadAll(w.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(body))
	require.Equal(t, "/xrpc/com.atproto.repo.uploadBlob", *lastPath)
}

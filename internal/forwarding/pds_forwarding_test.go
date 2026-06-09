package forwarding

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
)

// fakePDSServer returns a test server that records the last request path it received.
func fakePDSServer(t *testing.T) (server *httptest.Server, path *string) {
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

	req := httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.repo.getRecord", http.NoBody)
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
		http.NoBody,
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
		http.NoBody,
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
		oauth:            authn.NewStubAuthnForTest(callerDID),
		pdsClientFactory: pdsclient.NewDummyClientFactory(fakePDS.URL),
		plainHTTPClient:  fakePDS.Client(),
	}

	req := httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.repo.uploadBlob", http.NoBody)
	req.Header.Set("Authorization", "Bearer caller-token")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body, err := io.ReadAll(w.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(body))
	require.Equal(t, "/xrpc/com.atproto.repo.uploadBlob", *lastPath)
}

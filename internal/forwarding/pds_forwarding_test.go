package forwarding

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
)

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

func newTestForwardingWithClient(t *testing.T, fakePDS *httptest.Server, client pdsclient.PdsOAuthClient) *PDSForwarding {
	t.Helper()
	dir := pdsclient.NewDummyDirectory(fakePDS.URL)
	return &PDSForwarding{
		client:          client,
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

package mcpauth

import (
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/keyval"
	keyval_testutil "github.com/habitat-network/habitat/internal/keyval/testutil"
)

const (
	testRedirectURL = "https://pear.example.com/mcp/oauth/callback"
	testOrg         = syntax.DID("did:web:org.example.com")
	testMember      = syntax.DID("did:plc:member")
)

// testStores are the keyval stores a Service persists through, shared by
// every Service a test builds over them, as pear instances share a database.
type testStores struct {
	secrets keyval.EncryptedStore
	plain   keyval.Store
}

func newTestStores(t *testing.T) testStores {
	t.Helper()
	plain := keyval_testutil.New(t)
	secrets, err := keyval.NewEncrypted(plain, encrypt.TestKey)
	require.NoError(t, err)
	return testStores{secrets: secrets.Namespace("mcp_oauth"), plain: plain.Namespace("mcp_oauth")}
}

func newTestService(t *testing.T, stores testStores, f *fakeAuthServer, cfg Config) Service {
	t.Helper()
	if cfg.RedirectURL == "" {
		cfg.RedirectURL = testRedirectURL
	}
	svc, err := New(
		stores.secrets,
		stores.plain,
		NewDiscoverer(f.httpClient()),
		f.httpClient(),
		cfg,
	)
	require.NoError(t, err)
	return svc
}

func testServer(f *fakeAuthServer) Server {
	return Server{Org: testOrg, ID: "linear", URL: f.serverURL()}
}

// connect runs a member through the whole authorization flow.
func connect(t *testing.T, svc Service, f *fakeAuthServer, server Server) {
	t.Helper()
	authURL, err := svc.Begin(t.Context(), server, testMember)
	require.NoError(t, err)
	state, code := f.approve(t, authURL)
	conn, err := svc.Complete(t.Context(), Callback{State: state, Code: code, Iss: f.issuer()})
	require.NoError(t, err)
	require.Equal(t, server, conn.Server)
	require.Equal(t, testMember, conn.Member)
}

func TestServiceConnectsAndRefreshes(t *testing.T) {
	f := newFakeAuthServer(t, func(f *fakeAuthServer) {
		f.rotateRefresh = true
		f.issSupported = true
	})
	svc := newTestService(t, newTestStores(t), f, Config{ClientName: "Habitat"})
	server := testServer(f)

	status, err := svc.Status(t.Context(), server, testMember)
	require.NoError(t, err)
	require.Equal(t, StatusNotConnected, status)

	authURL, err := svc.Begin(t.Context(), server, testMember)
	require.NoError(t, err)
	q := mustQuery(t, authURL)
	require.Equal(t, testRedirectURL, q.Get("redirect_uri"))
	// The challenge's scopes, plus offline_access for a refresh token.
	require.Equal(t, "read write offline_access", q.Get("scope"))

	state, code := f.approve(t, authURL)
	_, err = svc.Complete(t.Context(), Callback{State: state, Code: code, Iss: f.issuer()})
	require.NoError(t, err)

	status, err = svc.Status(t.Context(), server, testMember)
	require.NoError(t, err)
	require.Equal(t, StatusConnected, status)

	// The token from the code exchange is cached.
	tok, err := svc.AccessToken(t.Context(), server, testMember)
	require.NoError(t, err)
	require.Equal(t, "access-2", tok)
	require.Zero(t, f.refreshCount())

	// A server rejecting it forces a refresh, which rotates the refresh
	// token; the rotated one must have been kept for the next refresh.
	require.NoError(t, svc.Invalidate(t.Context(), server, testMember))
	tok, err = svc.AccessToken(t.Context(), server, testMember)
	require.NoError(t, err)
	require.Equal(t, "access-3", tok)
	require.NoError(t, svc.Invalidate(t.Context(), server, testMember))
	tok, err = svc.AccessToken(t.Context(), server, testMember)
	require.NoError(t, err)
	require.Equal(t, "access-4", tok)
	require.Equal(t, 2, f.refreshCount())

	// Connecting again reuses pear's registration with the server.
	connect(t, svc, f, server)
	require.Equal(t, 1, f.registrationCount())
}

func TestServiceKeepsRefreshTokenWhenNotRotated(t *testing.T) {
	f := newFakeAuthServer(t, nil)
	svc := newTestService(t, newTestStores(t), f, Config{})
	server := testServer(f)
	connect(t, svc, f, server)

	for range 2 {
		require.NoError(t, svc.Invalidate(t.Context(), server, testMember))
		_, err := svc.AccessToken(t.Context(), server, testMember)
		require.NoError(t, err)
	}
	require.Equal(t, 2, f.refreshCount())
}

func TestServiceCompleteRejectsBadCallbacks(t *testing.T) {
	f := newFakeAuthServer(t, func(f *fakeAuthServer) { f.issSupported = true })
	svc := newTestService(t, newTestStores(t), f, Config{})
	server := testServer(f)

	_, err := svc.Complete(t.Context(), Callback{State: "unknown", Code: "x"})
	require.ErrorIs(t, err, ErrFlowNotFound)

	begin := func() (string, string) {
		authURL, err := svc.Begin(t.Context(), server, testMember)
		require.NoError(t, err)
		return f.approve(t, authURL)
	}

	state, code := begin()
	_, err = svc.Complete(
		t.Context(),
		Callback{State: state, Code: code, Iss: "https://evil.example.com"},
	)
	require.ErrorIs(t, err, ErrIssuerMismatch)
	// A request can't be retried once used, even unsuccessfully.
	_, err = svc.Complete(t.Context(), Callback{State: state, Code: code, Iss: f.issuer()})
	require.ErrorIs(t, err, ErrFlowNotFound)

	state, code = begin()
	_, err = svc.Complete(t.Context(), Callback{State: state, Code: code})
	require.ErrorIs(t, err, ErrIssuerMismatch, "iss is required when the server advertises it")

	state, _ = begin()
	_, err = svc.Complete(t.Context(), Callback{State: state, Error: "access_denied"})
	require.ErrorIs(t, err, ErrAuthorizationDenied)

	state, _ = begin()
	_, err = svc.Complete(t.Context(), Callback{State: state, Code: "wrong", Iss: f.issuer()})
	var te *tokenError
	require.ErrorAs(t, err, &te)
	require.Equal(t, "invalid_grant", te.Code)

	status, err := svc.Status(t.Context(), server, testMember)
	require.NoError(t, err)
	require.Equal(t, StatusNotConnected, status)
}

func TestServiceRevokedRefreshTokenNeedsReauth(t *testing.T) {
	f := newFakeAuthServer(t, nil)
	svc := newTestService(t, newTestStores(t), f, Config{})
	server := testServer(f)
	connect(t, svc, f, server)

	f.revokeRefreshTokens()
	require.NoError(t, svc.Invalidate(t.Context(), server, testMember))
	_, err := svc.AccessToken(t.Context(), server, testMember)
	require.ErrorIs(t, err, ErrNeedsReauth)

	status, err := svc.Status(t.Context(), server, testMember)
	require.NoError(t, err)
	require.Equal(t, StatusNeedsReauth, status)

	// Nothing is sent to the server again until the member reconnects.
	_, err = svc.AccessToken(t.Context(), server, testMember)
	require.ErrorIs(t, err, ErrNeedsReauth)
	require.Equal(t, 1, f.refreshCount())

	connect(t, svc, f, server)
	status, err = svc.Status(t.Context(), server, testMember)
	require.NoError(t, err)
	require.Equal(t, StatusConnected, status)
}

func TestServiceCredentialsBoundToServerURL(t *testing.T) {
	f := newFakeAuthServer(t, nil)
	svc := newTestService(t, newTestStores(t), f, Config{})
	server := testServer(f)
	connect(t, svc, f, server)

	moved := server
	moved.URL = "https://elsewhere.example.com/mcp"
	status, err := svc.Status(t.Context(), moved, testMember)
	require.NoError(t, err)
	require.Equal(t, StatusNotConnected, status)
	_, err = svc.AccessToken(t.Context(), moved, testMember)
	require.ErrorIs(t, err, ErrNotConnected)

	// Another member of the same org isn't connected either.
	_, err = svc.AccessToken(t.Context(), server, "did:plc:someone-else")
	require.ErrorIs(t, err, ErrNotConnected)
}

func TestServiceDisconnect(t *testing.T) {
	f := newFakeAuthServer(t, nil)
	svc := newTestService(t, newTestStores(t), f, Config{})
	server := testServer(f)

	require.ErrorIs(t, svc.Disconnect(t.Context(), server, testMember), ErrNotConnected)

	connect(t, svc, f, server)
	require.NoError(t, svc.Disconnect(t.Context(), server, testMember))

	status, err := svc.Status(t.Context(), server, testMember)
	require.NoError(t, err)
	require.Equal(t, StatusNotConnected, status)
	_, err = svc.AccessToken(t.Context(), server, testMember)
	require.ErrorIs(t, err, ErrNotConnected)
}

func TestServiceConcurrentRefreshesInOneProcessShareOne(t *testing.T) {
	f := newFakeAuthServer(t, nil)
	svc := newTestService(t, newTestStores(t), f, Config{})
	server := testServer(f)
	connect(t, svc, f, server)
	require.NoError(t, svc.Invalidate(t.Context(), server, testMember))

	var wg sync.WaitGroup
	tokens := make([]string, 8)
	for i := range tokens {
		wg.Go(func() {
			tok, err := svc.AccessToken(t.Context(), server, testMember)
			if err == nil {
				tokens[i] = tok
			}
		})
	}
	wg.Wait()
	require.Equal(t, 1, f.refreshCount())
	for _, tok := range tokens {
		require.Equal(t, tokens[0], tok)
		require.NotEmpty(t, tok)
	}
}

func TestServiceConcurrentRefreshesAcrossInstancesShareOne(t *testing.T) {
	refreshStarted := make(chan struct{}, 1)
	refreshGate := make(chan struct{})
	f := newFakeAuthServer(t, func(f *fakeAuthServer) {
		f.rotateRefresh = true
		f.refreshStarted = refreshStarted
		f.refreshGate = refreshGate
	})
	stores := newTestStores(t)
	instanceA := newTestService(t, stores, f, Config{})
	instanceB := newTestService(t, stores, f, Config{})
	server := testServer(f)
	connect(t, instanceA, f, server)
	require.NoError(t, instanceA.Invalidate(t.Context(), server, testMember))

	type result struct {
		tok string
		err error
	}
	results := make(chan result, 2)
	fetch := func(svc Service) {
		tok, err := svc.AccessToken(t.Context(), server, testMember)
		results <- result{tok, err}
	}

	// A's refresh is in flight, holding the lock, when B asks.
	go fetch(instanceA)
	<-refreshStarted
	go fetch(instanceB)
	close(refreshGate)

	a, b := <-results, <-results
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	require.Equal(t, a.tok, b.tok)
	// With a rotating refresh token, a second refresh would have used the
	// revoked one and failed.
	require.Equal(t, 1, f.refreshCount())
}

func TestServiceClientRegistration(t *testing.T) {
	t.Run("uses a client ID metadata document when supported", func(t *testing.T) {
		const cimd = "https://pear.example.com/oauth/mcp-client.json"
		f := newFakeAuthServer(t, func(f *fakeAuthServer) {
			f.cimdSupported = true
			f.expectedClientID = cimd
		})
		svc := newTestService(t, newTestStores(t), f, Config{ClientIDMetadataURL: cimd})
		authURL, err := svc.Begin(t.Context(), testServer(f), testMember)
		require.NoError(t, err)
		f.approve(t, authURL)
		require.Zero(t, f.registrationCount())
	})

	t.Run("fails without dynamic registration or a metadata document", func(t *testing.T) {
		f := newFakeAuthServer(t, func(f *fakeAuthServer) { f.noDCR = true })
		svc := newTestService(t, newTestStores(t), f, Config{})
		_, err := svc.Begin(t.Context(), testServer(f), testMember)
		require.ErrorIs(t, err, ErrClientRegistrationUnsupported)
	})
}

func TestDiscoverer(t *testing.T) {
	t.Run("finds the authorization server through protected resource metadata", func(t *testing.T) {
		f := newFakeAuthServer(t, nil)
		disc, err := NewDiscoverer(f.httpClient()).Discover(t.Context(), f.serverURL())
		require.NoError(t, err)
		require.Equal(t, f.serverURL(), disc.Resource)
		require.Equal(t, f.issuer(), disc.AuthServer.Issuer)
		require.Equal(t, []string{"read", "write", "offline_access"}, disc.Scopes)
	})

	t.Run(
		"treats a server without resource metadata as its own authorization server",
		func(t *testing.T) {
			f := newFakeAuthServer(t, func(f *fakeAuthServer) { f.noResourceMeta = true })
			disc, err := NewDiscoverer(f.httpClient()).Discover(t.Context(), f.serverURL())
			require.NoError(t, err)
			require.Equal(t, f.serverURL(), disc.Resource)
			require.Equal(t, f.issuer(), disc.AuthServer.Issuer)
		},
	)

	t.Run("requires PKCE S256", func(t *testing.T) {
		f := newFakeAuthServer(t, func(f *fakeAuthServer) { f.noPKCE = true })
		_, err := NewDiscoverer(f.httpClient()).Discover(t.Context(), f.serverURL())
		require.ErrorIs(t, err, ErrPKCEUnsupported)
	})

	t.Run("fails for an unreachable server", func(t *testing.T) {
		f := newFakeAuthServer(t, nil)
		f.srv.Close()
		_, err := NewDiscoverer(f.httpClient()).Discover(t.Context(), f.serverURL())
		require.Error(t, err)
	})
}

func TestNewValidatesArguments(t *testing.T) {
	stores := newTestStores(t)
	d := NewDiscoverer(nil)
	_, err := New(nil, stores.plain, d, nil, Config{RedirectURL: testRedirectURL})
	require.Error(t, err)
	_, err = New(stores.secrets, stores.plain, nil, nil, Config{RedirectURL: testRedirectURL})
	require.Error(t, err)
	_, err = New(stores.secrets, stores.plain, d, nil, Config{RedirectURL: "/relative"})
	require.Error(t, err)
	_, err = New(stores.secrets, stores.plain, d, nil, Config{RedirectURL: testRedirectURL})
	require.NoError(t, err)
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(u.Scheme, "http"))
	return u.Query()
}

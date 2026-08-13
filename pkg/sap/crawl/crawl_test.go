package crawl

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// rewriteTransport routes path-only request URLs to a test server, standing in
// for the OAuth client transport.
type rewriteTransport struct{ base *url.URL }

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.base.Scheme
	req.URL.Host = t.base.Host
	return http.DefaultTransport.RoundTrip(req)
}

type fakeClients struct{ base *url.URL }

func (f fakeClients) ClientForSpace(
	context.Context,
	habitat_syntax.SpaceURI,
) (*http.Client, error) {
	return &http.Client{Transport: rewriteTransport(f)}, nil
}

// testDPoPKey and testJWT stand in for a real session's DPoP key and access
// token: their values are never verified by the test server, only sent.
func testDPoPKey(t *testing.T) string {
	t.Helper()
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	return key.Multibase()
}

func testJWT(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tok, err := jwt.NewWithClaims(jwt.SigningMethodPS256,
		jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix(), "jti": "test"},
	).SignedString(key)
	require.NoError(t, err)
	return tok
}

// newOAuthApp builds a bare *oauth.ClientApp over an in-memory session store,
// with sessionID pre-saved as resumable for did against base.
func newOAuthApp(t *testing.T, base *url.URL, did syntax.DID, sessionID string) *oauth.ClientApp {
	t.Helper()
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	app := oauth.NewClientApp(&cfg, oauth.NewMemStore())
	require.NoError(t, app.Store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:              did,
		SessionID:               sessionID,
		HostURL:                 base.String(),
		AccessToken:             testJWT(t),
		DPoPPrivateKeyMultibase: testDPoPKey(t),
	}))
	return app
}

// recorder collects space access records and tracked repos.
type recorder struct {
	mu      sync.Mutex
	access  []habitat_syntax.SpaceURI
	tracked []syntax.DID
	checks  []syntax.DID
}

func (r *recorder) RecordSpaceAccess(
	_ context.Context,
	space habitat_syntax.SpaceURI,
	_ syntax.DID,
	_ string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.access = append(r.access, space)
	return nil
}

func (r *recorder) Track(
	_ context.Context,
	_ habitat_syntax.SpaceURI,
	repo syntax.DID,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tracked = append(r.tracked, repo)
	return nil
}

func (r *recorder) Check(
	_ context.Context,
	_ habitat_syntax.SpaceURI,
	repo syntax.DID,
	_ syntax.TID,
	_ string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checks = append(r.checks, repo)
	r.tracked = append(r.tracked, repo)
	return nil
}

func TestCrawlerBackfillsSession(t *testing.T) {
	t.Parallel()

	space := "ats://did:plc:owner/network.habitat.space/s1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.listSpaces":
			out := habitat.NetworkHabitatSpaceListSpacesOutput{}
			if r.URL.Query().Get("cursor") == "" {
				out.Spaces = []habitat.NetworkHabitatSpaceListSpacesSpaceView{{Uri: space}}
			}
			_ = json.NewEncoder(w).Encode(out)
		case "/xrpc/network.habitat.space.listRepos":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListReposOutput{
				Repos: []habitat.NetworkHabitatSpaceListReposRepo{
					{Did: "did:plc:alice"}, {Did: "did:plc:bob"},
				},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL)
	require.NoError(t, err)

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	rec := &recorder{}
	app := newOAuthApp(t, base, "did:plc:sessiondid", "sess1")
	c, err := New(db, app, rec, fakeClients{base: base}, rec, nil, nil, nil)
	require.NoError(t, err)

	c.Run(t.Context(), "did:plc:sessiondid", "sess1")

	require.Equal(t, []habitat_syntax.SpaceURI{habitat_syntax.SpaceURI(space)}, rec.access)
	require.Equal(t, []syntax.DID{"did:plc:alice", "did:plc:bob"}, rec.tracked)

	var cr crawl
	require.NoError(t, db.First(&cr, "did = ?", "did:plc:sessiondid").Error)
	require.Equal(t, stateComplete, cr.State)
}

// TestCrawlerDeduplicatesConcurrentRuns verifies that concurrent Run calls for
// the same session are deduplicated.
func TestCrawlerDeduplicatesConcurrentRuns(t *testing.T) {
	t.Parallel()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListSpacesOutput{})
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	rec := &recorder{}
	app := newOAuthApp(t, base, "did:plc:sessiondid", "sess1")
	c, err := New(db, app, rec, fakeClients{base: base}, rec, nil, nil, nil)
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(t.Context(), "did:plc:sessiondid", "sess1")
	}()
	c.Run(t.Context(), "did:plc:sessiondid", "sess1")
	<-done

	require.Equal(t, 1, calls)
}

// TestCrawlerRunCompleteThenRestart verifies that a completed crawl resets the
// cursor and starts fresh.
func TestCrawlerRunCompleteThenRestart(t *testing.T) {
	t.Parallel()

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path == "/xrpc/network.habitat.space.listSpaces" {
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListSpacesOutput{})
			return
		}
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListReposOutput{})
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	rec := &recorder{}
	app := newOAuthApp(t, base, "did:plc:alice", "sess1")
	c, err := New(db, app, rec, fakeClients{base: base}, rec, nil, nil, nil)
	require.NoError(t, err)

	c.Run(t.Context(), "did:plc:alice", "sess1")
	c.Run(t.Context(), "did:plc:alice", "sess1")

	var cr crawl
	require.NoError(t, db.First(&cr, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateComplete, cr.State)
	require.Empty(t, cr.Cursor)
}

// TestDetachCancel verifies that detachCancel returns a context that inherits
// cancellation from the parent.
func TestDetachCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	detached := detachCancel(ctx)

	cancel()
	select {
	case <-detached.Done():
	default:
		t.Fatal("detached context should be done after cancel")
	}
}

// TestCrawlerEnumerateReposError verifies that an error from the listRepos
// endpoint is propagated.
func TestCrawlerEnumerateReposError(t *testing.T) {
	t.Parallel()

	space := "ats://did:plc:owner/network.habitat.space/s1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.listSpaces":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListSpacesOutput{
				Spaces: []habitat.NetworkHabitatSpaceListSpacesSpaceView{{Uri: space}},
			})
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	rec := &recorder{}
	app := newOAuthApp(t, base, "did:plc:alice", "sess1")
	c, err := New(db, app, rec, fakeClients{base: base}, rec, nil, nil, nil)
	require.NoError(t, err)

	c.Run(t.Context(), "did:plc:alice", "sess1")

	var cr crawl
	require.NoError(t, db.First(&cr, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateErrored, cr.State)
	require.NotEmpty(t, cr.ErrorMsg)
}

// TestCrawlerNotifyRegistration covers the path where c.notify is non-nil.
func TestCrawlerNotifyRegistration(t *testing.T) {
	t.Parallel()

	space := "ats://did:plc:owner/network.habitat.space/s1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.listSpaces":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListSpacesOutput{
				Spaces: []habitat.NetworkHabitatSpaceListSpacesSpaceView{{Uri: space}},
			})
		case "/xrpc/network.habitat.space.listRepos":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListReposOutput{})
		}
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	rec := &recorder{}
	nr := &fakeNotifyRegistrar{}
	app := newOAuthApp(t, base, "did:plc:alice", "sess1")
	c, err := New(db, app, rec, fakeClients{base: base}, rec, nr, nil, nil)
	require.NoError(t, err)

	c.Run(t.Context(), "did:plc:alice", "sess1")
	require.True(t, nr.called)
}

type fakeNotifyRegistrar struct {
	called bool
}

func (f *fakeNotifyRegistrar) EnsureRegistered(_ context.Context, _ habitat_syntax.SpaceURI) error {
	f.called = true
	return nil
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

// TestCrawlerChecksRepoRevAndHash pins that the crawl compares the host's
// listRepos rev/hash against the tracker (so drift requeues) instead of only
// tracking newly-seen repos.
func TestCrawlerChecksRepoRevAndHash(t *testing.T) {
	space := habitat_syntax.SpaceURI("ats://did:web:owner/network.habitat.space/s1")
	repoDID := syntax.DID("did:web:alice")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.listSpaces":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListSpacesOutput{
				Spaces: []habitat.NetworkHabitatSpaceListSpacesSpaceView{{Uri: space.String()}},
			})
		case "/xrpc/network.habitat.space.listRepos":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListReposOutput{
				Repos: []habitat.NetworkHabitatSpaceListReposRepo{
					{Did: repoDID.String(), Rev: "3lrev2", Hash: "aGFzaA=="},
				},
			})
		}
	}))
	t.Cleanup(srv.Close)

	rec := &recorder{}
	base := mustParseURL(t, srv.URL)
	fc := fakeClients{base: base}
	app := newOAuthApp(t, base, "did:plc:alice", "sess1")
	c, err := New(db_testutil.NewDB(t), app, rec, fc, rec, nil, nil, nil)
	require.NoError(t, err)

	require.NoError(t, c.enumerateRepos(t.Context(), space))

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Contains(t, rec.checks, repoDID)
}

package crawl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
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

func (f fakeClients) ClientForSession(context.Context, syntax.DID) (*http.Client, error) {
	return &http.Client{Transport: rewriteTransport{base: f.base}}, nil
}

// recorder collects space access records and tracked repos.
type recorder struct {
	mu      sync.Mutex
	access  []habitat_syntax.SpaceURI
	tracked []syntax.DID
}

func (r *recorder) RecordSpaceAccess(
	_ context.Context,
	space habitat_syntax.SpaceURI,
	_ syntax.DID,
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
	c, err := New(db, fakeClients{base: base}, rec, rec, nil, nil)
	require.NoError(t, err)

	c.Run(t.Context(), "did:plc:sessiondid")

	require.Equal(t, []habitat_syntax.SpaceURI{habitat_syntax.SpaceURI(space)}, rec.access)
	require.Equal(t, []syntax.DID{"did:plc:alice", "did:plc:bob"}, rec.tracked)

	var cr crawl
	require.NoError(t, db.First(&cr, "did = ?", "did:plc:sessiondid").Error)
	require.Equal(t, stateComplete, cr.State)
}

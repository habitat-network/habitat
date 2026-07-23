package register

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

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
	return &http.Client{Transport: rewriteTransport{base: f.base}}, nil
}

type fakeSpaces []habitat_syntax.SpaceURI

func (f fakeSpaces) Spaces(context.Context) ([]habitat_syntax.SpaceURI, error) {
	return f, nil
}

// TestRegistrarRegistersDueSpaces covers the happy path: an unregistered space
// is registered with the host and its expiry recorded; a fresh registration is
// not re-registered on the next sweep.
func TestRegistrarRegistersDueSpaces(t *testing.T) {
	t.Parallel()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/network.habitat.space.registerNotify", r.URL.Path)
		calls++
		var in habitat.NetworkHabitatSpaceRegisterNotifyInput
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		require.Equal(t, "https://sap.example", in.Endpoint)
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceRegisterNotifyOutput{
			ExpiresAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL)
	require.NoError(t, err)

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg := New(db, fakeClients{base: base}, fakeSpaces{space}, "https://sap.example")

	reg.sweep(t.Context())
	require.Equal(t, 1, calls)

	var row registration
	require.NoError(t, db.First(&row, "space = ?", space).Error)
	require.True(t, row.ExpiresAt.After(time.Now()))

	// Still fresh: nothing to do on the next sweep.
	reg.sweep(t.Context())
	require.Equal(t, 1, calls)
}

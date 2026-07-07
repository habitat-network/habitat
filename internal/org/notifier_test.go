package org

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/core"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/stretchr/testify/require"
)

// fakeHive implements hive.Hive; only SignServiceAuth is exercised here.
type fakeHive struct {
	hive.Hive
	token string
}

func (f *fakeHive) SignServiceAuth(
	_ context.Context,
	_ syntax.DID,
	_ string,
	_ time.Duration,
	_ *syntax.NSID,
) (string, error) {
	return f.token, nil
}

// capturedRequest records what an app received on notifyApp.
type capturedRequest struct {
	path  string
	auth  string
	input habitat.NetworkHabitatOrgNotifyAppInput
}

// notifyAppServer returns an httptest server implementing notifyApp that records
// every request it receives.
func notifyAppServer(t *testing.T) (*httptest.Server, *[]capturedRequest) {
	t.Helper()
	var mu sync.Mutex
	captured := &[]capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var input habitat.NetworkHabitatOrgNotifyAppInput
		require.NoError(t, json.Unmarshal(body, &input))
		mu.Lock()
		*captured = append(*captured, capturedRequest{
			path:  r.URL.Path,
			auth:  r.Header.Get("Authorization"),
			input: input,
		})
		mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

func TestNotifyApps(t *testing.T) {
	srv, captured := notifyAppServer(t)
	notifier := NewNotifier([]string{srv.URL}, &fakeHive{token: "test-jwt"})

	orgDID := syntax.DID("did:web:acme.example.com")
	notifier.NotifyApps(t.Context(), orgDID)

	require.Len(t, *captured, 1)
	got := (*captured)[0]
	require.Equal(t, "/xrpc/network.habitat.org.notifyApp", got.path)
	require.Equal(t, "Bearer test-jwt", got.auth)
	require.Equal(t, orgDID.String(), got.input.Org)
}

func TestNotifyApps_NotifiesEveryApp(t *testing.T) {
	srv1, captured1 := notifyAppServer(t)
	srv2, captured2 := notifyAppServer(t)
	notifier := NewNotifier([]string{srv1.URL, srv2.URL}, &fakeHive{token: "jwt"})

	notifier.NotifyApps(t.Context(), syntax.DID("did:web:acme.example.com"))

	require.Len(t, *captured1, 1)
	require.Len(t, *captured2, 1)
}

// fakeStore implements Store; only ListOrgs is exercised.
type fakeStore struct {
	Store
	orgs []core.Org
}

func (f *fakeStore) ListOrgs(_ context.Context) ([]core.Org, error) {
	return f.orgs, nil
}

// fakeOrg implements core.Org; only DID is exercised.
type fakeOrg struct {
	core.Org
	did syntax.DID
}

func (f *fakeOrg) DID() syntax.DID { return f.did }

func TestBootstrapNotifications(t *testing.T) {
	srv, captured := notifyAppServer(t)
	notifier := NewNotifier([]string{srv.URL}, &fakeHive{token: "jwt"})

	store := &fakeStore{orgs: []core.Org{
		&fakeOrg{did: syntax.DID("did:web:acme.example.com")},
		&fakeOrg{did: syntax.DID("did:web:globex.example.com")},
	}}

	require.NoError(t, BootstrapNotifications(t.Context(), notifier, store))

	require.Len(t, *captured, 2)
	orgs := []string{(*captured)[0].input.Org, (*captured)[1].input.Org}
	require.ElementsMatch(t,
		[]string{"did:web:acme.example.com", "did:web:globex.example.com"},
		orgs,
	)
}

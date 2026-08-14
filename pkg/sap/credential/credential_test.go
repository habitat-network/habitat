package credential

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// stubDelegator hands out a fixed delegation token.
type stubDelegator struct{}

func (stubDelegator) DelegationToken(context.Context, habitat_syntax.SpaceURI) (string, error) {
	return "test-delegation", nil
}

// credToken extracts the bearer token a ClientForSpace client would send,
// standing in for the removed Manager.Credential.
func credToken(t *testing.T, m *Manager, space habitat_syntax.SpaceURI) string {
	t.Helper()
	client, err := m.ClientForSpace(t.Context(), space)
	require.NoError(t, err)
	return strings.TrimPrefix(client.Headers.Get("Authorization"), "Bearer ")
}

// dirWithSpaceHost builds an identity.MockDirectory with a single identity
// whose atproto_space_host service points at host.
func dirWithSpaceHost(did syntax.DID, host string) *identity.MockDirectory {
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID:      did,
		Services: map[string]identity.ServiceEndpoint{"atproto_space_host": {URL: host}},
	})
	return dir
}

// TestManagerMintsCachesAndAuthenticates covers mint (against the space
// owner's resolved host, not any fixed host), cache (no second mint on a hot
// call), and a repo-host request carrying the credential.
func TestManagerMintsCachesAndAuthenticates(t *testing.T) {
	var (
		mu           sync.Mutex
		credCalls    int
		repoAuth     string
		repoRevCalls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.getSpaceCredential":
			mu.Lock()
			credCalls++
			mu.Unlock()
			require.Equal(t, "Bearer test-delegation", r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(
				habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: "space-cred"})
		case "/xrpc/network.habitat.space.listRepos":
			mu.Lock()
			repoAuth = r.Header.Get("Authorization")
			repoRevCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListReposOutput{})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	owner := syntax.DID("did:web:org")
	space := habitat_syntax.SpaceURI("at://did:web:org/space/network.habitat.group/s1")
	dir := dirWithSpaceHost(owner, srv.URL)
	m := NewManager(dir, srv.Client(), stubDelegator{})

	require.Equal(t, "space-cred", credToken(t, m, space))

	// Cached: a second call does not re-mint.
	require.Equal(t, "space-cred", credToken(t, m, space))
	mu.Lock()
	require.Equal(t, 1, credCalls)
	mu.Unlock()

	// A repo-host read through the manager's client carries the credential.
	client, err := m.ClientForSpace(t.Context(), space)
	require.NoError(t, err)
	var out habitat.NetworkHabitatSpaceListReposOutput
	require.NoError(t, client.Get(
		t.Context(),
		"network.habitat.space.listRepos",
		map[string]any{"space": space.String()},
		&out,
	))
	mu.Lock()
	require.Equal(t, "Bearer space-cred", repoAuth)
	require.Equal(t, 1, repoRevCalls)
	mu.Unlock()

	// DropSpace evicts; the next mint re-exchanges.
	m.DropSpace(space)
	require.Equal(t, "space-cred", credToken(t, m, space))
	mu.Lock()
	require.Equal(t, 2, credCalls)
	mu.Unlock()
}

// TestManagerResolvesHostPerSpace verifies each space's credential is
// exchanged against its own owner's host, not a host fixed at construction —
// two spaces owned by different DIDs land on two different test servers.
func TestManagerResolvesHostPerSpace(t *testing.T) {
	newServer := func(label string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/xrpc/network.habitat.space.getSpaceCredential" {
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(
				habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: "cred-" + label})
		}))
	}
	srvA := newServer("a")
	t.Cleanup(srvA.Close)
	srvB := newServer("b")
	t.Cleanup(srvB.Close)

	ownerA := syntax.DID("did:web:org-a")
	ownerB := syntax.DID("did:web:org-b")
	spaceA := habitat_syntax.SpaceURI("at://did:web:org-a/space/network.habitat.group/s1")
	spaceB := habitat_syntax.SpaceURI("at://did:web:org-b/space/network.habitat.group/s1")
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID:      ownerA,
		Services: map[string]identity.ServiceEndpoint{"atproto_space_host": {URL: srvA.URL}},
	})
	dir.Insert(identity.Identity{
		DID:      ownerB,
		Services: map[string]identity.ServiceEndpoint{"atproto_space_host": {URL: srvB.URL}},
	})
	m := NewManager(dir, http.DefaultClient, stubDelegator{})

	require.Equal(t, "cred-a", credToken(t, m, spaceA))
	require.Equal(t, "cred-b", credToken(t, m, spaceB))
}

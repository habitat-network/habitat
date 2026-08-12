package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// fakeDirectory resolves each DID's habitat host from a fixed map, standing
// in for identity.Directory.
type fakeDirectory map[syntax.DID]string

func (f fakeDirectory) LookupDID(_ context.Context, did syntax.DID) (*identity.Identity, error) {
	host, ok := f[did]
	if !ok {
		return nil, fmt.Errorf("did %s not found", did)
	}
	return &identity.Identity{
		DID:      did,
		Services: map[string]identity.ServiceEndpoint{"habitat": {URL: host}},
	}, nil
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
	dir := fakeDirectory{owner: srv.URL}
	m := NewManager(dir, srv.Client(), stubDelegator{})

	token, err := m.Credential(t.Context(), space)
	require.NoError(t, err)
	require.Equal(t, "space-cred", token)

	// Cached: a second call does not re-mint.
	token, err = m.Credential(t.Context(), space)
	require.NoError(t, err)
	require.Equal(t, "space-cred", token)
	mu.Lock()
	require.Equal(t, 1, credCalls)
	mu.Unlock()

	// A repo-host read through the manager's client carries the credential.
	resp, err := m.ClientForSpace(space).Get(
		"/xrpc/network.habitat.space.listRepos?space=" + space.String())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	mu.Lock()
	require.Equal(t, "Bearer space-cred", repoAuth)
	require.Equal(t, 1, repoRevCalls)
	mu.Unlock()

	// DropSpace evicts; the next mint re-exchanges.
	m.DropSpace(space)
	token, err = m.Credential(t.Context(), space)
	require.NoError(t, err)
	require.Equal(t, "space-cred", token)
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
	dir := fakeDirectory{ownerA: srvA.URL, ownerB: srvB.URL}
	m := NewManager(dir, http.DefaultClient, stubDelegator{})

	tokenA, err := m.Credential(t.Context(), spaceA)
	require.NoError(t, err)
	require.Equal(t, "cred-a", tokenA)

	tokenB, err := m.Credential(t.Context(), spaceB)
	require.NoError(t, err)
	require.Equal(t, "cred-b", tokenB)
}

package credential

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// stubDelegator hands out a fixed delegation token.
type stubDelegator struct{}

func (stubDelegator) DelegationToken(context.Context, habitat_syntax.SpaceURI) (string, error) {
	return "test-delegation", nil
}

// TestManagerMintsCachesAndAuthenticates covers mint, cache (no second mint on
// a hot call), and a repo-host request carrying the credential.
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

	space := habitat_syntax.SpaceURI("at://did:web:org/space/network.habitat.group/s1")
	m := NewManager(srv.URL, srv.Client(), stubDelegator{})

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

package notify

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// newTestServer returns a server that authenticates every request as a space
// credential for the given space.
func newTestServer(t *testing.T, credSpace habitat_syntax.SpaceURI) *Server {
	t.Helper()
	return NewServer(
		newTestStore(t),
		authntest.NewSuccessValidator(&authn.CredentialInfo{Space: credSpace}),
	)
}

func TestServerRegisterNotify(t *testing.T) {
	s := newTestServer(t, space)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space: space.String(), Endpoint: "https://sync.example/all",
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	expiresAt, err := time.Parse(time.RFC3339, out.ExpiresAt)
	require.NoError(t, err)
	require.True(t, expiresAt.After(time.Now()))

	regs, err := s.store.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, "https://sync.example/all", regs[0].Endpoint)
	require.Empty(t, regs[0].Repo)
}

func TestServerRegisterNotifyRepoSpecific(t *testing.T) {
	s := newTestServer(t, space)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space: space.String(), Repo: repo.String(), Endpoint: "https://sync.example/alice",
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	regs, err := s.store.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, repo, regs[0].Repo)
}

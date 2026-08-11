package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
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

func registerNotifyReq(body string) *http.Request {
	return httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.registerNotify",
		strings.NewReader(body),
	)
}

func TestServerRegisterNotify(t *testing.T) {
	s := newTestServer(t, space)

	body := `{"space": "` + space.String() + `", "endpoint": "https://sync.example/all"}`
	w := httptest.NewRecorder()
	s.RegisterNotify(w, registerNotifyReq(body))

	require.Equal(t, http.StatusOK, w.Code)
	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
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

	body := `{"space": "` + space.String() + `", "repo": "` + repo.String() +
		`", "endpoint": "https://sync.example/alice"}`
	w := httptest.NewRecorder()
	s.RegisterNotify(w, registerNotifyReq(body))

	require.Equal(t, http.StatusOK, w.Code)
	regs, err := s.store.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, repo, regs[0].Repo)
}

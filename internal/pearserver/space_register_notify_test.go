package pearserver_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// notifySpace is the space the registerNotify test validator authenticates as,
// matching the space credential the handler requires.
var notifySpace = habitat_syntax.SpaceURI("at://did:plc:org/space/network.habitat.group/s1")

// newNotifyServer returns a server that authenticates every request as a space
// credential for notifySpace.
func newNotifyServer(t *testing.T) *pearserver_testutil.TestServer {
	t.Helper()
	return pearserver_testutil.NewTestServer(t,
		pearserver_testutil.WithValidator(
			authntest.NewSuccessValidator(&authn.CredentialInfo{Space: notifySpace}),
		),
	)
}

func TestServerRegisterNotify(t *testing.T) {
	ts := newNotifyServer(t)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space: notifySpace.String(), Endpoint: "https://sync.example/all",
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	expiresAt, err := time.Parse(time.RFC3339, out.ExpiresAt)
	require.NoError(t, err)
	require.True(t, expiresAt.After(time.Now()))

	regs, err := ts.NotifyStore.ListForRepo(t.Context(), notifySpace, alice)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, "https://sync.example/all", regs[0].Endpoint)
	require.Empty(t, regs[0].Repo)
}

func TestServerRegisterNotifyRepoSpecific(t *testing.T) {
	ts := newNotifyServer(t)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space:    notifySpace.String(),
			Repo:     alice.String(),
			Endpoint: "https://sync.example/alice",
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	regs, err := ts.NotifyStore.ListForRepo(t.Context(), notifySpace, alice)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, alice, regs[0].Repo)
}

func TestServerRegisterNotifyRejectsInvalidSpace(t *testing.T) {
	ts := newNotifyServer(t)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space: "not-a-space", Endpoint: "https://sync.example/all",
		},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)
}

func TestServerRegisterNotifyRejectsInvalidRepo(t *testing.T) {
	ts := newNotifyServer(t)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space: notifySpace.String(), Repo: "not-a-did", Endpoint: "https://sync.example/alice",
		},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)
}

func TestServerRegisterNotifyRejectsWithoutSpaceCredential(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t,
		pearserver_testutil.WithValidator(authntest.NewFailureValidator()),
	)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space: notifySpace.String(), Endpoint: "https://sync.example/all",
		},
		&out,
	)
	require.Equal(t, http.StatusUnauthorized, code)

	regs, err := ts.NotifyStore.ListForRepo(t.Context(), notifySpace, alice)
	require.NoError(t, err)
	require.Empty(t, regs)
}

// TestServerRegisterNotifyComAtprotoAlias pins that the proposal 0016 alias
// /xrpc/com.atproto.space.registerNotify is served by the same handler through
// the consolidated router.
func TestServerRegisterNotifyComAtprotoAlias(t *testing.T) {
	ts := newNotifyServer(t)

	body, err := json.Marshal(habitat.NetworkHabitatSpaceRegisterNotifyInput{
		Space:    notifySpace.String(),
		Endpoint: "https://sync.example/all",
	})
	require.NoError(t, err)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/com.atproto.space.registerNotify",
		bytes.NewReader(body),
	)
	w := httptest.NewRecorder()
	ts.Server.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	regs, err := ts.NotifyStore.ListForRepo(t.Context(), notifySpace, alice)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, "https://sync.example/all", regs[0].Endpoint)
}

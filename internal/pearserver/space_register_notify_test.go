package pearserver_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/notify"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// notifySpace is the space the registerNotify test validator authenticates as,
// matching the space credential the handler requires.
var notifySpace = habitat_syntax.SpaceURI("at://did:plc:org/space/network.habitat.group/s1")

// syncerDID publishes the notify service a test registers sap under, pointing at
// syncerEndpoint.
const (
	syncerDID      = "did:web:sync.example.com"
	syncerService  = "habitat_space_syncer"
	syncerRef      = syncerDID + "#" + syncerService
	syncerEndpoint = "https://sync.example"
)

// syncerDirectory returns a directory publishing syncerDID's notify service at
// syncerEndpoint.
func syncerDirectory() *identity.MockDirectory {
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID: syntax.DID(syncerDID),
		Services: map[string]identity.ServiceEndpoint{
			syncerService: {Type: "HabitatSpaceSyncer", URL: syncerEndpoint},
		},
	})
	return dir
}

// newNotifyServer returns a server that authenticates every request as a space
// credential for notifySpace and can resolve the syncer's service identifier.
func newNotifyServer(t *testing.T) *pearserver_testutil.TestServer {
	t.Helper()
	return pearserver_testutil.NewTestServer(t,
		pearserver_testutil.WithValidator(
			authntest.NewSuccessValidator(&authn.CredentialInfo{Space: notifySpace}),
		),
		pearserver_testutil.WithDirectory(syncerDirectory()),
	)
}

// register calls the handler and returns the status code it wrote.
func register(
	t *testing.T,
	ts *pearserver_testutil.TestServer,
	in habitat.NetworkHabitatSpaceRegisterNotifyInput,
) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(in)
	require.NoError(t, err)
	req := httptest.NewRequest(
		http.MethodPost, "/xrpc/network.habitat.space.registerNotify", bytes.NewReader(body),
	)
	w := httptest.NewRecorder()
	ts.Server.ServeHTTP(w, req)
	return w
}

func TestServerRegisterNotify(t *testing.T) {
	ts := newNotifyServer(t)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space: notifySpace.String(), Service: syncerRef,
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
	// The service identifier is both what the registration is addressed by and
	// what it was resolved to an endpoint through.
	require.Equal(t, syncerRef, regs[0].Audience)
	require.Equal(t, syncerEndpoint, regs[0].Endpoint)
	require.Equal(t, notify.NamespaceHabitat, regs[0].Namespace)
	require.Empty(t, regs[0].Repo)
}

// TestServerRegisterNotifyAtproto pins that com.atproto.space.registerNotify
// records its registrations as com.atproto.space ones, so they are delivered
// com.atproto.space notifications.
func TestServerRegisterNotifyAtproto(t *testing.T) {
	ts := newNotifyServer(t)

	body, err := json.Marshal(habitat.NetworkHabitatSpaceRegisterNotifyInput{
		Space: notifySpace.String(), Service: syncerRef,
	})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	ts.Server.ServeHTTP(w, httptest.NewRequest(
		http.MethodPost, "/xrpc/com.atproto.space.registerNotify", bytes.NewReader(body),
	))
	require.Equal(t, http.StatusOK, w.Code)

	regs, err := ts.NotifyStore.ListForSpace(t.Context(), notifySpace)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, syncerRef, regs[0].Audience)
	require.Equal(t, notify.NamespaceAtproto, regs[0].Namespace)
}

func TestServerRegisterNotifyRepoSpecific(t *testing.T) {
	ts := newNotifyServer(t)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space:   notifySpace.String(),
			Repo:    alice.String(),
			Service: syncerRef,
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	regs, err := ts.NotifyStore.ListForRepo(t.Context(), notifySpace, alice)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, alice, regs[0].Repo)
}

// TestServerRegisterNotifyDeprecatedEndpoint pins that a subscriber which only
// passes the deprecated endpoint field is still registered, addressed by the URL
// it gave.
func TestServerRegisterNotifyDeprecatedEndpoint(t *testing.T) {
	ts := newNotifyServer(t)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space: notifySpace.String(), Endpoint: "https://legacy.example/all",
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)

	regs, err := ts.NotifyStore.ListForRepo(t.Context(), notifySpace, alice)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, "https://legacy.example/all", regs[0].Endpoint)
	// The endpoint is its own audience, which is what the audience migration
	// writes for rows that predate the service field.
	require.Equal(t, "https://legacy.example/all", regs[0].Audience)
}

// TestServerRegisterNotifyServiceBeatsEndpoint verifies the service identifier
// wins when a caller sends both, so a half-migrated client can send the
// deprecated field harmlessly for one release.
func TestServerRegisterNotifyServiceBeatsEndpoint(t *testing.T) {
	ts := newNotifyServer(t)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space:   notifySpace.String(),
			Service: syncerRef,
			// Wrong on purpose: it must not be what gets stored.
			Endpoint: "https://stale.example/all",
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)

	regs, err := ts.NotifyStore.ListForRepo(t.Context(), notifySpace, alice)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, syncerEndpoint, regs[0].Endpoint)
	require.Equal(t, syncerRef, regs[0].Audience)
}

// TestServerRegisterNotifyRejectsNeitherTarget covers a caller that named no
// subscriber at all, which the lexicon's relaxed `required` cannot express.
func TestServerRegisterNotifyRejectsNeitherTarget(t *testing.T) {
	ts := newNotifyServer(t)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{Space: notifySpace.String()},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)

	regs, err := ts.NotifyStore.ListForRepo(t.Context(), notifySpace, alice)
	require.NoError(t, err)
	require.Empty(t, regs)
}

// TestServerRegisterNotifyRejectsUnresolvableService covers a service
// identifier whose DID does not publish the named service.
func TestServerRegisterNotifyRejectsUnresolvableService(t *testing.T) {
	ts := newNotifyServer(t)

	for _, service := range []string{
		"did:web:unknown.example.com#" + syncerService, // DID does not resolve
		syncerDID + "#not_published",                   // DID resolves, no such service
		"not-a-did#" + syncerService,                   // not a DID at all
		syncerDID + "#",                                // empty fragment
		syncerDID + "#a#b",                             // ambiguous fragment
	} {
		var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.RegisterNotify,
			habitat.NetworkHabitatSpaceRegisterNotifyInput{
				Space: notifySpace.String(), Service: service,
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code, "service %q should be rejected", service)
	}

	regs, err := ts.NotifyStore.ListForRepo(t.Context(), notifySpace, alice)
	require.NoError(t, err)
	require.Empty(t, regs)
}

// TestServerRegisterNotifyBareServiceDIDFallsBackToPDS pins the fragment-free
// form: a bare DID names an account, which is served by its personal data
// server, matching how service identifiers resolve elsewhere in habitat.
func TestServerRegisterNotifyBareServiceDIDFallsBackToPDS(t *testing.T) {
	ts := newNotifyServer(t)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space: notifySpace.String(), Service: syncerDID,
		},
		&out,
	)
	// syncerDirectory publishes no atproto_pds, so the fallback finds nothing.
	require.Equal(t, http.StatusBadRequest, code)
}

func TestServerRegisterNotifyRejectsInvalidSpace(t *testing.T) {
	ts := newNotifyServer(t)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space: "not-a-space", Service: syncerRef,
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
			Space: notifySpace.String(), Repo: "not-a-did", Service: syncerRef,
		},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)
}

func TestServerRegisterNotifyRejectsWithoutSpaceCredential(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t,
		pearserver_testutil.WithValidator(authntest.NewFailureValidator()),
		pearserver_testutil.WithDirectory(syncerDirectory()),
	)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RegisterNotify,
		habitat.NetworkHabitatSpaceRegisterNotifyInput{
			Space: notifySpace.String(), Service: syncerRef,
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

	w := register(t, ts, habitat.NetworkHabitatSpaceRegisterNotifyInput{
		Space: notifySpace.String(), Service: syncerRef,
	})
	require.Equal(t, http.StatusOK, w.Code)

	regs, err := ts.NotifyStore.ListForRepo(t.Context(), notifySpace, alice)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, syncerRef, regs[0].Audience)
	require.Equal(t, syncerEndpoint, regs[0].Endpoint)
}

// TestServerRegisterNotifyComAtprotoAliasDeprecatedEndpoint keeps the alias
// tolerant of the deprecated field too, since callers pinned to the proposal's
// earlier shape are exactly the ones that have not migrated.
func TestServerRegisterNotifyComAtprotoAliasDeprecatedEndpoint(t *testing.T) {
	ts := newNotifyServer(t)

	w := register(t, ts, habitat.NetworkHabitatSpaceRegisterNotifyInput{
		Space: notifySpace.String(), Endpoint: "https://legacy.example/all",
	})
	require.Equal(t, http.StatusOK, w.Code)

	regs, err := ts.NotifyStore.ListForRepo(t.Context(), notifySpace, alice)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, "https://legacy.example/all", regs[0].Endpoint)
}

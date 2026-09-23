package pearserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/mcpgateway"
)

// nangoKey computes the same Nango integration key mcpgateway derives
// internally, so tests can look it up in a fake NangoClient's Integrations
// map without duplicating the formula.
func nangoKey(orgDID, id string) string {
	return mcpgateway.NangoKeyFor(syntax.DID(orgDID), syntax.RecordKey(id))
}

func TestServer_AddServer(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("requires mcp.configure", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{Org: orgDID, Name: "Server"},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("admin adds a server", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var addOut habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{Org: orgDID, Name: "Linear"},
			&addOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, addOut.Id)
		require.NotEmpty(t, addOut.SessionToken)
		require.True(t, ts.NangoClient.Integrations[nangoKey(orgDID, addOut.Id)])

		// No org record exists until the caller completes authorization in
		// Nango's Connect UI.
		servers, err := ts.McpGatewayStore.ListServers(t.Context(), syntax.DID(orgDID), admin)
		require.NoError(t, err)
		require.Empty(t, servers)

		// The Nango Connect UI reports success directly to the frontend
		// (which simply refetches); the gateway learns about it by asking
		// Nango, not by being told.
		ts.NangoClient.Connect("conn-1", nangoKey(orgDID, addOut.Id), admin.String())

		var completeOut habitat.NetworkHabitatMcpCompleteAddServerOutput
		code = client.Procedure(
			ts.Server.CompleteAddServer,
			habitat.NetworkHabitatMcpCompleteAddServerInput{Org: orgDID, Id: addOut.Id, Name: "Linear"},
			&completeOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "Linear", completeOut.Server.Name)

		servers, err = ts.McpGatewayStore.ListServers(t.Context(), syntax.DID(orgDID), admin)
		require.NoError(t, err)
		require.Len(t, servers, 1)
	})

	t.Run("complete without a nango connection fails", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var addOut habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{Org: orgDID, Name: "Linear"},
			&addOut,
		)
		require.Equal(t, http.StatusOK, code)

		var completeOut habitat.NetworkHabitatMcpCompleteAddServerOutput
		code = client.Procedure(
			ts.Server.CompleteAddServer,
			habitat.NetworkHabitatMcpCompleteAddServerInput{Org: orgDID, Id: addOut.Id, Name: "Linear"},
			&completeOut,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})
}

func TestServer_CancelAddServer(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	ts := newOpenSocialServer(t, admin)
	orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)

	var addOut habitat.NetworkHabitatMcpAddServerOutput
	code := client.Procedure(
		ts.Server.AddServer,
		habitat.NetworkHabitatMcpAddServerInput{Org: orgDID, Name: "Linear"},
		&addOut,
	)
	require.Equal(t, http.StatusOK, code)
	require.True(t, ts.NangoClient.Integrations[nangoKey(orgDID, addOut.Id)])

	var cancelOut struct{}
	code = client.Procedure(
		ts.Server.CancelAddServer,
		habitat.NetworkHabitatMcpCancelAddServerInput{Org: orgDID, Id: addOut.Id},
		&cancelOut,
	)
	require.Equal(t, http.StatusOK, code)
	require.False(t, ts.NangoClient.Integrations[nangoKey(orgDID, addOut.Id)])
}

func TestServer_ListServersAndAuthorize(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("member lists servers and authorizes", func(t *testing.T) {
		adminTS, aliceTS, _ := newSharedOpenSocialServers(t)
		orgDID, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		require.NoError(
			t, adminTS.OpenSocialStore.AssignRoles(
				t.Context(), syntax.DID(orgDID), alice, []string{"member"},
			),
		)

		var addOut habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			adminTS.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{Org: orgDID, Name: "Linear"},
			&addOut,
		)
		require.Equal(t, http.StatusOK, code)
		adminTS.NangoClient.Connect("conn-admin", nangoKey(orgDID, addOut.Id), admin.String())

		var completeOut habitat.NetworkHabitatMcpCompleteAddServerOutput
		code = client.Procedure(
			adminTS.Server.CompleteAddServer,
			habitat.NetworkHabitatMcpCompleteAddServerInput{Org: orgDID, Id: addOut.Id, Name: "Linear"},
			&completeOut,
		)
		require.Equal(t, http.StatusOK, code)

		var listOut habitat.NetworkHabitatMcpListServersOutput
		code = client.Query(
			aliceTS.Server.ListServers, url.Values{"org": []string{orgDID}}, &listOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.Len(t, listOut.Servers, 1)
		require.False(t, listOut.Servers[0].Connected)

		var startOut habitat.NetworkHabitatMcpStartAuthorizationOutput
		code = client.Procedure(
			aliceTS.Server.StartAuthorization,
			habitat.NetworkHabitatMcpStartAuthorizationInput{Org: orgDID, Id: addOut.Id},
			&startOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, startOut.SessionToken)

		// The Nango Connect UI reports success directly to the frontend
		// (which simply refetches); the gateway learns about it by asking
		// Nango, not by being told.
		aliceTS.NangoClient.Connect("conn-alice", nangoKey(orgDID, addOut.Id), alice.String())

		code = client.Query(
			aliceTS.Server.ListServers, url.Values{"org": []string{orgDID}}, &listOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.True(t, listOut.Servers[0].Connected)
	})

	t.Run("non-member cannot list", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out habitat.NetworkHabitatMcpListServersOutput
		code := client.Query(
			ts.Server.ListServers, url.Values{"org": []string{orgDID}}, &out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})
}

func TestServer_StartAuthorization_NotFound(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	ts := newOpenSocialServer(t, admin)
	orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)

	var startOut habitat.NetworkHabitatMcpStartAuthorizationOutput
	code := client.Procedure(
		ts.Server.StartAuthorization,
		habitat.NetworkHabitatMcpStartAuthorizationInput{Org: orgDID, Id: "nonexistent"},
		&startOut,
	)
	require.Equal(t, http.StatusNotFound, code)
}

func TestServer_RemoveServer(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("admin removes a server", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var addOut habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{Org: orgDID, Name: "Linear"},
			&addOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.True(t, ts.NangoClient.Integrations[nangoKey(orgDID, addOut.Id)])
		ts.NangoClient.Connect("conn-1", nangoKey(orgDID, addOut.Id), admin.String())

		var completeOut habitat.NetworkHabitatMcpCompleteAddServerOutput
		code = client.Procedure(
			ts.Server.CompleteAddServer,
			habitat.NetworkHabitatMcpCompleteAddServerInput{Org: orgDID, Id: addOut.Id, Name: "Linear"},
			&completeOut,
		)
		require.Equal(t, http.StatusOK, code)

		var removeOut struct{}
		code = client.Procedure(
			ts.Server.RemoveServer,
			habitat.NetworkHabitatMcpRemoveServerInput{Org: orgDID, Id: addOut.Id},
			&removeOut,
		)
		require.Equal(t, http.StatusOK, code)

		servers, err := ts.McpGatewayStore.ListServers(t.Context(), syntax.DID(orgDID), admin)
		require.NoError(t, err)
		require.Empty(t, servers)
		require.False(t, ts.NangoClient.Integrations[nangoKey(orgDID, addOut.Id)])
	})

	t.Run("non-admin cannot remove", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out struct{}
		code := client.Procedure(
			ts.Server.RemoveServer,
			habitat.NetworkHabitatMcpRemoveServerInput{Org: orgDID, Id: "whatever"},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})
}

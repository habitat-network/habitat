package pearserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
)

func TestServer_AddServer(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("requires community.configure", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		srv := newFakeOpenMcpServer(t)

		var out habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{Org: orgDID, Name: "Server", Url: srv.URL},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("admin adds a server and auth type is detected", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		fake := newFakeOAuthMcpServer(t)

		var out habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{Org: orgDID, Name: "Linear", Url: fake.URL},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "Linear", out.Server.Name)
		require.Equal(t, "oauth", out.Server.AuthType)
		require.True(t, ts.NangoClient.Integrations[out.Server.Id])

		servers, err := ts.McpGatewayStore.ListServers(t.Context(), syntax.DID(orgDID))
		require.NoError(t, err)
		require.Len(t, servers, 1)
	})
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
		fake := newFakeOAuthMcpServer(t)

		var addOut habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			adminTS.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{Org: orgDID, Name: "Linear", Url: fake.URL},
			&addOut,
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
			habitat.NetworkHabitatMcpStartAuthorizationInput{Org: orgDID, Id: addOut.Server.Id},
			&startOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, startOut.SessionToken)

		// The Nango Connect UI reports success to the frontend, which
		// confirms the resulting connection with the gateway.
		aliceTS.NangoClient.Connections["conn-1"] = addOut.Server.Id
		var confirmOut struct{}
		code = client.Procedure(
			aliceTS.Server.ConfirmConnection,
			habitat.NetworkHabitatMcpConfirmConnectionInput{
				Org: orgDID, Id: addOut.Server.Id, ConnectionId: "conn-1",
			},
			&confirmOut,
		)
		require.Equal(t, http.StatusOK, code)

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

func TestServer_StartAuthorization_NotOAuthServer(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	ts := newOpenSocialServer(t, admin)
	orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)
	srv := newFakeOpenMcpServer(t)

	var addOut habitat.NetworkHabitatMcpAddServerOutput
	code := client.Procedure(
		ts.Server.AddServer,
		habitat.NetworkHabitatMcpAddServerInput{Org: orgDID, Name: "Open", Url: srv.URL},
		&addOut,
	)
	require.Equal(t, http.StatusOK, code)

	var startOut habitat.NetworkHabitatMcpStartAuthorizationOutput
	code = client.Procedure(
		ts.Server.StartAuthorization,
		habitat.NetworkHabitatMcpStartAuthorizationInput{Org: orgDID, Id: addOut.Server.Id},
		&startOut,
	)
	require.Equal(t, http.StatusBadRequest, code)
}

func TestServer_RemoveServer(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("admin removes a server", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		fake := newFakeOAuthMcpServer(t)

		var addOut habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{Org: orgDID, Name: "Linear", Url: fake.URL},
			&addOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.True(t, ts.NangoClient.Integrations[addOut.Server.Id])

		var removeOut struct{}
		code = client.Procedure(
			ts.Server.RemoveServer,
			habitat.NetworkHabitatMcpRemoveServerInput{Org: orgDID, Id: addOut.Server.Id},
			&removeOut,
		)
		require.Equal(t, http.StatusOK, code)

		servers, err := ts.McpGatewayStore.ListServers(t.Context(), syntax.DID(orgDID))
		require.NoError(t, err)
		require.Empty(t, servers)
		require.False(t, ts.NangoClient.Integrations[addOut.Server.Id])
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

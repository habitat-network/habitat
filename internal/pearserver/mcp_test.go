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
			habitat.NetworkHabitatMcpAddServerInput{
				Org: orgDID, Name: "Server", Url: "https://mcp.example.com", AuthType: "oauth",
			},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("rejects an invalid url", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{
				Org: orgDID, Name: "docs", Url: "not a url", AuthType: "oauth",
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("admin adds an oauth server", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var addOut habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{
				Org: orgDID, Name: "Linear", Url: "https://mcp.example.com/mcp", AuthType: "oauth",
			},
			&addOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "Linear", addOut.Server.Name)
		require.Equal(t, "oauth", addOut.Server.AuthType)
		// Nobody is signed in yet, but the record and Nango integration
		// already exist.
		require.True(t, ts.NangoClient.Integrations[nangoKey(orgDID, addOut.Server.Id)])

		servers, err := ts.McpGatewayStore.ListServers(t.Context(), syntax.DID(orgDID), admin)
		require.NoError(t, err)
		require.Len(t, servers, 1)
		require.False(t, servers[0].Connected)
	})

	t.Run("admin adds a manual server", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var addOut habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{
				Org: orgDID, Name: "docs", Url: "https://mcp.example.com/mcp", AuthType: "manual",
			},
			&addOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "manual", addOut.Server.AuthType)

		servers, err := ts.McpGatewayStore.ListServers(t.Context(), syntax.DID(orgDID), admin)
		require.NoError(t, err)
		require.Len(t, servers, 1)
		require.True(t, servers[0].Connected)
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

		var addOut habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			adminTS.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{
				Org: orgDID, Name: "Linear", Url: "https://mcp.example.com/mcp", AuthType: "oauth",
			},
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

		// The Nango Connect UI reports success directly to the frontend
		// (which simply refetches); the gateway learns about it by asking
		// Nango, not by being told.
		aliceTS.NangoClient.Connect(
			"conn-alice",
			nangoKey(orgDID, addOut.Server.Id),
			alice.String(),
		)

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
			habitat.NetworkHabitatMcpAddServerInput{
				Org: orgDID, Name: "Linear", Url: "https://mcp.example.com/mcp", AuthType: "oauth",
			},
			&addOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.True(t, ts.NangoClient.Integrations[nangoKey(orgDID, addOut.Server.Id)])

		var removeOut struct{}
		code = client.Procedure(
			ts.Server.RemoveServer,
			habitat.NetworkHabitatMcpRemoveServerInput{Org: orgDID, Id: addOut.Server.Id},
			&removeOut,
		)
		require.Equal(t, http.StatusOK, code)

		servers, err := ts.McpGatewayStore.ListServers(t.Context(), syntax.DID(orgDID), admin)
		require.NoError(t, err)
		require.Empty(t, servers)
		require.False(t, ts.NangoClient.Integrations[nangoKey(orgDID, addOut.Server.Id)])
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

func TestServer_ManualServer(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("members see it connected, with nothing to authorize", func(t *testing.T) {
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
			habitat.NetworkHabitatMcpAddServerInput{
				Org:      orgDID,
				Name:     "docs",
				Url:      "https://mcp.example.com/mcp",
				AuthType: "manual",
			},
			&addOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "manual", addOut.Server.AuthType)

		var listOut habitat.NetworkHabitatMcpListServersOutput
		code = client.Query(
			aliceTS.Server.ListServers, url.Values{"org": []string{orgDID}}, &listOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.Len(t, listOut.Servers, 1)
		require.True(t, listOut.Servers[0].Connected)
		require.Equal(t, "manual", listOut.Servers[0].Server.AuthType)

		var startOut habitat.NetworkHabitatMcpStartAuthorizationOutput
		code = client.Procedure(
			aliceTS.Server.StartAuthorization,
			habitat.NetworkHabitatMcpStartAuthorizationInput{Org: orgDID, Id: "docs"},
			&startOut,
		)
		require.Equal(t, http.StatusBadRequest, code)

		var updateOut habitat.NetworkHabitatMcpUpdateServerOutput
		code = client.Procedure(
			adminTS.Server.UpdateServer,
			habitat.NetworkHabitatMcpUpdateServerInput{
				Org: orgDID, Id: "docs", Url: "https://other.example.com/mcp",
			},
			&updateOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "manual", updateOut.Server.AuthType)
	})
}

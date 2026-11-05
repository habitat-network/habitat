package pearserver_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/mcpgateway/mcpgatewaytest"
	"github.com/habitat-network/habitat/internal/pearserver"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
)

// mcpOAuthCallback plays the authorization server redirecting the member's
// browser back to pear's callback with query, returning the response.
func mcpOAuthCallback(
	ts *pearserver_testutil.TestServer,
	query url.Values,
) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet, pearserver.MCPOAuthCallbackPath+"?"+query.Encode(), http.NoBody,
	)
	ts.Server.McpOAuthCallback(rec, req)
	return rec
}

func TestServer_AddServer(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	fake := mcpgatewaytest.New(t)

	t.Run("requires mcp.configure", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		var out habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{
				Org: orgDID, Name: "Server", Url: fake.URL, AuthType: "oauth",
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

	t.Run("rejects an oauth server pear can't register with", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		noDCR := mcpgatewaytest.New(t, mcpgatewaytest.WithoutDCR())
		var out habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{
				Org: orgDID, Name: "Linear", Url: noDCR.URL, AuthType: "oauth",
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)

		// It works with a pre-registered client.
		code = client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{
				Org: orgDID, Name: "Linear", Url: noDCR.URL, AuthType: "oauth",
				ClientId: noDCR.RegisterClient(pearserver_testutil.MCPOAuthCallbackURL),
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
	})

	t.Run("admin adds an oauth server", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		var addOut habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{
				Org: orgDID, Name: "Linear", Url: fake.URL, AuthType: "oauth",
			},
			&addOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "Linear", addOut.Server.Name)
		require.Equal(t, "oauth", addOut.Server.AuthType)
		require.Equal(t, fake.URL, addOut.Server.Url)

		// Nobody is signed in yet.
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
	fake := mcpgatewaytest.New(t)

	t.Run("member lists servers, authorizes and disconnects", func(t *testing.T) {
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
				Org: orgDID, Name: "Linear", Url: fake.URL, AuthType: "oauth",
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
		require.NotEmpty(t, startOut.AuthorizationUrl)

		// Alice approves in her browser, which the authorization server sends
		// back to pear's callback.
		rec := mcpOAuthCallback(aliceTS, fake.Authorize(t, startOut.AuthorizationUrl))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "Connected")

		code = client.Query(
			aliceTS.Server.ListServers, url.Values{"org": []string{orgDID}}, &listOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.True(t, listOut.Servers[0].Connected)

		// The admin hasn't connected.
		code = client.Query(
			adminTS.Server.ListServers, url.Values{"org": []string{orgDID}}, &listOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.False(t, listOut.Servers[0].Connected)

		var none struct{}
		code = client.Procedure(
			aliceTS.Server.DisconnectServer,
			habitat.NetworkHabitatMcpDisconnectServerInput{Org: orgDID, Id: addOut.Server.Id},
			&none,
		)
		require.Equal(t, http.StatusOK, code)
		code = client.Procedure(
			aliceTS.Server.DisconnectServer,
			habitat.NetworkHabitatMcpDisconnectServerInput{Org: orgDID, Id: addOut.Server.Id},
			&none,
		)
		require.Equal(t, http.StatusNotFound, code)
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

func TestServer_McpOAuthCallback(t *testing.T) {
	ts := newOpenSocialServer(t, admin)

	rec := mcpOAuthCallback(ts, url.Values{"error": {"access_denied"}, "state": {"s"}})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "access_denied")

	rec = mcpOAuthCallback(ts, url.Values{"code": {"c"}, "state": {"unknown"}})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "expired")
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
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
	fake := mcpgatewaytest.New(t)

	t.Run("admin removes a server", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		var addOut habitat.NetworkHabitatMcpAddServerOutput
		code := client.Procedure(
			ts.Server.AddServer,
			habitat.NetworkHabitatMcpAddServerInput{
				Org: orgDID, Name: "Linear", Url: fake.URL, AuthType: "oauth",
			},
			&addOut,
		)
		require.Equal(t, http.StatusOK, code)

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

		code = client.Procedure(
			ts.Server.RemoveServer,
			habitat.NetworkHabitatMcpRemoveServerInput{Org: orgDID, Id: addOut.Server.Id},
			&removeOut,
		)
		require.Equal(t, http.StatusNotFound, code)
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
		require.Equal(t, "https://other.example.com/mcp", updateOut.Server.Url)

		code = client.Procedure(
			adminTS.Server.UpdateServer,
			habitat.NetworkHabitatMcpUpdateServerInput{Org: orgDID, Id: "docs", Url: "nope"},
			&updateOut,
		)
		require.Equal(t, http.StatusBadRequest, code)
		code = client.Procedure(
			adminTS.Server.UpdateServer,
			habitat.NetworkHabitatMcpUpdateServerInput{Org: orgDID, Id: "missing"},
			&updateOut,
		)
		require.Equal(t, http.StatusNotFound, code)
	})
}

func TestServer_TokenServer(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	adminTS, aliceTS, _ := newSharedOpenSocialServers(t)
	orgDID, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)
	require.NoError(
		t, adminTS.OpenSocialStore.AssignRoles(
			t.Context(), syntax.DID(orgDID), alice, []string{"member"},
		),
	)

	// The GitHub preset needs no OAuth client, since members bring tokens.
	var addOut habitat.NetworkHabitatMcpAddServerOutput
	code := client.Procedure(
		adminTS.Server.AddServer,
		habitat.NetworkHabitatMcpAddServerInput{Org: orgDID, Name: "github", Preset: "github"},
		&addOut,
	)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "token", addOut.Server.AuthType)

	connected := func() bool {
		var listOut habitat.NetworkHabitatMcpListServersOutput
		code := client.Query(
			aliceTS.Server.ListServers, url.Values{"org": []string{orgDID}}, &listOut,
		)
		require.Equal(t, http.StatusOK, code)
		require.Len(t, listOut.Servers, 1)
		return listOut.Servers[0].Connected
	}
	require.False(t, connected())

	// Connecting without a token is refused; with one, it connects with no
	// URL to visit.
	var startOut habitat.NetworkHabitatMcpStartAuthorizationOutput
	code = client.Procedure(
		aliceTS.Server.StartAuthorization,
		habitat.NetworkHabitatMcpStartAuthorizationInput{Org: orgDID, Id: "github"},
		&startOut,
	)
	require.Equal(t, http.StatusBadRequest, code)
	code = client.Procedure(
		aliceTS.Server.StartAuthorization,
		habitat.NetworkHabitatMcpStartAuthorizationInput{
			Org: orgDID, Id: "github", Token: "ghp_alice",
		},
		&startOut,
	)
	require.Equal(t, http.StatusOK, code)
	require.Empty(t, startOut.AuthorizationUrl)
	require.True(t, connected())

	code = client.Procedure(
		aliceTS.Server.DisconnectServer,
		habitat.NetworkHabitatMcpDisconnectServerInput{Org: orgDID, Id: "github"},
		nil,
	)
	require.Equal(t, http.StatusOK, code)
	require.False(t, connected())
}

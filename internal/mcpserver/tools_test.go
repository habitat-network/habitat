package mcpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/habitat-network/habitat/internal/mcpgateway"

	"github.com/stretchr/testify/require"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeConnectedServers is an in-memory stand-in for mcpgateway.Store,
// satisfying ConnectedServers.
type fakeConnectedServers struct {
	servers    map[syntax.DID][]*mcpgateway.Server // member -> servers
	downstream map[serverKey]*mcpgateway.Downstream
	// unreachable servers fail to resolve.
	unreachable map[mcpgateway.ServerName]error
}

// serverKey identifies a server: its name is only unique within its org.
type serverKey struct {
	org  syntax.DID
	name mcpgateway.ServerName
}

func newFakeConnectedServers() *fakeConnectedServers {
	return &fakeConnectedServers{
		servers:     make(map[syntax.DID][]*mcpgateway.Server),
		downstream:  make(map[serverKey]*mcpgateway.Downstream),
		unreachable: make(map[mcpgateway.ServerName]error),
	}
}

// connect connects member to orgID's server name at url, sending headers.
func (f *fakeConnectedServers) connect(
	member syntax.DID, orgID syntax.DID, name mcpgateway.ServerName, url string,
	headers map[string]string,
) {
	f.servers[member] = append(f.servers[member], &mcpgateway.Server{OrgID: orgID, Name: name})
	f.downstream[serverKey{orgID, name}] = &mcpgateway.Downstream{URL: url, Headers: headers}
}

func (f *fakeConnectedServers) ListConnectedServers(
	_ context.Context, did syntax.DID, orgID syntax.DID,
) ([]*mcpgateway.Server, error) {
	var out []*mcpgateway.Server
	for _, server := range f.servers[did] {
		if server.OrgID == orgID {
			out = append(out, server)
		}
	}
	return out, nil
}

func (f *fakeConnectedServers) Downstream(
	_ context.Context, _ syntax.DID, orgID syntax.DID, name mcpgateway.ServerName,
) (*mcpgateway.Downstream, error) {
	if err, ok := f.unreachable[name]; ok {
		return nil, err
	}
	d, ok := f.downstream[serverKey{orgID, name}]
	if !ok {
		return nil, mcpgateway.ErrNotConnected
	}
	return d, nil
}

// newFakeMCPServer stands in for a downstream MCP server the caller has
// connected to, exposing a "ping" tool and an "echo" tool.
//
// If authorization is given, requests without it as their Authorization
// header are refused.
func newFakeMCPServer(t *testing.T, authorization ...string) *httptest.Server {
	t.Helper()
	impl := &mcp.Implementation{Name: "fake-downstream-mcp", Version: "0.1.0"}
	srv := mcp.NewServer(impl, nil)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ping",
		Description: "Replies with pong.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, struct {
		Message string `json:"message"`
	}, error) {
		return nil, struct {
			Message string `json:"message"`
		}{Message: "pong"}, nil
	})
	type echoInput struct {
		Text string `json:"text"`
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "echo",
		Description: "Echoes text, failing on an empty one.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in echoInput) (*mcp.CallToolResult, any, error) {
		if in.Text == "" {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "nothing to echo"}},
			}, nil, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: in.Text}},
		}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv
	}, &mcp.StreamableHTTPOptions{DisableLocalhostProtection: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(authorization) > 0 && r.Header.Get("Authorization") != authorization[0] {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)
	return httpServer
}

// newConnectedServerTestServer starts pear's MCP server, with servers as the
// caller's connected servers.
func newConnectedServerTestServer(
	t *testing.T,
	servers mcpgateway.ConnectedServers,
) *httptest.Server {
	t.Helper()
	spacesStore, permStore := setupStores(t)
	srv := New(
		fakeTokens{},
		spacesStore,
		permStore,
		nil,
		servers,
		nil,
		newTestDirectory(),
		"https://habitat.example",
		"https://habitat.example/mcp",
	)
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer
}

func toolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	result, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	names := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		names[i] = tool.Name
	}
	return names
}

func TestMCPServerToolsList_MergesConnectedServerTools(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")

	fake := newFakeMCPServer(t)
	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "cloudflare", fake.URL, nil)
	httpServer := newConnectedServerTestServer(t, servers)

	session, err := connectAs(t, ctx, httpServer.URL, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	names := toolNames(t, session)
	require.Contains(t, names, "get_record")
	require.Contains(t, names, "cloudflare:ping")
}

func TestMCPServerToolsList_SkipsUnreachableConnectedServer(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")

	fake := newFakeMCPServer(t)
	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "cloudflare", fake.URL, nil)
	servers.connect(caller, testOrg, "broken", fake.URL, nil)
	servers.unreachable["broken"] = errors.New("token refresh failed")
	httpServer := newConnectedServerTestServer(t, servers)

	session, err := connectAs(t, ctx, httpServer.URL, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	names := toolNames(t, session)
	require.Contains(t, names, "cloudflare:ping")
	require.NotContains(t, names, "broken:ping")
}

func TestMCPServerToolsList_NoConnectedServers(t *testing.T) {
	ctx := t.Context()
	httpServer := newConnectedServerTestServer(t, nil)

	session, err := connectAs(t, ctx, httpServer.URL, "did:plc:caller")
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	require.Equal(t, []string{"get_blob", "get_record"}, toolNames(t, session))
}

func TestMCPServerToolsCall_ProxiesToConnectedServer(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")

	fake := newFakeMCPServer(t)
	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "cloudflare", fake.URL, nil)
	httpServer := newConnectedServerTestServer(t, servers)

	session, err := connectAs(t, ctx, httpServer.URL, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "cloudflare:ping"})
	require.NoError(t, err)
	require.False(t, result.IsError, "%+v", result)
	require.Equal(t, map[string]any{"message": "pong"}, result.StructuredContent)
}

func TestMCPServerToolsCall_UnknownNamespacedToolFallsThrough(t *testing.T) {
	ctx := t.Context()
	httpServer := newConnectedServerTestServer(t, newFakeConnectedServers())

	session, err := connectAs(t, ctx, httpServer.URL, "did:plc:caller")
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	_, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "unknown-server:ping"})
	require.Error(t, err)
}

func TestMCPServerTools_OAuthServer(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")

	// The downstream server rejects calls without the caller's token.
	protected := newFakeMCPServer(t, "Bearer secret")

	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "linear", protected.URL,
		map[string]string{"Authorization": "Bearer secret"})
	httpServer := newConnectedServerTestServer(t, servers)

	session, err := connectAs(t, ctx, httpServer.URL, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	require.Contains(t, toolNames(t, session), "linear:ping")
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "linear:ping"})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

// The endpoint of one org offers only that org's connected servers' tools, and
// calls go to that org's server even when another of the caller's orgs has one
// of the same name.
func TestMCPServerTools_ScopedToEndpointOrg(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")
	otherOrg := syntax.DID("did:plc:other-org")

	ours := newFakeMCPServer(t, "Bearer ours")
	theirs := newFakeMCPServer(t, "Bearer theirs")
	servers := newFakeConnectedServers()
	servers.connect(caller, otherOrg, "github", theirs.URL,
		map[string]string{"Authorization": "Bearer theirs"})
	servers.connect(caller, otherOrg, "slack", theirs.URL,
		map[string]string{"Authorization": "Bearer theirs"})
	servers.connect(caller, testOrg, "github", ours.URL,
		map[string]string{"Authorization": "Bearer ours"})
	httpServer := newConnectedServerTestServer(t, servers)

	session, err := connectAs(t, ctx, httpServer.URL, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	names := toolNames(t, session)
	require.Equal(t, 1, countOf(names, "github:ping"), names)
	require.NotContains(t, names, "slack:ping")

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "github:ping"})
	require.NoError(t, err)
	require.False(t, res.IsError, "%+v", res)
	_, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "slack:ping"})
	require.Error(t, err)
}

func countOf(names []string, name string) int {
	n := 0
	for _, got := range names {
		if got == name {
			n++
		}
	}
	return n
}

// Arguments reach the connected server as given, and its results, including
// tool errors, come back as it sent them.
func TestMCPServerToolsCall_PassesThroughArgumentsAndToolErrors(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")

	fake := newFakeMCPServer(t)
	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "cloudflare", fake.URL, nil)
	httpServer := newConnectedServerTestServer(t, servers)

	session, err := connectAs(t, ctx, httpServer.URL, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "cloudflare:echo", Arguments: map[string]any{"text": "hello"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, "hello", res.Content[0].(*mcp.TextContent).Text)

	res, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "cloudflare:echo", Arguments: map[string]any{"text": ""},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Equal(t, "nothing to echo", res.Content[0].(*mcp.TextContent).Text)

	// A tool the connected server doesn't have is its error to report.
	_, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "cloudflare:nope"})
	require.Error(t, err)
}

// A call to a connected server pear can't reach on the caller's behalf (say
// their sign-in expired) fails, without affecting pear's own tools.
func TestMCPServerToolsCall_UnreachableConnectedServer(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")

	fake := newFakeMCPServer(t)
	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "linear", fake.URL, nil)
	servers.unreachable["linear"] = mcpgateway.ErrNotConnected
	httpServer := newConnectedServerTestServer(t, servers)

	session, err := connectAs(t, ctx, httpServer.URL, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	_, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "linear:ping"})
	require.ErrorContains(t, err, "not connected")
	require.Contains(t, toolNames(t, session), "get_record")
}

// Connected servers' tools are listed in the order the servers are, however
// long each takes to list.
func TestMCPServerToolsList_KeepsServerOrder(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")

	servers := newFakeConnectedServers()
	names := []mcpgateway.ServerName{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	for _, name := range names {
		servers.connect(caller, testOrg, name, newFakeMCPServer(t).URL, nil)
	}
	httpServer := newConnectedServerTestServer(t, servers)

	session, err := connectAs(t, ctx, httpServer.URL, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	var pings []string
	for _, name := range toolNames(t, session) {
		if strings.HasSuffix(name, ":ping") {
			pings = append(pings, name)
		}
	}
	want := make([]string, len(names))
	for i, name := range names {
		want[i] = string(name) + ":ping"
	}
	require.Equal(t, want, pings)
}

func TestValidOutputSchema(t *testing.T) {
	object := map[string]any{"type": "object"}
	require.Equal(t, object, validOutputSchema(object))
	require.Nil(t, validOutputSchema(nil))
	require.Nil(t, validOutputSchema(map[string]any{"type": "array"}))
	require.Nil(t, validOutputSchema(map[string]any{"anyOf": []any{object}}))
}

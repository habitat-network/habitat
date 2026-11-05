package mcpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/habitat-network/habitat/internal/mcpgateway"
	"go.opentelemetry.io/otel/trace"

	"github.com/stretchr/testify/require"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeConnectedServers is an in-memory stand-in for mcpgateway.Store,
// satisfying ConnectedServers.
type fakeConnectedServers struct {
	servers    map[syntax.DID][]*mcpgateway.Server // member -> servers
	downstream map[mcpgateway.ServerName]*mcpgateway.Downstream
	// unreachable servers fail to resolve.
	unreachable map[mcpgateway.ServerName]error
}

func newFakeConnectedServers() *fakeConnectedServers {
	return &fakeConnectedServers{
		servers:     make(map[syntax.DID][]*mcpgateway.Server),
		downstream:  make(map[mcpgateway.ServerName]*mcpgateway.Downstream),
		unreachable: make(map[mcpgateway.ServerName]error),
	}
}

// connect connects member to the server name at url, sending headers.
func (f *fakeConnectedServers) connect(
	member syntax.DID, orgID syntax.DID, name mcpgateway.ServerName, url string,
	headers map[string]string,
) {
	f.servers[member] = append(f.servers[member], &mcpgateway.Server{OrgID: orgID, Name: name})
	f.downstream[name] = &mcpgateway.Downstream{URL: url, Headers: headers}
}

func (f *fakeConnectedServers) ListConnectedServers(
	_ context.Context, did syntax.DID,
) ([]*mcpgateway.Server, error) {
	return f.servers[did], nil
}

func (f *fakeConnectedServers) Downstream(
	_ context.Context, _ syntax.DID, _ syntax.DID, name mcpgateway.ServerName,
) (*mcpgateway.Downstream, error) {
	if err, ok := f.unreachable[name]; ok {
		return nil, err
	}
	d, ok := f.downstream[name]
	if !ok {
		return nil, mcpgateway.ErrNotConnected
	}
	return d, nil
}

// newFakeMCPServer stands in for a downstream MCP server the caller has
// connected to, exposing a single "ping" tool.
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
func newConnectedServerTestServer(t *testing.T, servers ConnectedServers) *httptest.Server {
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
	servers.connect(caller, "did:web:org1.example", "cloudflare", fake.URL, nil)
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
	servers.connect(caller, "did:web:org1.example", "cloudflare", fake.URL, nil)
	servers.connect(caller, "did:web:org1.example", "broken", fake.URL, nil)
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
	servers.connect(caller, "did:web:org1.example", "cloudflare", fake.URL, nil)
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

func TestDownstreamContext(t *testing.T) {
	type key struct{}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1},
		SpanID:  trace.SpanID{1},
	})
	parent, cancel := context.WithCancel(
		trace.ContextWithSpanContext(context.WithValue(t.Context(), key{}, "v"), spanCtx),
	)
	ctx := downstreamContext(parent)

	require.Nil(t, ctx.Value(key{}))
	require.Equal(t, spanCtx, trace.SpanContextFromContext(ctx))
	cancel()
	<-ctx.Done()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestMCPServerTools_OAuthServer(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")

	// The downstream server rejects calls without the caller's token.
	protected := newFakeMCPServer(t, "Bearer secret")

	servers := newFakeConnectedServers()
	servers.connect(caller, "did:web:org1.example", "linear", protected.URL,
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

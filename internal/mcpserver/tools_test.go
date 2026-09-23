package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/nango"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeNangoClient is an in-memory stand-in for internal/nango.Client,
// satisfying NangoClient, so tests don't need real Nango credentials or
// network access.
type fakeNangoClient struct {
	connections map[string][]nango.Connection       // endUserID -> connections
	details     map[string]*nango.ConnectionDetails // connectionID -> details
	detailsErr  map[string]error                    // connectionID -> error, if any
}

func newFakeNangoClient() *fakeNangoClient {
	return &fakeNangoClient{
		connections: make(map[string][]nango.Connection),
		details:     make(map[string]*nango.ConnectionDetails),
		detailsErr:  make(map[string]error),
	}
}

// connect registers a connection for endUserID to the MCP server at url,
// with no authorization required.
func (f *fakeNangoClient) connect(endUserID, connectionID, providerConfigKey, orgID, url string) {
	f.connections[endUserID] = append(f.connections[endUserID], nango.Connection{
		ConnectionID:      connectionID,
		ProviderConfigKey: providerConfigKey,
		OrgID:             orgID,
	})
	f.details[connectionID] = &nango.ConnectionDetails{MCPServerURL: url}
}

func (f *fakeNangoClient) ListConnections(
	ctx context.Context, endUserID string,
) ([]nango.Connection, error) {
	return f.connections[endUserID], nil
}

func (f *fakeNangoClient) GetConnection(
	ctx context.Context, connectionID, providerConfigKey string,
) (*nango.ConnectionDetails, error) {
	if err, ok := f.detailsErr[connectionID]; ok {
		return nil, err
	}
	details, ok := f.details[connectionID]
	if !ok {
		return nil, fmt.Errorf("no connection %q", connectionID)
	}
	return details, nil
}

// fakeOrgMcpServerStore is an in-memory stand-in for opensocial.Store,
// satisfying OrgMcpServerStore.
type fakeOrgMcpServerStore struct {
	servers map[string][]*opensocial.McpServer // orgDID -> servers
}

func newFakeOrgMcpServerStore() *fakeOrgMcpServerStore {
	return &fakeOrgMcpServerStore{servers: make(map[string][]*opensocial.McpServer)}
}

// add registers a server record for orgDID, as mcpgateway.CompleteAddServer would.
func (f *fakeOrgMcpServerStore) add(orgDID, name, nangoKey string) {
	f.servers[orgDID] = append(f.servers[orgDID], &opensocial.McpServer{
		ID:       syntax.RecordKey(name),
		Name:     name,
		NangoKey: nangoKey,
	})
}

func (f *fakeOrgMcpServerStore) GetMcpServer(
	ctx context.Context, orgDID syntax.DID, id syntax.RecordKey,
) (*opensocial.McpServer, error) {
	for _, srv := range f.servers[orgDID.String()] {
		if srv.ID == id {
			return srv, nil
		}
	}
	return nil, opensocial.ErrMcpServerNotFound
}

func (f *fakeOrgMcpServerStore) ListMcpServers(
	ctx context.Context, orgDID syntax.DID,
) ([]*opensocial.McpServer, error) {
	return f.servers[orgDID.String()], nil
}

// newFakeMCPServer stands in for a downstream MCP server the caller has
// connected to, exposing a single "ping" tool.
func newFakeMCPServer(t *testing.T) *httptest.Server {
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
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	return httpServer
}

func TestMCPServerToolsList_MergesConnectedServerTools(t *testing.T) {
	ctx := t.Context()
	spacesStore, permStore := setupStores(t)
	caller := "did:plc:caller"
	const orgID = "did:web:org1.example"

	fake := newFakeMCPServer(t)
	nangoClient := newFakeNangoClient()
	nangoClient.connect(caller, "conn-1", "nango-key-1", orgID, fake.URL)
	orgRecords := newFakeOrgMcpServerStore()
	orgRecords.add(orgID, "cloudflare", "nango-key-1")

	srv := New(fakeTokens{}, spacesStore, permStore, nangoClient, orgRecords, "https://habitat.example")
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	session, err := connectAs(t, ctx, httpServer.URL, caller)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	result, err := session.ListTools(ctx, nil)
	require.NoError(t, err)

	names := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		names[i] = tool.Name
	}
	require.Contains(t, names, "get_record")
	require.Contains(t, names, "cloudflare:ping")
}

func TestMCPServerToolsList_SkipsUnreachableConnectedServer(t *testing.T) {
	ctx := t.Context()
	spacesStore, permStore := setupStores(t)
	caller := "did:plc:caller"
	const orgID = "did:web:org1.example"

	nangoClient := newFakeNangoClient()
	nangoClient.connections[caller] = []nango.Connection{
		{ConnectionID: "conn-1", ProviderConfigKey: "nango-key-1", OrgID: orgID},
	}
	nangoClient.detailsErr["conn-1"] = errors.New("nango unavailable")
	orgRecords := newFakeOrgMcpServerStore()
	orgRecords.add(orgID, "cloudflare", "nango-key-1")

	srv := New(fakeTokens{}, spacesStore, permStore, nangoClient, orgRecords, "https://habitat.example")
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	session, err := connectAs(t, ctx, httpServer.URL, caller)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	result, err := session.ListTools(ctx, nil)
	require.NoError(t, err)

	names := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		names[i] = tool.Name
	}
	require.Contains(t, names, "get_record")
	require.Len(t, names, 1) // the unreachable connected server contributed nothing
}

func TestMCPServerToolsList_SkipsConnectionWithNoMatchingRecord(t *testing.T) {
	ctx := t.Context()
	spacesStore, permStore := setupStores(t)
	caller := "did:plc:caller"
	const orgID = "did:web:org1.example"

	fake := newFakeMCPServer(t)
	nangoClient := newFakeNangoClient()
	nangoClient.connect(caller, "conn-1", "nango-key-1", orgID, fake.URL)
	// No matching org record registered (e.g. it was since removed).

	srv := New(
		fakeTokens{}, spacesStore, permStore, nangoClient, newFakeOrgMcpServerStore(), "https://habitat.example",
	)
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	session, err := connectAs(t, ctx, httpServer.URL, caller)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	result, err := session.ListTools(ctx, nil)
	require.NoError(t, err)

	names := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		names[i] = tool.Name
	}
	require.Equal(t, []string{"get_record"}, names)
}

func TestMCPServerToolsCall_ProxiesToConnectedServer(t *testing.T) {
	ctx := t.Context()
	spacesStore, permStore := setupStores(t)
	caller := "did:plc:caller"
	const orgID = "did:web:org1.example"

	fake := newFakeMCPServer(t)
	nangoClient := newFakeNangoClient()
	nangoClient.connect(caller, "conn-1", "nango-key-1", orgID, fake.URL)
	orgRecords := newFakeOrgMcpServerStore()
	orgRecords.add(orgID, "cloudflare", "nango-key-1")

	srv := New(fakeTokens{}, spacesStore, permStore, nangoClient, orgRecords, "https://habitat.example")
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	session, err := connectAs(t, ctx, httpServer.URL, caller)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "cloudflare:ping"})
	require.NoError(t, err)
	require.False(t, result.IsError, "%+v", result)
	require.Equal(t, map[string]any{"message": "pong"}, result.StructuredContent)
}

func TestMCPServerToolsCall_UnknownNamespacedToolFallsThrough(t *testing.T) {
	ctx := t.Context()
	spacesStore, permStore := setupStores(t)
	caller := "did:plc:caller"

	srv := New(
		fakeTokens{}, spacesStore, permStore, newFakeNangoClient(), newFakeOrgMcpServerStore(),
		"https://habitat.example",
	)
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	session, err := connectAs(t, ctx, httpServer.URL, caller)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	_, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "unknown-server:ping"})
	require.Error(t, err)
}

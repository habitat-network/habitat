package mcpserver

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/toolsearch"
)

// fakeToolCatalog is an in-memory ToolCatalog matching a query's words
// against tool names and descriptions.
type fakeToolCatalog struct {
	mu    sync.Mutex
	tools map[toolCatalogKey][]toolsearch.Tool
	// replaced counts ReplaceServer calls.
	replaced int
}

func newFakeToolCatalog() *fakeToolCatalog {
	return &fakeToolCatalog{tools: map[toolCatalogKey][]toolsearch.Tool{}}
}

func (f *fakeToolCatalog) ReplaceServer(
	_ context.Context, org, member syntax.DID, server string, tools []toolsearch.Tool,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replaced++
	key := toolCatalogKey{org, member}
	var kept []toolsearch.Tool
	for _, t := range f.tools[key] {
		if t.Server != server {
			kept = append(kept, t)
		}
	}
	f.tools[key] = append(kept, tools...)
	return nil
}

func (f *fakeToolCatalog) PruneServers(
	_ context.Context, org, member syntax.DID, keep []string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := toolCatalogKey{org, member}
	var kept []toolsearch.Tool
	for _, t := range f.tools[key] {
		for _, k := range keep {
			if t.Server == k {
				kept = append(kept, t)
				break
			}
		}
	}
	f.tools[key] = kept
	return nil
}

func (f *fakeToolCatalog) Search(
	_ context.Context, org, member syntax.DID, query string, limit int,
) ([]toolsearch.Hit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var hits []toolsearch.Hit
	for _, t := range f.tools[toolCatalogKey{org, member}] {
		text := strings.ToLower(t.Name + " " + t.Description)
		for _, w := range strings.Fields(strings.ToLower(query)) {
			if strings.Contains(text, w) {
				hits = append(hits, toolsearch.Hit{
					Server: t.Server, Name: t.Name, Description: t.Description,
				})
				break
			}
		}
	}
	return hits, nil
}

func (f *fakeToolCatalog) Get(
	_ context.Context, org, member syntax.DID, server, name string,
) (*toolsearch.Tool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tools[toolCatalogKey{org, member}] {
		if t.Server == server && t.Name == name {
			return &t, nil
		}
	}
	return nil, toolsearch.ErrNotFound
}

// newToolSearchTestServer starts pear's MCP server with tool search over
// catalog.
func newToolSearchTestServer(
	t *testing.T, servers ConnectedServers, catalog ToolCatalog,
) string {
	t.Helper()
	spacesStore, permStore := setupStores(t)
	srv := New(
		fakeTokens{}, spacesStore, permStore, nil, servers, nil, newTestDirectory(),
		"https://habitat.example", "https://habitat.example/mcp",
		WithToolSearch(catalog),
	)
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

func callStructured(
	t *testing.T, session *mcp.ClientSession, name string, args any,
) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	return result
}

func TestToolSearch_ListsOnlyMetaTools(t *testing.T) {
	caller := syntax.DID("did:plc:caller")
	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "cloudflare", newFakeMCPServer(t).URL, nil)
	url := newToolSearchTestServer(t, servers, newFakeToolCatalog())

	session, err := connectAs(t, t.Context(), url, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	names := toolNames(t, session)
	require.Contains(t, names, "search_tools")
	require.Contains(t, names, "get_tool_details")
	require.Contains(t, names, "call_tool")
	require.NotContains(t, names, "cloudflare:ping")
}

func TestToolSearch_SearchDetailsAndCall(t *testing.T) {
	caller := syntax.DID("did:plc:caller")
	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "cloudflare", newFakeMCPServer(t).URL, nil)
	catalog := newFakeToolCatalog()
	url := newToolSearchTestServer(t, servers, catalog)

	session, err := connectAs(t, t.Context(), url, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	// The first search lists the server's tools into the catalog.
	res := callStructured(t, session, "search_tools", map[string]any{"query": "echoes"})
	require.False(t, res.IsError, "%+v", res)
	require.Equal(t, map[string]any{"tools": []any{map[string]any{
		"name":        "cloudflare:echo",
		"server":      "cloudflare",
		"description": "Echoes text, failing on an empty one.",
	}}}, res.StructuredContent)

	res = callStructured(t, session, "get_tool_details", map[string]any{"name": "cloudflare:echo"})
	require.False(t, res.IsError, "%+v", res)
	tool := res.StructuredContent.(map[string]any)["tool"].(map[string]any)
	require.Equal(t, "cloudflare:echo", tool["name"])
	require.Contains(t, tool["inputSchema"].(map[string]any)["properties"], "text")

	res = callStructured(t, session, "call_tool", map[string]any{
		"name": "cloudflare:echo", "arguments": map[string]any{"text": "hello"},
	})
	require.False(t, res.IsError, "%+v", res)
	require.Equal(t, "hello", res.Content[0].(*mcp.TextContent).Text)

	// A tool's own error comes back as the tool's error.
	res = callStructured(t, session, "call_tool", map[string]any{
		"name": "cloudflare:echo", "arguments": map[string]any{"text": ""},
	})
	require.True(t, res.IsError)
}

func TestToolSearch_CatalogIsReusedUntilStale(t *testing.T) {
	caller := syntax.DID("did:plc:caller")
	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "cloudflare", newFakeMCPServer(t).URL, nil)
	catalog := newFakeToolCatalog()
	url := newToolSearchTestServer(t, servers, catalog)

	session, err := connectAs(t, t.Context(), url, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	callStructured(t, session, "search_tools", map[string]any{"query": "ping"})
	callStructured(t, session, "search_tools", map[string]any{"query": "echo"})
	require.Equal(t, 1, catalog.replaced, "the second search reuses the catalog")
}

func TestToolSearch_UnknownToolResyncsThenFails(t *testing.T) {
	caller := syntax.DID("did:plc:caller")
	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "cloudflare", newFakeMCPServer(t).URL, nil)
	catalog := newFakeToolCatalog()
	url := newToolSearchTestServer(t, servers, catalog)

	session, err := connectAs(t, t.Context(), url, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	res := callStructured(t, session, "get_tool_details", map[string]any{"name": "cloudflare:nope"})
	require.True(t, res.IsError)
	require.Equal(t, 1, catalog.replaced, "an unknown tool resyncs once")

	res = callStructured(t, session, "get_tool_details", map[string]any{"name": "ping"})
	require.True(t, res.IsError, "names must be namespaced")

	// A tool added since the catalog was built is found by resyncing.
	callStructured(t, session, "search_tools", map[string]any{"query": "ping"})
	require.NoError(t, catalog.ReplaceServer(
		t.Context(), testOrg, caller, "cloudflare", nil,
	))
	res = callStructured(t, session, "get_tool_details", map[string]any{"name": "cloudflare:ping"})
	require.False(t, res.IsError, "%+v", res)
}

func TestToolSearch_UnreachableServerKeepsItsTools(t *testing.T) {
	caller := syntax.DID("did:plc:caller")
	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "cloudflare", newFakeMCPServer(t).URL, nil)
	servers.connect(caller, testOrg, "broken", newFakeMCPServer(t).URL, nil)
	catalog := newFakeToolCatalog()
	syncer := newToolSyncer(catalog, servers)

	require.NoError(t, syncer.ensureFresh(t.Context(), testOrg, caller, false))
	hits, err := catalog.Search(t.Context(), testOrg, caller, "ping", 10)
	require.NoError(t, err)
	require.Len(t, hits, 2)

	servers.unreachable["broken"] = errors.New("token refresh failed")
	require.NoError(t, syncer.ensureFresh(t.Context(), testOrg, caller, true))
	hits, err = catalog.Search(t.Context(), testOrg, caller, "ping", 10)
	require.NoError(t, err)
	require.Len(t, hits, 2, "an outage doesn't empty the catalog")

	// A server the caller is no longer connected to loses its tools.
	servers.servers[caller] = servers.servers[caller][:1]
	require.NoError(t, syncer.ensureFresh(t.Context(), testOrg, caller, true))
	hits, err = catalog.Search(t.Context(), testOrg, caller, "ping", 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, "cloudflare", hits[0].Server)
}

func TestToolSearch_CallToolUnknownServer(t *testing.T) {
	caller := syntax.DID("did:plc:caller")
	url := newToolSearchTestServer(t, newFakeConnectedServers(), newFakeToolCatalog())
	session, err := connectAs(t, t.Context(), url, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	res := callStructured(t, session, "call_tool", map[string]any{"name": "nowhere:ping"})
	require.True(t, res.IsError)
	res = callStructured(t, session, "call_tool", map[string]any{"name": "ping"})
	require.True(t, res.IsError)
}

func TestTruncate(t *testing.T) {
	require.Equal(t, "short", truncate("short", 10))
	require.Equal(t, "héllo…", truncate("héllo wörld", 6))
}

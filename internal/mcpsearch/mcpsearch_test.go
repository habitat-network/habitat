package mcpsearch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/habitat-network/habitat/internal/mcpgateway"
)

var (
	testCaller = syntax.DID("did:plc:caller")
	testOrg    = syntax.DID("did:plc:org")
)

// fakeConnectedServers is an in-memory stand-in for mcpgateway.Store,
// satisfying ConnectedServers for testCaller in testOrg.
type fakeConnectedServers struct {
	servers    []*mcpgateway.Server
	downstream map[mcpgateway.ServerName]*mcpgateway.Downstream
	// failures make Downstream fail for a server, e.g. with an expired
	// sign-in.
	failures map[mcpgateway.ServerName]error
	listErr  error
}

func newFakeConnectedServers() *fakeConnectedServers {
	return &fakeConnectedServers{
		downstream: make(map[mcpgateway.ServerName]*mcpgateway.Downstream),
		failures:   make(map[mcpgateway.ServerName]error),
	}
}

// connect connects testCaller to the server name at url.
func (f *fakeConnectedServers) connect(name mcpgateway.ServerName, url string) {
	f.servers = append(f.servers, &mcpgateway.Server{OrgID: testOrg, Name: name, URL: url})
	f.downstream[name] = &mcpgateway.Downstream{URL: url}
}

func (f *fakeConnectedServers) ListConnectedServers(
	_ context.Context, did syntax.DID, orgID syntax.DID,
) ([]*mcpgateway.Server, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if did != testCaller || orgID != testOrg {
		return nil, nil
	}
	return f.servers, nil
}

func (f *fakeConnectedServers) Downstream(
	_ context.Context, _ syntax.DID, _ syntax.DID, name mcpgateway.ServerName,
) (*mcpgateway.Downstream, error) {
	if err, ok := f.failures[name]; ok {
		return nil, err
	}
	d, ok := f.downstream[name]
	if !ok {
		return nil, mcpgateway.ErrNotConnected
	}
	return d, nil
}

type searchInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

// newFakeSearchServer starts a fake downstream MCP server, returning its
// URL. Its tools:
//   - "search" returns limit+2 JSON results named after the query and label,
//     so tests can tell servers apart and see the limit enforced.
//   - "structured" returns one result as structured content.
//   - "fail" answers with a tool error.
//   - "hang" blocks until its call is cancelled.
//   - "cancel" calls onCancel, then blocks like "hang".
func newFakeSearchServer(t *testing.T, label string, onCancel func()) string {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "fake-search", Version: "0.1.0"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "search"}, func(
		_ context.Context, _ *mcp.CallToolRequest, in searchInput,
	) (*mcp.CallToolResult, any, error) {
		var results []string
		for i := range in.Limit + 2 {
			results = append(results, fmt.Sprintf(
				`{"name":"%s-%s-%d","link":"https://%s.example/%d","text":"about %s"}`,
				label, in.Query, i, label, i, in.Query,
			))
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{
			Text: `{"results":[` + strings.Join(results, ",") + `]}`,
		}}}, nil, nil
	})
	mcp.AddTool(srv, &mcp.Tool{Name: "structured"}, func(
		_ context.Context, _ *mcp.CallToolRequest, in searchInput,
	) (*mcp.CallToolResult, any, error) {
		return nil, map[string]any{"results": []any{map[string]any{
			"name": label + "-structured-" + in.Query, "link": "https://" + label + ".example/s",
		}}}, nil
	})
	mcp.AddTool(srv, &mcp.Tool{Name: "fail"}, func(
		_ context.Context, _ *mcp.CallToolRequest, _ searchInput,
	) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{
			IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "rate limited"}},
		}, nil, nil
	})
	release := make(chan struct{})
	hang := func(ctx context.Context) (*mcp.CallToolResult, any, error) {
		select {
		case <-ctx.Done():
		case <-release:
		}
		return nil, nil, fmt.Errorf("gave up")
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "hang"}, func(
		ctx context.Context, _ *mcp.CallToolRequest, _ searchInput,
	) (*mcp.CallToolResult, any, error) {
		return hang(ctx)
	})
	mcp.AddTool(srv, &mcp.Tool{Name: "cancel"}, func(
		ctx context.Context, _ *mcp.CallToolRequest, _ searchInput,
	) (*mcp.CallToolResult, any, error) {
		onCancel()
		return hang(ctx)
	})
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{DisableLocalhostProtection: true},
	))
	// Unblock hanging tools before Close waits for their requests.
	t.Cleanup(httpServer.Close)
	t.Cleanup(func() { close(release) })
	return httpServer.URL + "/mcp"
}

// testProvider is a ToolProvider for one of newFakeSearchServer's tools at
// url.
func testProvider(id ProviderID, url, tool string) *ToolProvider {
	return &ToolProvider{
		Name: id,
		URL:  url,
		Tool: tool,
		Args: func(q Query) map[string]any {
			return map[string]any{"query": q.Text, "limit": q.Limit}
		},
		ItemsPath:    "results",
		TitlePaths:   []string{"name"},
		URLPaths:     []string{"link"},
		SnippetPaths: []string{"text"},
	}
}

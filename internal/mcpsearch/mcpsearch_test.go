package mcpsearch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

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

func titles(items []Item) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Title
	}
	return out
}

// statuses maps each source's "server/provider" to its status.
func statuses(sources []Source) map[string]SourceStatus {
	out := make(map[string]SourceStatus, len(sources))
	for _, s := range sources {
		key := string(s.ServerName)
		if key == "" {
			key = s.ServerURL
		}
		out[key+"/"+string(s.Provider)] = s.Status
	}
	return out
}

func TestSearcherSearchMergesServersInParallel(t *testing.T) {
	alpha := newFakeSearchServer(t, "alpha", nil)
	beta := newFakeSearchServer(t, "beta", nil)
	servers := newFakeConnectedServers()
	servers.connect("alpha", alpha)
	servers.connect("beta", beta)
	registry := NewRegistry(
		testProvider("alpha-search", alpha, "search"),
		testProvider("beta-search", beta, "search"),
		testProvider("beta-structured", beta, "structured"),
	)

	// beta is requested first, and with a trailing slash; alpha twice.
	res, err := NewSearcher(servers, registry).Search(
		t.Context(), testCaller, testOrg, []string{beta + "/", alpha, alpha}, Query{
			Text: " roadmap ", Limit: 1,
		},
	)
	require.NoError(t, err)

	require.Equal(t, []string{
		"beta-roadmap-0", "beta-structured-roadmap", "alpha-roadmap-0",
	}, titles(res.Items), "grouped by requested server, then provider, limited to 1 each")
	require.Equal(t, Item{
		Source:  ItemSource{ServerURL: beta, ServerName: "beta", Provider: "beta-search"},
		Title:   "beta-roadmap-0",
		URL:     "https://beta.example/0",
		Snippet: "about roadmap",
	}, res.Items[0])
	require.Equal(t, []Source{
		{ServerURL: beta, ServerName: "beta", Provider: "beta-search", Status: StatusOK, Count: 1},
		{ServerURL: beta, ServerName: "beta", Provider: "beta-structured", Status: StatusOK, Count: 1},
		{ServerURL: alpha, ServerName: "alpha", Provider: "alpha-search", Status: StatusOK, Count: 1},
	}, res.Sources)
}

func TestSearcherSearchReportsEachServersFailure(t *testing.T) {
	healthy := newFakeSearchServer(t, "healthy", nil)
	expired := newFakeSearchServer(t, "expired", nil)
	failing := newFakeSearchServer(t, "failing", nil)
	slow := newFakeSearchServer(t, "slow", nil)
	unsupported := newFakeSearchServer(t, "unsupported", nil)
	const unconnected = "https://unconnected.example/mcp"

	servers := newFakeConnectedServers()
	for name, url := range map[mcpgateway.ServerName]string{
		"healthy": healthy, "expired": expired, "failing": failing, "slow": slow,
		"unsupported": unsupported,
	} {
		servers.connect(name, url)
	}
	servers.failures["expired"] = fmt.Errorf(
		"%w: sign-in expired", mcpgateway.ErrNotConnected,
	)
	registry := NewRegistry(
		testProvider("healthy", healthy, "search"),
		testProvider("expired", expired, "search"),
		testProvider("failing", failing, "fail"),
		testProvider("failing-ok", failing, "search"),
		testProvider("slow", slow, "hang"),
		testProvider("unconnected", unconnected, "search"),
	)

	start := time.Now()
	res, err := NewSearcher(servers, registry, WithTimeout(500*time.Millisecond)).Search(
		t.Context(), testCaller, testOrg,
		[]string{healthy, expired, failing, slow, unsupported, unconnected},
		Query{Text: "q", Limit: 2},
	)
	require.NoError(t, err)
	// The hung server is given up on at its timeout, not held open by the
	// go-sdk's 5s session teardown.
	require.Less(t, time.Since(start), 4*time.Second)

	require.Equal(t, []string{
		"healthy-q-0", "healthy-q-1", "failing-q-0", "failing-q-1",
	}, titles(res.Items), "one server's or tool's failure doesn't drop the others' items")
	require.Equal(t, map[string]SourceStatus{
		"healthy/healthy":    StatusOK,
		"expired/expired":    StatusNotConnected,
		"failing/failing":    StatusError,
		"failing/failing-ok": StatusOK,
		"slow/slow":          StatusTimeout,
		"unsupported/":       StatusUnsupported,
		unconnected + "/":    StatusNotConnected,
	}, statuses(res.Sources))
	for _, s := range res.Sources {
		if s.Provider == "failing" {
			require.Contains(t, s.Error, "rate limited")
		}
		if s.Status != StatusOK {
			require.NotEmpty(t, s.Error)
		}
	}
}

func TestSearcherSearchWithoutURLsSearchesEverySupportedServer(t *testing.T) {
	alpha := newFakeSearchServer(t, "alpha", nil)
	unsupported := newFakeSearchServer(t, "unsupported", nil)
	servers := newFakeConnectedServers()
	servers.connect("alpha", alpha)
	servers.connect("unsupported", unsupported)
	// A provider for a server the caller isn't connected to isn't searched.
	registry := NewRegistry(
		testProvider("alpha", alpha, "search"),
		testProvider("other", "https://other.example/mcp", "search"),
	)

	res, err := NewSearcher(servers, registry).Search(
		t.Context(), testCaller, testOrg, nil, Query{Text: "q", Limit: 1},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"alpha-q-0"}, titles(res.Items))
	require.Equal(t, map[string]SourceStatus{"alpha/alpha": StatusOK}, statuses(res.Sources))
}

func TestSearcherSearchNoServers(t *testing.T) {
	res, err := NewSearcher(newFakeConnectedServers(), NewRegistry()).Search(
		t.Context(), testCaller, testOrg, nil, Query{Text: "q"},
	)
	require.NoError(t, err)
	require.Empty(t, res.Items)
	require.NotNil(t, res.Items, "an empty result encodes as [], not null")
	require.Empty(t, res.Sources)
}

func TestSearcherSearchCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	url := newFakeSearchServer(t, "alpha", cancel)
	servers := newFakeConnectedServers()
	servers.connect("alpha", url)

	_, err := NewSearcher(servers, NewRegistry(testProvider("alpha", url, "cancel"))).Search(
		ctx, testCaller, testOrg, []string{url}, Query{Text: "q"},
	)
	require.ErrorIs(t, err, context.Canceled)
}

func TestSearcherSearchRejectsInvalidInput(t *testing.T) {
	servers := newFakeConnectedServers()
	servers.listErr = fmt.Errorf("db down")
	searcher := NewSearcher(servers, NewRegistry())
	for name, tc := range map[string]struct {
		urls []string
		q    Query
		want error
	}{
		"empty query":    {q: Query{Text: "  "}, want: ErrEmptyQuery},
		"negative limit": {q: Query{Text: "q", Limit: -1}, want: ErrInvalidLimit},
		"limit too big":  {q: Query{Text: "q", Limit: MaxLimit + 1}, want: ErrInvalidLimit},
		"relative url":   {urls: []string{"/mcp"}, q: Query{Text: "q"}, want: ErrInvalidServerURL},
		"non-http url": {
			urls: []string{"ftp://x.example"}, q: Query{Text: "q"}, want: ErrInvalidServerURL,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := searcher.Search(t.Context(), testCaller, testOrg, tc.urls, tc.q)
			require.ErrorIs(t, err, tc.want)
		})
	}

	_, err := searcher.Search(t.Context(), testCaller, testOrg, nil, Query{Text: "q"})
	require.ErrorContains(t, err, "db down", "servers that can't be listed fail the search")
}

func TestSearcherSearchTruncatesLongSnippets(t *testing.T) {
	url := newFakeSearchServer(t, "alpha", nil)
	servers := newFakeConnectedServers()
	servers.connect("alpha", url)
	long := strings.Repeat("é", maxSnippetRunes+10)
	provider := testProvider("alpha", url, "search")
	// The fake server's snippet is "about <query>".
	res, err := NewSearcher(servers, NewRegistry(provider)).Search(
		t.Context(), testCaller, testOrg, []string{url}, Query{Text: long, Limit: 1},
	)
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	require.Equal(t, maxSnippetRunes, len([]rune(res.Items[0].Snippet)))
	require.True(t, strings.HasSuffix(res.Items[0].Snippet, "…"))
}

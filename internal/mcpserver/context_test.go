package mcpserver

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/mcpsearch"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

type fakeFederated struct {
	caller syntax.DID
	org    syntax.DID
	query  mcpsearch.Query
	result *mcpsearch.Result
	err    error
}

func (f *fakeFederated) Search(
	_ context.Context, caller syntax.DID, org syntax.DID, _ []string, q mcpsearch.Query,
) (*mcpsearch.Result, error) {
	f.caller, f.org, f.query = caller, org, q
	return f.result, f.err
}

type failingRecordSearcher struct{}

func (failingRecordSearcher) Search(
	context.Context, syntax.DID, search.Query,
) (search.Page, error) {
	return search.Page{}, errors.New("boom")
}

func newContextTestServer(
	t *testing.T,
	servers mcpgateway.ConnectedServers,
	records RecordSearcher,
	opts ...Option,
) *httptest.Server {
	t.Helper()
	spacesStore, permStore := setupStores(t)
	srv := New(
		fakeTokens{}, spacesStore, permStore, records, servers, nil, newTestDirectory(),
		"https://habitat.example", "https://habitat.example/mcp", opts...,
	)
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer
}

func TestGetContext(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")
	fake := newFakeMCPServer(t)
	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "cloudflare", fake.URL, nil)

	t.Run("gathers federated results and suggests tools", func(t *testing.T) {
		federated := &fakeFederated{result: &mcpsearch.Result{
			Items: []mcpsearch.Item{{
				Source: mcpsearch.ItemSource{ServerName: "cloudflare"},
				Title:  "A doc", URL: "https://example.com/doc", Snippet: "hi",
			}},
			Sources: []mcpsearch.Source{
				{ServerName: "drive", Status: mcpsearch.StatusNotConnected},
				{ServerName: "cloudflare", Status: mcpsearch.StatusOK},
			},
		}}
		httpServer := newContextTestServer(t, servers, nil, WithFederatedSearcher(federated))
		session, err := connectAs(t, ctx, httpServer.URL, caller.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_context",
			Arguments: map[string]any{"query": "ping the server"},
		})
		require.NoError(t, err)
		require.False(t, result.IsError, "%+v", result)
		require.Equal(t, caller, federated.caller)
		require.Equal(t, testOrg, federated.org)
		require.Equal(t, "ping the server", federated.query.Text)

		out := result.StructuredContent.(map[string]any)
		items := out["items"].([]any)
		require.Len(t, items, 1)
		require.Equal(t, "A doc", items[0].(map[string]any)["title"])
		tools := out["suggested_tools"].([]any)
		require.NotEmpty(t, tools)
		require.Equal(t, "cloudflare:ping", tools[0].(map[string]any)["name"])
		require.Equal(t, []any{"drive: not connected"}, out["notes"])
	})

	t.Run("a failing source is a note, not an error", func(t *testing.T) {
		federated := &fakeFederated{err: errors.New("boom")}
		httpServer := newContextTestServer(
			t, servers, failingRecordSearcher{}, WithFederatedSearcher(federated),
		)
		session, err := connectAs(t, ctx, httpServer.URL, caller.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_context",
			Arguments: map[string]any{"query": "ping"},
		})
		require.NoError(t, err)
		require.False(t, result.IsError, "%+v", result)
		out := result.StructuredContent.(map[string]any)
		require.ElementsMatch(t,
			[]any{"Habitat record search failed", "searching connected apps failed"},
			out["notes"])
		require.NotEmpty(t, out["suggested_tools"])
	})

	t.Run("max_tools 0 suggests nothing and bad values fail", func(t *testing.T) {
		httpServer := newContextTestServer(t, servers, nil)
		session, err := connectAs(t, ctx, httpServer.URL, caller.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_context",
			Arguments: map[string]any{"query": "ping", "max_tools": 0},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Empty(t, result.StructuredContent.(map[string]any)["suggested_tools"])

		result, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_context",
			Arguments: map[string]any{"query": "ping", "max_tools": 99},
		})
		require.NoError(t, err)
		require.True(t, result.IsError)

		result, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name: "get_context", Arguments: map[string]any{"query": ""},
		})
		require.NoError(t, err)
		require.True(t, result.IsError)
	})
}

func TestCallTool(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")
	fake := newFakeMCPServer(t)
	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "cloudflare", fake.URL, nil)
	httpServer := newContextTestServer(t, servers, nil)
	session, err := connectAs(t, ctx, httpServer.URL, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	call := func(name string, args map[string]any) *mcp.CallToolResult {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "call_tool", Arguments: map[string]any{"name": name, "arguments": args},
		})
		require.NoError(t, err)
		return res
	}

	t.Run("runs a connected app's tool", func(t *testing.T) {
		res := call("cloudflare:echo", map[string]any{"text": "hello"})
		require.False(t, res.IsError, "%+v", res)
		require.Equal(t, "hello", res.Content[0].(*mcp.TextContent).Text)
	})

	t.Run("keeps the downstream tool's error", func(t *testing.T) {
		res := call("cloudflare:echo", map[string]any{"text": ""})
		require.True(t, res.IsError)
		require.Equal(t, "nothing to echo", res.Content[0].(*mcp.TextContent).Text)
	})

	t.Run("rejects unnamespaced and unconnected tools", func(t *testing.T) {
		require.True(t, call("echo", nil).IsError)
		require.True(t, call("other:echo", nil).IsError)
	})
}

type fakeReranker struct {
	query string
	docs  []string
	order []int
	err   error
}

func (f *fakeReranker) Rerank(
	_ context.Context, query string, docs []string, _ int,
) ([]int, error) {
	f.query, f.docs = query, docs
	return f.order, f.err
}

func TestRerank(t *testing.T) {
	ctx := t.Context()
	text := func(s string) string { return s }

	t.Run("reorders by the reranker", func(t *testing.T) {
		fake := &fakeReranker{order: []int{2, 0, 1}}
		failed := false
		got := rerank(ctx, fake, "q", []string{"a", "b", "c"}, text, func() { failed = true })
		require.Equal(t, []string{"c", "a", "b"}, got)
		require.Equal(t, "q", fake.query)
		require.Equal(t, []string{"a", "b", "c"}, fake.docs)
		require.False(t, failed)
	})

	t.Run("without a reranker leaves the order", func(t *testing.T) {
		got := rerank(ctx, nil, "q", []string{"a", "b"}, text, func() {})
		require.Equal(t, []string{"a", "b"}, got)
	})

	t.Run("a failing reranker keeps the order and reports", func(t *testing.T) {
		failed := false
		got := rerank(ctx, &fakeReranker{err: errors.New("boom")}, "q",
			[]string{"a", "b"}, text, func() { failed = true })
		require.Equal(t, []string{"a", "b"}, got)
		require.True(t, failed)
	})

	t.Run("skips a single item", func(t *testing.T) {
		fake := &fakeReranker{}
		got := rerank(ctx, fake, "q", []string{"a"}, text, func() {})
		require.Equal(t, []string{"a"}, got)
		require.Nil(t, fake.docs)
	})
}

func TestGetContextReranksItems(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")
	servers := newFakeConnectedServers()
	federated := &fakeFederated{result: &mcpsearch.Result{Items: []mcpsearch.Item{
		{Title: "first"}, {Title: "second"},
	}}}
	httpServer := newContextTestServer(t, servers, nil,
		WithFederatedSearcher(federated), WithReranker(&fakeReranker{order: []int{1, 0}}))
	session, err := connectAs(t, ctx, httpServer.URL, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_context", Arguments: map[string]any{"query": "x", "max_tools": 0},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "%+v", result)
	out := result.StructuredContent.(map[string]any)
	items := out["items"].([]any)
	require.Equal(t, "second", items[0].(map[string]any)["title"])
	require.Equal(t, "first", items[1].(map[string]any)["title"])
}

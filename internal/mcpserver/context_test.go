package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/mcpsearch"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

type fakeFederated struct {
	caller syntax.DID
	org    syntax.DID
	query  mcpsearch.Query
	calls  int
	result *mcpsearch.Result
	err    error
}

func (f *fakeFederated) Search(
	_ context.Context, caller syntax.DID, org syntax.DID, _ []string, q mcpsearch.Query,
) (*mcpsearch.Result, error) {
	f.caller, f.org, f.query = caller, org, q
	f.calls++
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

// scriptedReranker orders documents by a score parsed from the title's first
// byte, highest digit first, so a test can say which sources' items win.
type scriptedReranker struct{}

func (scriptedReranker) Rerank(
	_ context.Context, _ string, docs []string, topN int,
) ([]int, error) {
	order := make([]int, len(docs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return docs[order[a]][0] > docs[order[b]][0] })
	if topN > 0 && topN < len(order) {
		order = order[:topN]
	}
	return order, nil
}

func TestGetContextPaginates(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")
	servers := newFakeConnectedServers()
	// Two sources: each one's own order is a-first, but the reranker puts the
	// highest digit first across both.
	var items []mcpsearch.Item
	for i := range 6 {
		items = append(items,
			mcpsearch.Item{Source: mcpsearch.ItemSource{ServerName: "a"}, Title: fmt.Sprintf("%d-a%d", i, i)},
			mcpsearch.Item{Source: mcpsearch.ItemSource{ServerName: "b"}, Title: fmt.Sprintf("%d-b%d", 9-i, i)},
		)
	}
	federated := &fakeFederated{result: &mcpsearch.Result{Items: items}}
	httpServer := newContextTestServer(t, servers, nil,
		WithFederatedSearcher(federated), WithReranker(scriptedReranker{}))
	session, err := connectAs(t, ctx, httpServer.URL, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	call := func(args map[string]any) (map[string]any, error) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_context", Arguments: args})
		if err != nil {
			return nil, err
		}
		require.False(t, res.IsError, "%+v", res)
		return res.StructuredContent.(map[string]any), nil
	}
	titles := func(out map[string]any) []string {
		var ts []string
		for _, it := range out["items"].([]any) {
			ts = append(ts, it.(map[string]any)["title"].(string))
		}
		return ts
	}

	first, err := call(map[string]any{"query": "x", "max_tools": 0})
	require.NoError(t, err)
	// The pool is ranked across both sources before the page is cut, so the
	// first page holds the best of both, not the first few of each.
	require.Equal(t, []string{"9-b0", "8-b1", "7-b2", "6-b3", "5-b4"}, titles(first))
	cursor, ok := first["next_cursor"].(string)
	require.True(t, ok)

	// Later pages continue the same ranking; the query isn't needed again.
	second, err := call(map[string]any{"cursor": cursor})
	require.NoError(t, err)
	require.Equal(t, []string{"5-a5", "4-a4", "4-b5", "3-a3", "2-a2"}, titles(second))
	require.Empty(t, second["suggested_tools"])
	cursor = second["next_cursor"].(string)

	third, err := call(map[string]any{"cursor": cursor})
	require.NoError(t, err)
	require.Equal(t, []string{"1-a1", "0-a0"}, titles(third))
	require.NotContains(t, third, "next_cursor", "the last page has no next_cursor")

	// The search isn't repeated for later pages.
	require.Equal(t, 1, federated.calls)

	t.Run("an invalid cursor is Invalid params", func(t *testing.T) {
		for _, bad := range []string{"", "not-a-cursor", "e30"} {
			_, err := call(map[string]any{"cursor": bad})
			var rpcErr *jsonrpc.Error
			require.ErrorAs(t, err, &rpcErr, "cursor %q", bad)
			require.EqualValues(t, jsonrpc.CodeInvalidParams, rpcErr.Code)
		}
	})

	t.Run("another caller can't use the cursor", func(t *testing.T) {
		other, err := connectAs(t, ctx, httpServer.URL, "did:plc:other")
		require.NoError(t, err)
		defer func() { _ = other.Close() }()
		_, err = other.CallTool(ctx, &mcp.CallToolParams{
			Name: "get_context", Arguments: map[string]any{"cursor": cursor},
		})
		require.Error(t, err)
	})
}

func TestContextPages(t *testing.T) {
	caller := syntax.DID("did:plc:caller")
	now := time.Now()
	p := newContextPages()
	p.now = func() time.Time { return now }

	id := p.put(&contextSnapshot{caller: caller})
	_, ok := p.get(id, caller)
	require.True(t, ok)
	_, ok = p.get(id, "did:plc:other")
	require.False(t, ok)
	_, ok = p.get("missing", caller)
	require.False(t, ok)

	t.Run("use extends life, disuse expires", func(t *testing.T) {
		now = now.Add(p.ttl - time.Second)
		_, ok := p.get(id, caller)
		require.True(t, ok)
		now = now.Add(p.ttl - time.Second)
		_, ok = p.get(id, caller)
		require.True(t, ok)
		now = now.Add(p.ttl)
		_, ok = p.get(id, caller)
		require.False(t, ok)
	})

	t.Run("expired snapshots are dropped on put and the store is bounded", func(t *testing.T) {
		p := newContextPages()
		p.now = func() time.Time { return now }
		for range contextPagesMax + 10 {
			p.put(&contextSnapshot{caller: caller})
		}
		require.Len(t, p.snaps, contextPagesMax)
		now = now.Add(2 * p.ttl)
		p.put(&contextSnapshot{caller: caller})
		require.Len(t, p.snaps, 1)
	})
}

func TestContextCursor(t *testing.T) {
	c := contextCursor{ID: "abc", Records: 8, Items: 5}
	got, ok := decodeContextCursor(c.encode())
	require.True(t, ok)
	require.Equal(t, c, got)
	for _, bad := range []string{"", "%%%", "e30", "eyJpZCI6ImEiLCJyIjotMX0"} {
		_, ok := decodeContextCursor(bad)
		require.False(t, ok, bad)
	}
}

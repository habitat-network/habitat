package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/fedsearch"
	kvtestutil "github.com/habitat-network/habitat/internal/keyval/testutil"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/reranker"
	"github.com/habitat-network/habitat/internal/search"
)

// fakeBackend is a fedsearch backend answering with results, or failing with
// err. It counts its searches.
type fakeBackend struct {
	id      fedsearch.BackendID
	results []fedsearch.Result
	err     error
	calls   int
}

func (b *fakeBackend) ID() fedsearch.BackendID { return b.id }
func (b *fakeBackend) Kind() fedsearch.Kind    { return "fake" }
func (b *fakeBackend) Name() string            { return string(b.id) }

func (b *fakeBackend) Search(context.Context, fedsearch.Query) ([]fedsearch.Result, error) {
	b.calls++
	return b.results, b.err
}

// fakeResolver lists its backends for any caller, remembering the last.
type fakeResolver struct {
	backends []*fakeBackend
	caller   syntax.DID
	org      syntax.DID
}

func (r *fakeResolver) Kind() fedsearch.Kind { return "fake" }

func (r *fakeResolver) Backends(
	_ context.Context, caller, org syntax.DID,
) ([]fedsearch.Backend, error) {
	r.caller, r.org = caller, org
	out := make([]fedsearch.Backend, len(r.backends))
	for i, b := range r.backends {
		out[i] = b
	}
	return out, nil
}

// digitReranker scores each document by the number its first character is,
// so a test can say which results win.
type digitReranker struct{}

func (digitReranker) Rerank(
	_ context.Context, _ string, docs []string, _ int,
) ([]reranker.Scored, error) {
	out := make([]reranker.Scored, len(docs))
	for i, doc := range docs {
		score, _ := strconv.Atoi(doc[:1])
		out[i] = reranker.Scored{Index: i, Score: float64(score)}
	}
	return out, nil
}

func newTestSearch(t *testing.T, backends ...*fakeBackend) (*fedsearch.Service, *fakeResolver) {
	t.Helper()
	resolver := &fakeResolver{backends: backends}
	svc := fedsearch.NewService(
		kvtestutil.NewEncrypted(t), []fedsearch.Resolver{resolver},
		fedsearch.WithReranker(digitReranker{}),
	)
	t.Cleanup(svc.Wait)
	return svc, resolver
}

func newContextTestServer(
	t *testing.T,
	servers mcpgateway.ConnectedServers,
	opts ...Option,
) *httptest.Server {
	t.Helper()
	spacesStore, permStore := setupStores(t)
	srv := New(
		fakeTokens{}, spacesStore, permStore, servers, nil, newTestDirectory(),
		"https://habitat.example", "https://habitat.example/mcp", opts...,
	)
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer
}

// callGetContext calls get_context with args and decodes its output.
func callGetContext(
	t *testing.T, session *mcp.ClientSession, args map[string]any,
) (getContextOutput, error) {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "get_context", Arguments: args,
	})
	if err != nil {
		return getContextOutput{}, err
	}
	require.False(t, res.IsError, "%+v", res)
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	var out getContextOutput
	require.NoError(t, json.Unmarshal(raw, &out))
	return out, nil
}

func resultTitles(out getContextOutput) []string {
	var titles []string
	for _, r := range out.Results {
		titles = append(titles, r.Title)
	}
	return titles
}

func TestGetContext(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")
	fake := newFakeMCPServer(t)
	servers := newFakeConnectedServers()
	servers.connect(caller, testOrg, "cloudflare", fake.URL, nil)

	t.Run("ranks every source's results together and suggests tools", func(t *testing.T) {
		recordData, err := json.Marshal(search.RecordData{
			Space: "ats://did:plc:org/com.example.type/k", Collection: "com.example.note",
			Value: map[string]any{"text": "hi"},
		})
		require.NoError(t, err)
		records := &fakeBackend{id: "records", results: []fedsearch.Result{{
			Kind: search.ResultKind, ID: "r1", Title: "8 record", URL: "ats://r1", Data: recordData,
		}}}
		apps := &fakeBackend{id: "mcp:cloudflare/docs", results: []fedsearch.Result{
			{Kind: "item", ID: "d1", Title: "9 doc", URL: "https://example.com/doc", Snippet: "hi"},
			{Kind: "item", ID: "d2", Title: "1 doc"},
		}}
		broken := &fakeBackend{id: "mcp:drive/files", err: errors.New("rate limited")}
		svc, resolver := newTestSearch(t, records, apps, broken)
		httpServer := newContextTestServer(t, servers, WithSearch(svc))
		session, err := connectAs(t, ctx, httpServer.URL, caller.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		out, err := callGetContext(t, session, map[string]any{"query": "ping the server"})
		require.NoError(t, err)
		require.Equal(t, caller, resolver.caller)
		require.Equal(t, testOrg, resolver.org)
		require.Equal(t, []string{"9 doc", "8 record", "1 doc"}, resultTitles(out))
		require.Equal(t, contextResult{
			Source: "records", Kind: "record", Title: "8 record", URL: "ats://r1",
			Space: "ats://did:plc:org/com.example.type/k", Value: map[string]any{"text": "hi"},
		}, out.Results[1])
		require.Equal(t, "mcp:cloudflare/docs", out.Results[0].Source)
		require.Equal(t, []contextBackend{
			{Name: "mcp:drive/files", Status: "error", Error: "rate limited"},
		}, out.Backends)
		require.Nil(t, out.NextCursor)
		require.NotEmpty(t, out.SuggestedTools)
		require.Equal(t, "cloudflare:ping", out.SuggestedTools[0].Name)
	})

	t.Run("without search it only suggests tools", func(t *testing.T) {
		httpServer := newContextTestServer(t, servers)
		session, err := connectAs(t, ctx, httpServer.URL, caller.String())
		require.NoError(t, err)
		defer func() { _ = session.Close() }()

		out, err := callGetContext(t, session, map[string]any{"query": "ping"})
		require.NoError(t, err)
		require.Empty(t, out.Results)
		require.NotEmpty(t, out.SuggestedTools)
	})

	t.Run("max_tools 0 suggests nothing and bad values fail", func(t *testing.T) {
		httpServer := newContextTestServer(t, servers)
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
	httpServer := newContextTestServer(t, servers)
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

func TestGetContextPaginates(t *testing.T) {
	ctx := t.Context()
	caller := syntax.DID("did:plc:caller")
	// Two sources: each one's own order is lowest first, but the reranker
	// puts the highest digit first across both.
	a := &fakeBackend{id: "a"}
	b := &fakeBackend{id: "b"}
	for i := range 6 {
		a.results = append(a.results, fedsearch.Result{
			Kind: "item", ID: fmt.Sprint(i), Title: fmt.Sprintf("%d-a", i),
		})
		b.results = append(b.results, fedsearch.Result{
			Kind: "item", ID: fmt.Sprint(i), Title: fmt.Sprintf("%d-b", i+4),
		})
	}
	svc, _ := newTestSearch(t, a, b)
	httpServer := newContextTestServer(t, newFakeConnectedServers(), WithSearch(svc))
	session, err := connectAs(t, ctx, httpServer.URL, caller.String())
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	first, err := callGetContext(t, session, map[string]any{"query": "x", "max_tools": 0})
	require.NoError(t, err)
	require.Len(t, first.Results, contextPageSize)
	require.Equal(t, "9-b", first.Results[0].Title)
	require.NotNil(t, first.NextCursor)

	// The next page continues the same ranking; the query isn't needed again
	// and the search isn't repeated.
	second, err := callGetContext(t, session, map[string]any{"cursor": *first.NextCursor})
	require.NoError(t, err)
	require.Equal(t, []string{"1-a", "0-a"}, resultTitles(second))
	require.Empty(t, second.SuggestedTools)
	require.Nil(t, second.NextCursor, "the last page has no next_cursor")
	require.Equal(t, 1, a.calls)

	t.Run("an invalid cursor is Invalid params", func(t *testing.T) {
		for _, bad := range []string{"", "not-a-cursor", "e30"} {
			_, err := callGetContext(t, session, map[string]any{"cursor": bad})
			var rpcErr *jsonrpc.Error
			require.ErrorAs(t, err, &rpcErr, "cursor %q", bad)
			require.EqualValues(t, jsonrpc.CodeInvalidParams, rpcErr.Code)
		}
	})

	t.Run("another caller can't use the cursor", func(t *testing.T) {
		other, err := connectAs(t, ctx, httpServer.URL, "did:plc:other")
		require.NoError(t, err)
		defer func() { _ = other.Close() }()
		_, err = callGetContext(t, other, map[string]any{"cursor": *first.NextCursor})
		var rpcErr *jsonrpc.Error
		require.ErrorAs(t, err, &rpcErr)
		require.EqualValues(t, jsonrpc.CodeInvalidParams, rpcErr.Code)
	})
}

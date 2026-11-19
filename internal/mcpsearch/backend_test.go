package mcpsearch

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/fedsearch"
	"github.com/habitat-network/habitat/internal/mcpgateway"
)

func TestResolverBackendsSearchEachTool(t *testing.T) {
	ctx := t.Context()
	servers := newFakeConnectedServers()
	urlA := newFakeSearchServer(t, "a", nil)
	urlB := newFakeSearchServer(t, "b", nil)
	servers.connect("alpha", urlA)
	servers.connect("alpha-copy", urlA)
	servers.connect("beta", urlB)
	servers.connect("unknown", newFakeSearchServer(t, "c", nil))
	r := NewResolver(servers, NewRegistry(
		testProvider("a-search", urlA, "search"),
		testProvider("a-fail", urlA, "fail"),
		testProvider("b-structured", urlB, "structured"),
	))

	backends, err := r.Backends(ctx, testCaller, testOrg)
	require.NoError(t, err)
	var ids []fedsearch.BackendID
	for _, b := range backends {
		require.Equal(t, BackendKind, b.Kind())
		ids = append(ids, b.ID())
	}
	require.Equal(t, []fedsearch.BackendID{
		"mcp:alpha/a-search", "mcp:alpha/a-fail", "mcp:beta/b-structured",
	}, ids, "one per known tool; a duplicate URL and a server with no known tool have none")
	require.Equal(t, "alpha search", backends[0].Name())

	results, err := backends[0].Search(ctx, fedsearch.Query{Text: "q", Limit: 2})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, fedsearch.Result{
		Kind: ResultKind, ID: "https://a.example/0", Title: "a-q-0",
		URL: "https://a.example/0", Snippet: "about q",
		Data: json.RawMessage(`{"server":"alpha","provider":"a-search"}`),
	}, results[0])

	_, err = backends[1].Search(ctx, fedsearch.Query{Text: "q", Limit: 2})
	require.ErrorIs(t, err, errToolFailed)

	results, err = backends[2].Search(ctx, fedsearch.Query{Text: "q", Limit: 2})
	require.NoError(t, err)
	require.Equal(t, []string{"b-structured-q"}, []string{results[0].Title})
}

func TestResolverBackendNotConnected(t *testing.T) {
	ctx := t.Context()
	servers := newFakeConnectedServers()
	url := newFakeSearchServer(t, "a", nil)
	servers.connect("alpha", url)
	servers.failures["alpha"] = mcpgateway.ErrNotConnected
	r := NewResolver(servers, NewRegistry(testProvider("a-search", url, "search")))

	backends, err := r.Backends(ctx, testCaller, testOrg)
	require.NoError(t, err)
	require.Len(t, backends, 1)
	_, err = backends[0].Search(ctx, fedsearch.Query{Text: "q", Limit: 2})
	require.ErrorIs(t, err, fedsearch.ErrNotConnected)

	servers.listErr = errors.New("db down")
	_, err = r.Backends(ctx, testCaller, testOrg)
	require.Error(t, err)
}

func TestResolverBackendTruncatesLongSnippets(t *testing.T) {
	url := newFakeSearchServer(t, "alpha", nil)
	servers := newFakeConnectedServers()
	servers.connect("alpha", url)
	backends, err := NewResolver(servers, NewRegistry(testProvider("alpha", url, "search"))).
		Backends(t.Context(), testCaller, testOrg)
	require.NoError(t, err)
	long := strings.Repeat("é", maxSnippetRunes+10)
	// The fake server's snippet is "about <query>".
	results, err := backends[0].Search(t.Context(), fedsearch.Query{Text: long, Limit: 1})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, maxSnippetRunes, len([]rune(results[0].Snippet)))
	require.True(t, strings.HasSuffix(results[0].Snippet, "…"))
}

package toolsearch_test

import (
	"encoding/json"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/search/searchtest"
	"github.com/habitat-network/habitat/internal/toolsearch"
)

const (
	org   = syntax.DID("did:plc:org")
	alice = syntax.DID("did:plc:alice")
	bob   = syntax.DID("did:plc:bob")
)

func openIndex(t *testing.T, opts ...toolsearch.Option) *toolsearch.Index {
	t.Helper()
	idx, err := toolsearch.Open(t.Context(), searchtest.URL(t), "", searchtest.NewUID(t), opts...)
	require.NoError(t, err)
	return idx
}

func tool(server, name, description string) toolsearch.Tool {
	def, _ := json.Marshal(map[string]any{
		"name": server + ":" + name, "description": description,
		"inputSchema": map[string]any{"type": "object"},
	})
	return toolsearch.Tool{Server: server, Name: name, Description: description, Definition: def}
}

func names(hits []toolsearch.Hit) []string {
	out := []string{}
	for _, h := range hits {
		out = append(out, h.Server+":"+h.Name)
	}
	return out
}

func TestSearchFindsToolsByNameAndDescription(t *testing.T) {
	idx := openIndex(t)
	require.NoError(t, idx.ReplaceServer(t.Context(), org, alice, "github", []toolsearch.Tool{
		tool("github", "create_issue", "Open a new issue in a repository"),
		tool("github", "list_pulls", "List pull requests"),
	}))
	require.NoError(t, idx.ReplaceServer(t.Context(), org, alice, "slack", []toolsearch.Tool{
		tool("slack", "post_message", "Post a message to a channel"),
	}))

	hits, err := idx.Search(t.Context(), org, alice, "issue", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"github:create_issue"}, names(hits))
	require.Equal(t, "Open a new issue in a repository", hits[0].Description)

	hits, err = idx.Search(t.Context(), org, alice, "post message", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"slack:post_message"}, names(hits))

	hits, err = idx.Search(t.Context(), org, alice, "  ", 0)
	require.NoError(t, err)
	require.Empty(t, hits)
}

func TestSearchIsScopedToMemberAndOrg(t *testing.T) {
	idx := openIndex(t)
	require.NoError(t, idx.ReplaceServer(t.Context(), org, alice, "github", []toolsearch.Tool{
		tool("github", "create_issue", "Open an issue"),
	}))
	hits, err := idx.Search(t.Context(), org, bob, "issue", 0)
	require.NoError(t, err)
	require.Empty(t, hits, "another member's tools are not searched")
	hits, err = idx.Search(t.Context(), "did:plc:other", alice, "issue", 0)
	require.NoError(t, err)
	require.Empty(t, hits, "another org's tools are not searched")

	_, err = idx.Get(t.Context(), org, bob, "github", "create_issue")
	require.ErrorIs(t, err, toolsearch.ErrNotFound)
}

func TestSearchLimit(t *testing.T) {
	idx := openIndex(t)
	var tools []toolsearch.Tool
	for _, n := range []string{"a", "b", "c"} {
		tools = append(tools, tool("s", "tool_"+n, "does a thing"))
	}
	require.NoError(t, idx.ReplaceServer(t.Context(), org, alice, "s", tools))
	hits, err := idx.Search(t.Context(), org, alice, "thing", 2)
	require.NoError(t, err)
	require.Len(t, hits, 2)
}

func TestGetReturnsDefinition(t *testing.T) {
	idx := openIndex(t)
	want := tool("github", "create_issue", "Open an issue")
	require.NoError(
		t,
		idx.ReplaceServer(t.Context(), org, alice, "github", []toolsearch.Tool{want}),
	)

	got, err := idx.Get(t.Context(), org, alice, "github", "create_issue")
	require.NoError(t, err)
	require.Equal(t, "github", got.Server)
	require.JSONEq(t, string(want.Definition), string(got.Definition))

	_, err = idx.Get(t.Context(), org, alice, "github", "missing")
	require.ErrorIs(t, err, toolsearch.ErrNotFound)
}

func TestReplaceServerDropsToolsItNoLongerHas(t *testing.T) {
	idx := openIndex(t)
	ctx := t.Context()
	require.NoError(t, idx.ReplaceServer(ctx, org, alice, "github", []toolsearch.Tool{
		tool("github", "old_tool", "gone soon"),
		tool("github", "kept_tool", "stays"),
	}))
	require.NoError(t, idx.ReplaceServer(ctx, org, alice, "slack", []toolsearch.Tool{
		tool("slack", "other_tool", "unrelated server"),
	}))

	require.NoError(t, idx.ReplaceServer(ctx, org, alice, "github", []toolsearch.Tool{
		tool("github", "kept_tool", "stays, updated"),
	}))
	_, err := idx.Get(ctx, org, alice, "github", "old_tool")
	require.ErrorIs(t, err, toolsearch.ErrNotFound)
	kept, err := idx.Get(ctx, org, alice, "github", "kept_tool")
	require.NoError(t, err)
	require.Equal(t, "stays, updated", kept.Description)
	_, err = idx.Get(ctx, org, alice, "slack", "other_tool")
	require.NoError(t, err, "other servers are untouched")

	// An empty list removes the server's tools.
	require.NoError(t, idx.ReplaceServer(ctx, org, alice, "github", nil))
	_, err = idx.Get(ctx, org, alice, "github", "kept_tool")
	require.ErrorIs(t, err, toolsearch.ErrNotFound)
}

func TestPruneServers(t *testing.T) {
	idx := openIndex(t)
	ctx := t.Context()
	require.NoError(t, idx.ReplaceServer(ctx, org, alice, "github", []toolsearch.Tool{
		tool("github", "a", "x"),
	}))
	require.NoError(t, idx.ReplaceServer(ctx, org, alice, "slack", []toolsearch.Tool{
		tool("slack", "b", "x"),
	}))
	require.NoError(t, idx.ReplaceServer(ctx, org, bob, "github", []toolsearch.Tool{
		tool("github", "a", "x"),
	}))

	require.NoError(t, idx.PruneServers(ctx, org, alice, []string{"github"}))
	_, err := idx.Get(ctx, org, alice, "slack", "b")
	require.ErrorIs(t, err, toolsearch.ErrNotFound)
	_, err = idx.Get(ctx, org, alice, "github", "a")
	require.NoError(t, err)
	_, err = idx.Get(ctx, org, bob, "github", "a")
	require.NoError(t, err, "other members are untouched")

	require.NoError(t, idx.PruneServers(ctx, org, alice, nil))
	_, err = idx.Get(ctx, org, alice, "github", "a")
	require.ErrorIs(t, err, toolsearch.ErrNotFound)
}

func TestServerNamesAreQuotedInFilters(t *testing.T) {
	idx := openIndex(t)
	ctx := t.Context()
	require.NoError(t, idx.ReplaceServer(ctx, org, alice, `we"ird`, []toolsearch.Tool{
		tool(`we"ird`, "t", "x"),
	}))
	require.NoError(t, idx.PruneServers(ctx, org, alice, []string{`we"ird`}))
	_, err := idx.Get(ctx, org, alice, `we"ird`, "t")
	require.NoError(t, err)
}

func TestHybridSearchFindsSynonyms(t *testing.T) {
	keyword := openIndex(t)
	hybrid := openIndex(t, toolsearch.WithEmbedder(searchtest.Embedder(t, 1, 0)))
	for _, idx := range []*toolsearch.Index{keyword, hybrid} {
		require.NoError(t, idx.ReplaceServer(t.Context(), org, alice, "garage", []toolsearch.Tool{
			tool("garage", "rent", "rent a car"),
			tool("garage", "bake", "bake bread"),
		}))
	}
	hits, err := keyword.Search(t.Context(), org, alice, "automobile", 0)
	require.NoError(t, err)
	require.Empty(t, hits, "keyword search doesn't know synonyms")

	hits, err = hybrid.Search(t.Context(), org, alice, "automobile", 1)
	require.NoError(t, err)
	require.Equal(t, []string{"garage:rent"}, names(hits))
}

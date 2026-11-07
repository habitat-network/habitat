package mcpsearch

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/mcpgateway"
)

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func TestToolProviderNormalize(t *testing.T) {
	p := &ToolProvider{
		ItemsPath:    "data.hits",
		TitlePaths:   []string{"title", "name"},
		URLPaths:     []string{"link.href"},
		SnippetPaths: []string{"summary"},
	}
	for name, tc := range map[string]struct {
		result *mcp.CallToolResult
		want   []Item
	}{
		"json text, with fallback paths and skipping empty entries": {
			result: textResult(`{"data":{"hits":[
				{"title":"A","link":{"href":"https://a"},"summary":"sa"},
				{"title":"","name":"B"},
				{"other":"ignored"},
				"not an object"
			]}}`),
			want: []Item{{Title: "A", URL: "https://a", Snippet: "sa"}, {Title: "B"}},
		},
		"structured content wins over text": {
			result: &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "a summary"}},
				StructuredContent: map[string]any{"data": map[string]any{
					"hits": []any{map[string]any{"title": "S"}},
				}},
			},
			want: []Item{{Title: "S"}},
		},
		"missing list is no matches": {
			result: textResult(`{"data":{}}`),
			want:   nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			items, err := p.Normalize(tc.result)
			require.NoError(t, err)
			require.Equal(t, tc.want, items)
		})
	}

	_, err := p.Normalize(textResult("Found 3 messages"))
	require.ErrorIs(t, err, ErrUnparseableResult)

	p.TextFallback = true
	items, err := p.Normalize(textResult("Found 3 messages"))
	require.NoError(t, err)
	require.Equal(t, []Item{{Snippet: "Found 3 messages"}}, items)
	items, err = p.Normalize(textResult("  "))
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestCanonicalURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://Example.COM/mcp/":           "https://example.com/mcp",
		"https://example.com:443/mcp":        "https://example.com/mcp",
		"http://example.com:80/mcp#frag":     "http://example.com/mcp",
		"http://example.com:8080/mcp?x=1":    "http://example.com:8080/mcp?x=1",
		"https://api.githubcopilot.com/mcp/": "https://api.githubcopilot.com/mcp",
	} {
		got, err := CanonicalURL(raw)
		require.NoError(t, err, raw)
		require.Equal(t, want, got, raw)
	}
	trimmed, err := CanonicalURL("  https://example.com/mcp\n")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/mcp", trimmed, "surrounding whitespace is ignored")
	for _, raw := range []string{"", "/mcp", "ftp://example.com", "https://"} {
		_, err := CanonicalURL(raw)
		require.Error(t, err, raw)
	}
}

func TestRegistryForURL(t *testing.T) {
	a := &ToolProvider{Name: "a", URL: "https://x.example/mcp"}
	b := &ToolProvider{Name: "b", URL: "https://X.example/mcp/"}
	c := &ToolProvider{Name: "c", URL: "https://y.example/mcp"}
	r := NewRegistry(a, b, c)
	require.Equal(t, []Provider{a, b}, r.ForURL("https://x.example/mcp"))
	require.Empty(t, r.ForURL("https://z.example/mcp"))
	require.Empty(t, r.ForURL("not a url"))

	require.Panics(t, func() { NewRegistry(a, &ToolProvider{Name: "a"}) })
}

func TestDefaultProvidersCoverPresets(t *testing.T) {
	r := NewRegistry(DefaultProviders()...)
	ids := func(url string) []ProviderID {
		var out []ProviderID
		for _, p := range r.ForURL(url) {
			out = append(out, p.ID())
		}
		return out
	}
	require.Equal(t, []ProviderID{"google-drive-files"},
		ids(mcpgateway.PresetURL(mcpgateway.PresetGoogleDrive)))
	require.Equal(t, []ProviderID{"slack-messages"},
		ids(mcpgateway.PresetURL(mcpgateway.PresetSlack)))
	require.Equal(t, []ProviderID{"github-issues", "github-pull-requests", "github-code"},
		ids(mcpgateway.PresetURL(mcpgateway.PresetGitHub)))
}

// providerByID returns the default provider id.
func providerByID(t *testing.T, id ProviderID) Provider {
	t.Helper()
	for _, p := range DefaultProviders() {
		if p.ID() == id {
			return p
		}
	}
	t.Fatalf("no provider %q", id)
	return nil
}

func TestGoogleDriveProvider(t *testing.T) {
	p := providerByID(t, "google-drive-files")
	require.Equal(t, "search_files", p.ToolName())
	require.Equal(t, map[string]any{
		"query":    `fullText contains 'Q3 plan \'draft\' C:\\docs'`,
		"pageSize": 5,
	}, p.Arguments(Query{Text: `Q3 plan 'draft' C:\docs`, Limit: 5}))

	items, err := p.Normalize(textResult(`{"files":[{
		"id":"1","title":"Q3 plan","viewUrl":"https://docs.google.com/d/1",
		"contentSnippet":"the plan","mimeType":"application/vnd.google-apps.document"
	},{"id":"2","title":"Notes","description":"no snippet"}],"nextPageToken":"t"}`))
	require.NoError(t, err)
	require.Equal(t, []Item{
		{Title: "Q3 plan", URL: "https://docs.google.com/d/1", Snippet: "the plan"},
		{Title: "Notes", Snippet: "no snippet"},
	}, items)
}

func TestGitHubProviders(t *testing.T) {
	issues := providerByID(t, "github-issues")
	require.Equal(t, "search_issues", issues.ToolName())
	require.Equal(t, map[string]any{"query": "login bug", "perPage": 3},
		issues.Arguments(Query{Text: "login bug", Limit: 3}))
	items, err := issues.Normalize(textResult(`{"total_count":1,"incomplete_results":false,
		"items":[{"number":7,"title":"Login fails","html_url":"https://github.com/o/r/issues/7",
		"body":"Steps to reproduce"}]}`))
	require.NoError(t, err)
	require.Equal(t, []Item{{
		Title: "Login fails", URL: "https://github.com/o/r/issues/7", Snippet: "Steps to reproduce",
	}}, items)

	code := providerByID(t, "github-code")
	require.Equal(t, "search_code", code.ToolName())
	items, err = code.Normalize(textResult(`{"total_count":1,"items":[{"name":"auth.go",
		"path":"internal/auth.go","html_url":"https://github.com/o/r/blob/main/internal/auth.go",
		"repository":{"full_name":"o/r"}}]}`))
	require.NoError(t, err)
	require.Equal(t, []Item{{
		Title: "internal/auth.go", URL: "https://github.com/o/r/blob/main/internal/auth.go",
		Snippet: "o/r",
	}}, items)
}

func TestSlackProvider(t *testing.T) {
	p := providerByID(t, "slack-messages")
	require.Equal(t, "slack_search_public_and_private", p.ToolName())
	require.Equal(
		t,
		map[string]any{"query": "launch"},
		p.Arguments(Query{Text: "launch", Limit: 5}),
	)
	items, err := p.Normalize(textResult("# Results\n- #general: launch is Tuesday"))
	require.NoError(t, err)
	require.Equal(t, []Item{{Snippet: "# Results\n- #general: launch is Tuesday"}}, items)
}

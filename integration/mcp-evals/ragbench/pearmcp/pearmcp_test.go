package pearmcp

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/corpus"
	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/internal/benchtest"
	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/mockmcp"
)

// setupStack serves the test dataset through the mocks and pear, and returns
// a session to pear's MCP endpoint.
func setupStack(t *testing.T, opts Options) (*Stack, *corpus.Slice, *mcp.ClientSession) {
	t.Helper()
	dataset, err := corpus.Open(benchtest.Redwood(t))
	require.NoError(t, err)
	slice, err := dataset.Slice(corpus.SliceOptions{
		Sources: []corpus.Source{
			corpus.SourceSlack,
			corpus.SourceGoogleDrive,
			corpus.SourceGitHub,
		},
		Distractors: 10,
	})
	require.NoError(t, err)
	stack, err := Start(slice, opts)
	require.NoError(t, err)
	t.Cleanup(stack.Close)
	session, err := stack.Connect(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return stack, slice, session
}

type contextOutput struct {
	Items []struct {
		Server, Title, URL, Snippet string
	}
	SuggestedTools []struct {
		Name string
	} `json:"suggested_tools"`
	Notes []string
}

func getContext(t *testing.T, session *mcp.ClientSession, query string) contextOutput {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "get_context", Arguments: map[string]any{"query": query},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	var out contextOutput
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestStackGetContextSearchesEveryMockThroughPear(t *testing.T) {
	stack, slice, session := setupStack(t, Options{})

	out := getContext(t, session, "pull requests about multipart upload limits")

	// pear's own providers recognized the mocks and read their answers: the
	// pull request comes back as an item with its URL.
	require.Empty(t, out.Notes)
	var urls []string
	for _, item := range out.Items {
		urls = append(urls, item.URL)
	}
	require.Contains(t, urls, slice.Doc("dsid_b1").URL)
	// Every search tool pear knows was called on the mocks, with the query
	// in each tool's own syntax.
	queries := make(map[string]any)
	for _, call := range stack.Log.Take() {
		queries[string(call.App)+":"+call.Tool] = call.Args["query"]
	}
	require.Equal(t, map[string]any{
		"google-drive:search_files":             "fullText contains 'pull requests about multipart upload limits'",
		"slack:slack_search_public_and_private": "pull requests about multipart upload limits",
		"github:search_issues":                  "pull requests about multipart upload limits",
		"github:search_pull_requests":           "pull requests about multipart upload limits",
		"github:search_code":                    "pull requests about multipart upload limits",
	}, queries)
	// Suggestions are drawn from the mocks' tools, named for call_tool.
	require.NotEmpty(t, out.SuggestedTools)
	for _, tool := range out.SuggestedTools {
		require.Regexp(t, `^(google-drive|slack|github):`, tool.Name)
	}
}

func TestStackGetContextReadsSlackResultsFromText(t *testing.T) {
	_, slice, session := setupStack(t, Options{})

	out := getContext(t, session, "kestrel cache outage")

	// Slack answers in text, so its results are one item, with the threads'
	// permalinks in it.
	var found []corpus.DocID
	for _, item := range out.Items {
		if item.Server == "slack" {
			found = slice.Mentioned(item.Snippet)
		}
	}
	require.Contains(t, found, corpus.DocID("dsid_a1"))
}

func TestStackCallToolReadsADocumentFromAMock(t *testing.T) {
	stack, _, session := setupStack(t, Options{})

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "call_tool",
		Arguments: map[string]any{
			"name": "github:pull_request_read",
			"arguments": map[string]any{
				"method":     "get",
				"owner":      "redwood-inference",
				"repo":       "redwood",
				"pullNumber": 101,
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
	raw, err := json.Marshal(res.Content)
	require.NoError(t, err)
	require.Contains(t, string(raw), "max_file_size")
	require.Equal(t, []mockmcp.Call{{
		App: mockmcp.AppGitHub, Tool: "pull_request_read",
		Args: map[string]any{
			"method":     "get",
			"owner":      "redwood-inference",
			"repo":       "redwood",
			"pullNumber": float64(101),
		},
		Docs: []corpus.DocID{"dsid_b1"},
	}}, stack.Log.Take())
}

func TestStackOnlyConnectsTheAppsAskedFor(t *testing.T) {
	stack, _, session := setupStack(t, Options{Apps: []mockmcp.App{mockmcp.AppSlack}})

	getContext(t, session, "kestrel cache outage")

	require.Len(t, stack.MockEndpoints, 1)
	for _, call := range stack.Log.Take() {
		require.Equal(t, mockmcp.AppSlack, call.App)
	}
}

func TestStackRejectsRequestsWithoutTheToken(t *testing.T) {
	stack, _, _ := setupStack(t, Options{})

	client := mcp.NewClient(&mcp.Implementation{Name: "anonymous", Version: "0.1.0"}, nil)
	_, err := client.Connect(
		t.Context(),
		&mcp.StreamableClientTransport{Endpoint: stack.Endpoint},
		nil,
	)
	require.Error(t, err)
}

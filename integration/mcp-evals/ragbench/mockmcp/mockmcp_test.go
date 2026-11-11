package mockmcp

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/corpus"
	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/internal/benchtest"
)

// setupMock serves app's mock over the test dataset and returns a session
// to it and the log of its calls.
func setupMock(t *testing.T, app App, opts Options) (*mcp.ClientSession, *Log, *corpus.Slice) {
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
	log := &Log{}
	server := httptest.NewServer(New(app, slice, log, opts).Handler())
	t.Cleanup(server.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1.0"}, nil)
	session, err := client.Connect(
		t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session, log, slice
}

// call calls tool and returns its result and the text of its content.
func call(t *testing.T, session *mcp.ClientSession, tool string, args map[string]any) (
	result *mcp.CallToolResult, text string,
) {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	require.NoError(t, err)
	raw, err := json.Marshal(res.Content)
	require.NoError(t, err)
	var blocks []struct{ Text string }
	require.NoError(t, json.Unmarshal(raw, &blocks))
	for _, block := range blocks {
		text += block.Text
	}
	return res, text
}

func TestDriveSearchAnswersInDrivesShape(t *testing.T) {
	session, log, slice := setupMock(t, AppGoogleDrive, Options{})

	// The query is in Drive's syntax, as pear sends it.
	res, _ := call(t, session, "search_files", map[string]any{
		"query": `fullText contains 'runway model\'s hiring'`, "pageSize": 2,
	})

	require.False(t, res.IsError)
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	var out driveSearchOutput
	require.NoError(t, json.Unmarshal(raw, &out))
	require.NotEmpty(t, out.Files)
	require.LessOrEqual(t, len(out.Files), 2)
	model := slice.Doc("dsid_c1")
	require.Equal(t, "Headcount ramp model", out.Files[0].Title)
	require.Equal(t, model.URL, out.Files[0].ViewURL)
	require.Contains(t, out.Files[0].ContentSnippet, "runway sensitivity")
	calls := log.Take()
	require.Len(t, calls, 1)
	require.Equal(t, corpus.DocID("dsid_c1"), calls[0].Docs[0])

	// The file can be read in full by the id search gave.
	res, body := call(t, session, "read_file_content", map[string]any{"fileId": out.Files[0].ID})
	require.False(t, res.IsError)
	require.Contains(t, body, "four engineers a quarter")
	res, _ = call(t, session, "read_file_content", map[string]any{"fileId": "nope"})
	require.True(t, res.IsError)
}

func TestSlackSearchAnswersInTextWithPermalinks(t *testing.T) {
	session, _, slice := setupMock(t, AppSlack, Options{})

	res, body := call(t, session, "slack_search_public_and_private",
		map[string]any{"query": "kestrel cache outage"})

	require.False(t, res.IsError)
	require.Nil(t, res.StructuredContent)
	require.Contains(t, body, "Channel: #incidents")
	require.Equal(t, corpus.DocID("dsid_a1"), slice.Mentioned(body)[0])

	res, body = call(t, session, "slack_read_thread",
		map[string]any{"channel_id": "#incidents", "message_ts": "1700000001"})
	require.False(t, res.IsError)
	require.Contains(t, body, "expired certificate")
}

func TestGitHubSearchAppliesQualifiers(t *testing.T) {
	session, log, _ := setupMock(t, AppGitHub, Options{})

	search := func(tool, query string) githubSearchOutput {
		res, text := call(t, session, tool, map[string]any{"query": query, "perPage": 5})
		require.False(t, res.IsError)
		var out githubSearchOutput
		require.NoError(t, json.Unmarshal([]byte(text), &out))
		return out
	}

	out := search("search_pull_requests", "is:pr multipart upload")
	require.Equal(t, "Add multipart upload limits", out.Items[0].Title)
	require.Equal(t, 101, out.Items[0].Number)
	require.Equal(t, "https://github.com/redwood-inference/redwood/pull/101", out.Items[0].HTMLURL)

	// repo: limits results to that repository.
	out = search("search_pull_requests", "repo:redwood-inference/installer image upload")
	require.Len(t, out.Items, 1)
	require.Equal(t, "Bump base image", out.Items[0].Title)

	// The dataset has no issues or code, so those searches find nothing,
	// but they're still recorded.
	require.Empty(t, search("search_issues", "multipart upload").Items)
	require.Empty(t, search("search_code", "multipart upload").Items)
	require.Len(t, log.Take(), 4)
}

func TestMatchAllRequiresEveryWord(t *testing.T) {
	session, _, _ := setupMock(t, AppSlack, Options{Match: corpus.MatchAll})

	_, body := call(t, session, "slack_search_public_and_private",
		map[string]any{"query": "What caused the kestrel cache outage last Tuesday?"})
	require.Contains(t, body, "Found 0 messages")

	_, body = call(t, session, "slack_search_public_and_private",
		map[string]any{"query": "kestrel outage"})
	require.Contains(t, body, "Found 1 messages")
}

func TestOtherToolsAreListedButUnavailable(t *testing.T) {
	session, log, _ := setupMock(t, AppGitHub, Options{})

	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	names := make([]string, len(tools.Tools))
	for i, tool := range tools.Tools {
		names[i] = tool.Name
		require.NotEmpty(t, tool.Description, tool.Name)
	}
	require.Contains(t, names, "search_pull_requests")
	require.Contains(t, names, "merge_pull_request")

	res, body := call(t, session, "merge_pull_request", map[string]any{"pullNumber": 1})
	require.True(t, res.IsError)
	require.Contains(t, body, "isn't available")
	require.Equal(t, "merge_pull_request", log.Take()[0].Tool)
}

func TestAppsMapToSourcesAndSearchTools(t *testing.T) {
	for _, app := range Apps {
		require.Equal(t, app, AppOf(app.Source()))
		require.NotEmpty(t, app.SearchTool())
	}
	require.Empty(t, AppOf("jira"))
}

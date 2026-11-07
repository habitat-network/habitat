package mcpsearch

import (
	"strings"

	"github.com/habitat-network/habitat/internal/mcpgateway"
)

// DefaultProviders are the search tools pear knows how to call, for the
// connectors in mcpgateway's presets. To support another search tool, add a
// ToolProvider here (or a custom Provider for a result ToolProvider can't
// read) and a test of its Normalize against a sample of the tool's output.
func DefaultProviders() []Provider {
	return []Provider{
		// https://developers.google.com/workspace/drive/api/reference/mcp/tools_list/search_files
		&ToolProvider{
			Name: "google-drive-files",
			URL:  mcpgateway.PresetURL(mcpgateway.PresetGoogleDrive),
			Tool: "search_files",
			Args: func(q Query) map[string]any {
				return map[string]any{
					"query":    "fullText contains " + driveQuote(q.Text),
					"pageSize": q.Limit,
				}
			},
			ItemsPath:    "files",
			TitlePaths:   []string{"title"},
			URLPaths:     []string{"viewUrl"},
			SnippetPaths: []string{"contentSnippet", "description"},
		},
		// https://docs.slack.dev/ai/slack-mcp-server/. Slack answers in
		// formatted text rather than JSON, so the whole answer is one item.
		&ToolProvider{
			Name: "slack-messages",
			URL:  mcpgateway.PresetURL(mcpgateway.PresetSlack),
			Tool: "slack_search_public_and_private",
			Args: func(q Query) map[string]any {
				return map[string]any{"query": q.Text}
			},
			ItemsPath:    "messages",
			TitlePaths:   []string{"channel_name", "channel"},
			URLPaths:     []string{"permalink"},
			SnippetPaths: []string{"text"},
			TextFallback: true,
		},
		// https://github.com/github/github-mcp-server. Its search tools return
		// GitHub's search API response.
		&ToolProvider{
			Name:         "github-issues",
			URL:          mcpgateway.PresetURL(mcpgateway.PresetGitHub),
			Tool:         "search_issues",
			Args:         githubSearchArgs,
			ItemsPath:    "items",
			TitlePaths:   []string{"title"},
			URLPaths:     []string{"html_url"},
			SnippetPaths: []string{"body"},
		},
		&ToolProvider{
			Name:         "github-pull-requests",
			URL:          mcpgateway.PresetURL(mcpgateway.PresetGitHub),
			Tool:         "search_pull_requests",
			Args:         githubSearchArgs,
			ItemsPath:    "items",
			TitlePaths:   []string{"title"},
			URLPaths:     []string{"html_url"},
			SnippetPaths: []string{"body"},
		},
		&ToolProvider{
			Name:       "github-code",
			URL:        mcpgateway.PresetURL(mcpgateway.PresetGitHub),
			Tool:       "search_code",
			Args:       githubSearchArgs,
			ItemsPath:  "items",
			TitlePaths: []string{"path", "name"},
			URLPaths:   []string{"html_url"},
			// Code results carry no text unless text matches are requested,
			// so the repository is the best context there is.
			SnippetPaths: []string{"repository.full_name"},
		},
	}
}

func githubSearchArgs(q Query) map[string]any {
	return map[string]any{"query": q.Text, "perPage": q.Limit}
}

// driveQuote quotes s as a string value in a Drive search query, which is
// single-quoted with backslash escapes.
func driveQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

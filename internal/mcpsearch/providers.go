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
		// https://docs.slack.dev/ai/slack-mcp-server/. Slack answers with a
		// markdown report, which slackProvider splits into messages.
		&slackProvider{&ToolProvider{
			Name: "slack-messages",
			URL:  mcpgateway.PresetURL(mcpgateway.PresetSlack),
			Tool: "slack_search_public_and_private",
			Args: func(q Query) map[string]any {
				return map[string]any{"query": q.Text}
			},
		}},
	}
}

// driveQuote quotes s as a string value in a Drive search query, which is
// single-quoted with backslash escapes.
func driveQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

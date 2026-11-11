package mockmcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/corpus"
)

// Tool names of Slack's MCP server (https://docs.slack.dev/ai/slack-mcp-server/).
const (
	slackSearchTool = "slack_search_public_and_private"
	slackReadTool   = "slack_read_thread"
)

type slackSearchInput struct {
	Query string `json:"query"           jsonschema:"keywords to search messages for"`
	Limit int    `json:"limit,omitempty" jsonschema:"most results to return"`
}

type slackReadInput struct {
	ChannelID string `json:"channel_id" jsonschema:"the channel's name or id"`
	MessageTS string `json:"message_ts" jsonschema:"the timestamp of the thread's first message"`
}

func addSlackTools(srv *mcp.Server, b *backend) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: slackSearchTool,
		Description: "Search messages in all Slack channels the user can see, public and " +
			"private, by keyword. Returns matching messages with their channel, author, " +
			"time and permalink.",
	}, func(
		_ context.Context, _ *mcp.CallToolRequest, in slackSearchInput,
	) (*mcp.CallToolResult, any, error) {
		docs := b.search(slackSearchTool, in, in.Query, limitOr(in.Limit, 20), nil)
		// Slack's server answers in formatted text, not JSON.
		var out strings.Builder
		fmt.Fprintf(&out, "# Search results for %q\n\nFound %d messages.\n", in.Query, len(docs))
		for i, doc := range docs {
			fmt.Fprintf(&out, "\n## Result %d of %d\nChannel: #%s\nParticipants: %s\n"+
				"Message_ts: %s\nPermalink: %s\nText:\n%s\n",
				i+1, len(docs), doc.Field("channel"),
				strings.ReplaceAll(doc.Field("participants"), "\n", ", "),
				doc.Field("thread_ts"), doc.URL, snippet(doc.Text, 300))
		}
		return textResult(out.String()), nil, nil
	})
	mcp.AddTool(srv, &mcp.Tool{
		Name: slackReadTool,
		Description: "Read every message of a Slack thread, given its channel and the " +
			"timestamp of its first message.",
	}, func(
		_ context.Context, _ *mcp.CallToolRequest, in slackReadInput,
	) (*mcp.CallToolResult, any, error) {
		channel := strings.TrimPrefix(in.ChannelID, "#")
		doc := b.find(func(doc *corpus.Doc) bool {
			return doc.Field("channel") == channel && doc.Field("thread_ts") == in.MessageTS
		})
		if doc == nil {
			b.record(slackReadTool, in)
			return errorResult("no thread at %s in #%s", in.MessageTS, channel), nil, nil
		}
		b.record(slackReadTool, in, doc)
		return textResult(fmt.Sprintf("# Thread in #%s\nPermalink: %s\n\n%s",
			channel, doc.URL, doc.Text)), nil, nil
	})
	addOtherTools(srv, b, []otherTool{
		{"slack_search_public", "Search messages in public Slack channels only, by keyword."},
		{"slack_search_channels", "Find Slack channels by name or description."},
		{"slack_search_users", "Find people in the Slack workspace by name, email or title."},
		{"slack_read_channel", "Read the most recent messages of a Slack channel."},
		{"slack_read_user_profile", "Read a Slack user's profile: name, title, time zone " +
			"and status."},
		{"slack_send_message", "Send a message to a Slack channel or person."},
		{"slack_send_message_draft", "Save a draft message in Slack for the user to review " +
			"and send."},
		{"slack_schedule_message", "Schedule a Slack message to be sent later."},
		{"slack_create_canvas", "Create a Slack canvas document from Markdown."},
		{"slack_read_canvas", "Read the content of a Slack canvas."},
	})
}

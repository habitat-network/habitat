package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyToolCall(t *testing.T) {
	tests := []struct {
		name  string
		call  ToolCall
		want  Source
		isHab bool
	}{
		{"habitat search", ToolCall{Name: "mcp__habitat__search_records"}, SourceHabitat, true},
		{"habitat get_record", ToolCall{Name: "mcp__habitat__get_record"}, SourceHabitat, true},
		{
			"habitat proxied tool",
			ToolCall{Name: "mcp__habitat__Granola_list_meetings"},
			SourceHabitatGateway,
			true,
		},
		{"direct granola", ToolCall{Name: "mcp__granola__list_meetings"}, "mcp:granola", false},
		{
			"claude.ai connector",
			ToolCall{Name: "mcp__claude_ai_Google_Drive__search"},
			"mcp:claude_ai_Google_Drive",
			false,
		},
		{
			"read habitat resource",
			ToolCall{
				Name:  "ReadMcpResourceTool",
				Input: json.RawMessage(`{"server":"habitat","uri":"habitat-record:///?uri=x"}`),
			},
			SourceHabitat,
			true,
		},
		{
			"list other server resources",
			ToolCall{Name: "ListMcpResourcesTool", Input: json.RawMessage(`{"server":"granola"}`)},
			"mcp:granola",
			false,
		},
		{"list all resources", ToolCall{Name: "ListMcpResourcesTool"}, SourceMeta, false},
		{"tool search", ToolCall{Name: "ToolSearch"}, SourceMeta, false},
		{"subagent", ToolCall{Name: "Agent"}, SourceMeta, false},
		{"web search", ToolCall{Name: "WebSearch"}, "builtin:WebSearch", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.call)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.isHab, got.isHabitat())
		})
	}
}

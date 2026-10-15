package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// sampleTranscript mirrors the shape of `claude -p --output-format
// stream-json --verbose` output: an init line, assistant/user turns, and a
// final result line.
const sampleTranscript = `{"type":"system","subtype":"init","tools":["Read","ToolSearch","mcp__habitat__search_records","mcp__granola__list_meetings"],"mcp_servers":[{"name":"habitat","status":"connected"},{"name":"granola","status":"needs-auth"}]}
{"type":"assistant","message":{"content":[{"type":"text","text":"Let me look."},{"type":"tool_use","id":"t1","name":"ToolSearch","input":{"query":"meetings"}}]},"parent_tool_use_id":null}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]},"parent_tool_use_id":null}
{"type":"rate_limit_event"}

{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"mcp__habitat__Granola_list_meetings","input":{}}]},"parent_tool_use_id":"t0"}
{"type":"result","subtype":"success","is_error":false,"num_turns":3,"total_cost_usd":0.25,"duration_ms":1200,"result":"You met with Sam.","permission_denials":[{"tool_name":"Bash"}]}
`

func TestParseTranscriptExtractsInitCallsAndResult(t *testing.T) {
	got, err := parseTranscript(strings.NewReader(sampleTranscript))
	require.NoError(t, err)

	require.Equal(t, []string{
		"Read", "ToolSearch", "mcp__habitat__search_records", "mcp__granola__list_meetings",
	}, got.Tools)
	require.Equal(t, []MCPServerStatus{
		{Name: "habitat", Status: "connected"},
		{Name: "granola", Status: "needs-auth"},
	}, got.Servers)

	require.Len(t, got.Calls, 2)
	require.Equal(t, "ToolSearch", got.Calls[0].Name)
	require.JSONEq(t, `{"query":"meetings"}`, string(got.Calls[0].Input))
	require.False(t, got.Calls[0].Subagent)
	require.Equal(t, "mcp__habitat__Granola_list_meetings", got.Calls[1].Name)
	require.True(t, got.Calls[1].Subagent)

	require.NotNil(t, got.Result)
	require.Equal(t, ResultSummary{
		IsError:           false,
		NumTurns:          3,
		CostUSD:           0.25,
		DurationMS:        1200,
		Text:              "You met with Sam.",
		PermissionDenials: 1,
	}, *got.Result)
}

func TestParseTranscriptRejectsInvalidJSON(t *testing.T) {
	_, err := parseTranscript(strings.NewReader("{\"type\":\"system\"}\nnot json\n"))
	require.Error(t, err)
}

func TestParseTranscriptWithoutResult(t *testing.T) {
	got, err := parseTranscript(
		strings.NewReader(`{"type":"system","subtype":"init","tools":[]}` + "\n"),
	)
	require.NoError(t, err)
	require.Nil(t, got.Result)
}

func TestParseTranscriptAttachesToolResults(t *testing.T) {
	got, err := parseTranscript(strings.NewReader(
		`{"type":"assistant","message":{"content":[` +
			`{"type":"tool_use","id":"a","name":"mcp__habitat__search_records","input":{}},` +
			`{"type":"tool_use","id":"b","name":"Read","input":{}}]}}` + "\n" +
			`{"type":"user","message":{"content":[` +
			`{"type":"tool_result","tool_use_id":"a","content":[{"type":"text","text":"{\"hits\":[{\"uri\":\"x\"}]}"}]},` +
			`{"type":"tool_result","tool_use_id":"b","content":"file text"}]}}` + "\n",
	))
	require.NoError(t, err)
	require.Len(t, got.Calls, 2)
	require.Equal(t, `{"hits":[{"uri":"x"}]}`, got.Calls[0].Result, "text blocks are joined")
	require.Equal(t, "file text", got.Calls[1].Result, "string content is kept as is")
}

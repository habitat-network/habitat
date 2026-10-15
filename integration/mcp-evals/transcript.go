package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Transcript is what the eval needs from one `claude -p --output-format
// stream-json --verbose` run.
type Transcript struct {
	// Tools is every tool name offered to the model, including deferred tools
	// whose schemas haven't been loaded yet.
	Tools []string
	// Servers is the connection status of each MCP server at startup.
	Servers []MCPServerStatus
	// Calls is every tool call in the order the model made them, including
	// those made by subagents.
	Calls []ToolCall
	// Result is the final result line, nil if the run ended without one.
	Result *ResultSummary
}

// MCPServerStatus is an MCP server's connection status, e.g. "connected" or
// "needs-auth".
type MCPServerStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ToolCall is one tool_use block.
type ToolCall struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input,omitempty"`
	// Subagent is true if a subagent (rather than the main loop) made the call.
	Subagent bool `json:"subagent,omitempty"`
}

// ResultSummary is the final result line of a run.
type ResultSummary struct {
	IsError    bool
	NumTurns   int
	CostUSD    float64
	DurationMS int64
	Text       string
	// PermissionDenials counts tool calls the CLI refused because the tool
	// wasn't allowed (see claudeRunner's allowed and disallowed tools).
	PermissionDenials int
}

// streamLine is the union of the stream-json line shapes the eval reads.
type streamLine struct {
	Type            string            `json:"type"`
	Subtype         string            `json:"subtype"`
	Tools           []string          `json:"tools"`
	MCPServers      []MCPServerStatus `json:"mcp_servers"`
	ParentToolUseID *string           `json:"parent_tool_use_id"`
	Message         *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`

	IsError           bool              `json:"is_error"`
	NumTurns          int               `json:"num_turns"`
	TotalCostUSD      float64           `json:"total_cost_usd"`
	DurationMS        int64             `json:"duration_ms"`
	Result            string            `json:"result"`
	PermissionDenials []json.RawMessage `json:"permission_denials"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// parseTranscript reads stream-json output line by line.
func parseTranscript(r io.Reader) (Transcript, error) {
	var t Transcript
	scanner := bufio.NewScanner(r)
	// Tool results (e.g. meeting transcripts) can make single lines large.
	scanner.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var line streamLine
		if err := json.Unmarshal(raw, &line); err != nil {
			return Transcript{}, fmt.Errorf("line %d: %w", lineNo, err)
		}
		switch {
		case line.Type == "system" && line.Subtype == "init":
			t.Tools = line.Tools
			t.Servers = line.MCPServers
		case line.Type == "assistant" && line.Message != nil:
			var blocks []contentBlock
			if err := json.Unmarshal(line.Message.Content, &blocks); err != nil {
				return Transcript{}, fmt.Errorf("line %d: assistant content: %w", lineNo, err)
			}
			for _, b := range blocks {
				if b.Type != "tool_use" {
					continue
				}
				t.Calls = append(t.Calls, ToolCall{
					Name:     b.Name,
					Input:    b.Input,
					Subagent: line.ParentToolUseID != nil,
				})
			}
		case line.Type == "result":
			t.Result = &ResultSummary{
				IsError:           line.IsError,
				NumTurns:          line.NumTurns,
				CostUSD:           line.TotalCostUSD,
				DurationMS:        line.DurationMS,
				Text:              line.Result,
				PermissionDenials: len(line.PermissionDenials),
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Transcript{}, fmt.Errorf("reading transcript: %w", err)
	}
	return t, nil
}

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
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
	// Result is the text of the call's tool_result, empty if it has none.
	Result string `json:"result,omitempty"`
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
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	// ToolUseID and Content are set on tool_result blocks. Content is either
	// a string or a list of content blocks.
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	Text      string          `json:"text"`
}

// resultText returns a tool_result's content as text: a string as is, or a
// list's text blocks joined.
func resultText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var blocks []contentBlock
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" {
			texts = append(texts, b.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// parseTranscript reads stream-json output line by line.
func parseTranscript(r io.Reader) (Transcript, error) {
	var t Transcript
	// callIndex finds a tool_result's call by its tool_use id.
	callIndex := map[string]int{}
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
				callIndex[b.ID] = len(t.Calls)
				t.Calls = append(t.Calls, ToolCall{
					Name:     b.Name,
					Input:    b.Input,
					Subagent: line.ParentToolUseID != nil,
				})
			}
		case line.Type == "user" && line.Message != nil:
			// A user message's content may be a plain string (the prompt), which
			// holds no tool results.
			var blocks []contentBlock
			if json.Unmarshal(line.Message.Content, &blocks) != nil {
				continue
			}
			for _, b := range blocks {
				if i, ok := callIndex[b.ToolUseID]; ok && b.Type == "tool_result" {
					t.Calls[i].Result = resultText(b.Content)
				}
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

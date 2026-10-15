package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func calls(names ...string) []ToolCall {
	out := make([]ToolCall, len(names))
	for i, n := range names {
		out[i] = ToolCall{Name: n}
	}
	return out
}

func TestScoreRunOutcomes(t *testing.T) {
	ok := &ResultSummary{NumTurns: 2}
	tests := []struct {
		name      string
		expect    Expectation
		t         Transcript
		want      Outcome
		wantFirst string
	}{
		{
			name:      "search after meta tools passes",
			expect:    ExpectSearch,
			t:         Transcript{Calls: calls("ToolSearch", searchTool, "WebSearch"), Result: ok},
			want:      OutcomePass,
			wantFirst: searchTool,
		},
		{
			name:      "search after another tool passes",
			expect:    ExpectSearch,
			t:         Transcript{Calls: calls("WebSearch", searchTool), Result: ok},
			want:      OutcomePass,
			wantFirst: "WebSearch",
		},
		{
			name:      "other habitat tools without search fail",
			expect:    ExpectSearch,
			t:         Transcript{Calls: calls("mcp__habitat__get_record"), Result: ok},
			want:      OutcomeFail,
			wantFirst: "mcp__habitat__get_record",
		},
		{
			name:   "no tool calls fails",
			expect: ExpectSearch,
			t:      Transcript{Calls: calls("ToolSearch"), Result: ok},
			want:   OutcomeFail,
		},
		{
			name:      "negative without habitat passes",
			expect:    ExpectNone,
			t:         Transcript{Calls: calls("WebSearch"), Result: ok},
			want:      OutcomePass,
			wantFirst: "WebSearch",
		},
		{
			name:      "negative calling any habitat tool fails",
			expect:    ExpectNone,
			t:         Transcript{Calls: calls("mcp__habitat__get_blob"), Result: ok},
			want:      OutcomeFail,
			wantFirst: "mcp__habitat__get_blob",
		},
		{
			name:      "missing result is an error",
			expect:    ExpectSearch,
			t:         Transcript{Calls: calls(searchTool)},
			want:      OutcomeError,
			wantFirst: searchTool,
		},
		{
			name:   "errored result is an error",
			expect: ExpectSearch,
			t:      Transcript{Result: &ResultSummary{IsError: true}},
			want:   OutcomeError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := Scenario{ID: "s", Prompt: "p", Expect: tt.expect}
			got := scoreRun("setup", sc, 1, tt.t)
			require.Equal(t, tt.want, got.Outcome)
			require.Equal(t, tt.wantFirst, got.FirstTool)
			require.Equal(t, SetupName("setup"), got.Setup)
			require.Equal(t, ScenarioID("s"), got.Scenario)
		})
	}
}

func TestScoreRunRecordsUnusableTranscriptError(t *testing.T) {
	sc := Scenario{ID: "s", Prompt: "p", Expect: ExpectSearch}
	got := scoreRunError("setup", sc, 2, errString("claude exited 1"))
	require.Equal(t, OutcomeError, got.Outcome)
	require.Equal(t, "claude exited 1", got.Error)
	require.Equal(t, 2, got.Run)
}

type errString string

func (e errString) Error() string { return string(e) }

func TestRenderMarkdownSummarizesBySetupAndScenario(t *testing.T) {
	results := []RunResult{
		{
			Setup:     "a",
			Scenario:  "s1",
			Expect:    ExpectSearch,
			Outcome:   OutcomePass,
			FirstTool: searchTool,
			CostUSD:   0.1,
		},
		{
			Setup:     "a",
			Scenario:  "s1",
			Expect:    ExpectSearch,
			Outcome:   OutcomeFail,
			FirstTool: "WebSearch",
			CostUSD:   0.2,
		},
		{Setup: "a", Scenario: "s2", Expect: ExpectSearch, Outcome: OutcomeError},
		{Setup: "a", Scenario: "n1", Expect: ExpectNone, Outcome: OutcomePass},
	}
	md := renderMarkdown(results)
	require.Contains(t, md, "| a | search | 3 | 1 | 1/2 (50%) |")
	require.Contains(t, md, "| a | none | 1 | 0 | 1/1 (100%) |")
	require.Contains(t, md, "| a | s1 | search | 1/2 | 0 | WebSearch×1, "+searchTool+"×1 |")
	require.Contains(t, md, "| a | n1 | none | 1/1 | 0 | (none)×1 |")
	require.Contains(t, md, "$0.30")
}

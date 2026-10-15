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
		contains  []string
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
			name:     "answer with a wanted string passes",
			expect:   ExpectSearch,
			contains: []string{"2.4M", "2.4 million"},
			t: Transcript{
				Calls:  calls(searchTool),
				Result: &ResultSummary{Text: "a $2.4 Million round"},
			},
			want:      OutcomePass,
			wantFirst: searchTool,
		},
		{
			name:     "answer without any wanted string fails",
			expect:   ExpectSearch,
			contains: []string{"2.4M"},
			t: Transcript{
				Calls:  calls(searchTool),
				Result: &ResultSummary{Text: "unknown"},
			},
			want:      OutcomeFail,
			wantFirst: searchTool,
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
			turn := Turn{Prompt: "p", Expect: tt.expect, AnswerContains: tt.contains}
			got := scoreTurn("setup", "s", 1, 2, turn, tt.t)
			require.Equal(t, tt.want, got.Outcome)
			require.Equal(t, tt.wantFirst, got.FirstTool)
			require.Equal(t, SetupName("setup"), got.Setup)
			require.Equal(t, ScenarioID("s"), got.Scenario)
			require.Equal(t, 2, got.Turn)
			require.Equal(t, tt.expect, got.Expect)
			if got.Outcome == OutcomeFail {
				require.NotEmpty(t, got.FailReason, "a failure says why")
			}
		})
	}
}

func TestScoreRunRecordsUnusableTranscriptError(t *testing.T) {
	turn := Turn{Prompt: "p", Expect: ExpectSearch}
	got := scoreTurnError("setup", "s", 2, 3, turn, errString("claude exited 1"))
	require.Equal(t, OutcomeError, got.Outcome)
	require.Equal(t, "claude exited 1", got.Error)
	require.Equal(t, 2, got.Run)
	require.Equal(t, 3, got.Turn)
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
	for i := range results {
		results[i].Turn = 1
	}
	md := renderMarkdown(results)
	require.Contains(t, md, "| a | search | 3 | 1 | 1/2 (50%) |")
	require.Contains(t, md, "| a | none | 1 | 0 | 1/1 (100%) |")
	require.Contains(t, md, "| a | s1 | 1 | search | 1/2 | 0 | WebSearch×1, "+searchTool+"×1 |")
	require.Contains(t, md, "| a | n1 | 1 | none | 1/1 | 0 | (none)×1 |")
	require.Contains(t, md, "$0.30")
	require.NotContains(t, md, "Later turns", "only shown with multi-turn scenarios")
}

func TestRenderMarkdownSummarizesLaterTurns(t *testing.T) {
	turn := func(scenario ScenarioID, run, n int, expect Expectation, o Outcome) RunResult {
		return RunResult{
			Setup: "a", Scenario: scenario, Run: run, Turn: n, Expect: expect, Outcome: o,
			FirstTool: searchTool,
		}
	}
	results := []RunResult{
		turn("c", 1, 1, ExpectSearch, OutcomePass),
		turn("c", 1, 2, ExpectSearch, OutcomePass),
		turn("c", 2, 1, ExpectSearch, OutcomePass),
		turn("c", 2, 2, ExpectSearch, OutcomeFail),
		turn("d", 1, 1, ExpectSearch, OutcomePass),
		turn("d", 1, 2, ExpectNone, OutcomePass),
		turn("single", 1, 1, ExpectSearch, OutcomeFail),
	}
	md := renderMarkdown(results)
	require.Contains(t, md, "## Later turns")
	require.Contains(t, md, "| a | search | 2 | 0 | 1/2 (50%) | "+searchTool+"×2 |",
		"turn 2+ only, by expectation")
	require.Contains(t, md, "| a | none | 1 | 0 | 1/1 (100%) |")
	require.Contains(t, md, "| a | 3 | 0 | 2/3 (67%) |",
		"multi-turn conversations pass when every turn does; single-turn ones aren't counted")
	require.Contains(t, md, "| a | c | 2 | search | 1/2 | 0 |")
}

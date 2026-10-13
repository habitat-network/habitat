package main

import (
	"strings"
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
	okResult := &ResultSummary{NumTurns: 2, CostUSD: 0.1}
	tests := []struct {
		name      string
		expect    Expectation
		t         Transcript
		want      Outcome
		wantFirst Source
		wantCalls int
	}{
		{
			name:   "habitat first after meta passes",
			expect: ExpectHabitat,
			t: Transcript{
				Calls:  calls("ToolSearch", "mcp__habitat__search_records", "WebSearch"),
				Result: okResult,
			},
			want:      OutcomePass,
			wantFirst: SourceHabitat,
			wantCalls: 2,
		},
		{
			name:   "direct server first fails even if habitat used later",
			expect: ExpectHabitat,
			t: Transcript{
				Calls:  calls("mcp__granola__list_meetings", "mcp__habitat__search_records"),
				Result: okResult,
			},
			want:      OutcomeFail,
			wantFirst: "mcp:granola",
			wantCalls: 2,
		},
		{
			name:      "no data calls fails habitat expectation",
			expect:    ExpectHabitat,
			t:         Transcript{Calls: calls("ToolSearch"), Result: okResult},
			want:      OutcomeFail,
			wantFirst: "",
		},
		{
			name:      "negative without habitat passes",
			expect:    ExpectNone,
			t:         Transcript{Calls: calls("WebSearch"), Result: okResult},
			want:      OutcomePass,
			wantFirst: "builtin:WebSearch",
			wantCalls: 1,
		},
		{
			name:   "negative touching habitat fails",
			expect: ExpectNone,
			t: Transcript{
				Calls:  calls("WebSearch", "mcp__habitat__Granola_list_meetings"),
				Result: okResult,
			},
			want:      OutcomeFail,
			wantFirst: "builtin:WebSearch",
			wantCalls: 2,
		},
		{
			name:   "any is unscored",
			expect: ExpectAny,
			t: Transcript{
				Calls:  calls("mcp__claude_ai_Google_Drive__search"),
				Result: okResult,
			},
			want:      OutcomeUnscored,
			wantFirst: "mcp:claude_ai_Google_Drive",
			wantCalls: 1,
		},
		{
			name:   "missing result is an error",
			expect: ExpectHabitat,
			t:      Transcript{Calls: calls("mcp__habitat__search_records")},
			want:   OutcomeError,
			// The call still happened, so it is still reported.
			wantFirst: SourceHabitat,
			wantCalls: 1,
		},
		{
			name:      "errored result is an error",
			expect:    ExpectHabitat,
			t:         Transcript{Result: &ResultSummary{IsError: true}},
			want:      OutcomeError,
			wantFirst: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := Scenario{ID: "s", Category: CategoryGateway, Prompt: "p", Expect: tt.expect}
			got := scoreRun("setup", sc, 1, tt.t)
			require.Equal(t, tt.want, got.Outcome)
			require.Equal(t, tt.wantFirst, got.FirstDataSource)
			require.Equal(t, tt.wantCalls, got.DataCalls)
			require.Equal(t, SetupName("setup"), got.Setup)
			require.Equal(t, ScenarioID("s"), got.Scenario)
		})
	}
}

func TestScoreRunRecordsUnusableTranscriptError(t *testing.T) {
	sc := Scenario{ID: "s", Category: CategoryNative, Prompt: "p", Expect: ExpectHabitat}
	got := scoreRunError("setup", sc, 2, errString("claude exited 1"))
	require.Equal(t, OutcomeError, got.Outcome)
	require.Equal(t, "claude exited 1", got.Error)
	require.Equal(t, 2, got.Run)
}

type errString string

func (e errString) Error() string { return string(e) }

func TestSummarizeAggregatesBySetupAndCategory(t *testing.T) {
	results := []RunResult{
		{
			Setup:           "a",
			Scenario:        "s1",
			Category:        CategoryGateway,
			Expect:          ExpectHabitat,
			Outcome:         OutcomePass,
			FirstDataSource: SourceHabitatGateway,
			UsedHabitat:     true,
			DataCalls:       2,
			CostUSD:         0.1,
		},
		{
			Setup:           "a",
			Scenario:        "s1",
			Category:        CategoryGateway,
			Expect:          ExpectHabitat,
			Outcome:         OutcomeFail,
			FirstDataSource: "mcp:granola",
			UsedHabitat:     true,
			DataCalls:       4,
			CostUSD:         0.3,
		},
		{
			Setup:    "a",
			Scenario: "s2",
			Category: CategoryGateway,
			Expect:   ExpectHabitat,
			Outcome:  OutcomeError,
			CostUSD:  0.05,
		},
		{
			Setup:       "a",
			Scenario:    "n1",
			Category:    CategoryNegative,
			Expect:      ExpectNone,
			Outcome:     OutcomeFail,
			UsedHabitat: true,
			DataCalls:   1,
		},
		{
			Setup:           "b",
			Scenario:        "s1",
			Category:        CategoryGateway,
			Expect:          ExpectHabitat,
			Outcome:         OutcomePass,
			FirstDataSource: SourceHabitat,
			UsedHabitat:     true,
			DataCalls:       1,
		},
	}
	got := summarize(results)
	require.Equal(t, []SummaryRow{
		{
			Setup: "a", Category: CategoryGateway, Runs: 3, Errors: 1, Scored: 2, Passed: 1,
			HabitatFirst: 1, HabitatAny: 2, MeanDataCalls: 3, CostUSD: 0.45,
		},
		{
			Setup: "a", Category: CategoryNegative, Runs: 1, Scored: 1, Passed: 0,
			HabitatAny: 1, MeanDataCalls: 1,
		},
		{
			Setup: "b", Category: CategoryGateway, Runs: 1, Scored: 1, Passed: 1,
			HabitatFirst: 1, HabitatAny: 1, MeanDataCalls: 1,
		},
	}, roundCosts(got))

	md := renderMarkdown(results)
	require.Contains(t, md, "| a | gateway | 3 | 1 | 50% |")
	require.Contains(t, md, "| a | s1 | gateway | habitat | 1/2 |")
	require.True(
		t,
		strings.Contains(md, "mcp:granola"),
		"first-source breakdown lists direct servers",
	)
}

// roundCosts trims float noise from summed costs so rows compare exactly.
func roundCosts(rows []SummaryRow) []SummaryRow {
	for i := range rows {
		rows[i].CostUSD = float64(int(rows[i].CostUSD*1000+0.5)) / 1000
	}
	return rows
}

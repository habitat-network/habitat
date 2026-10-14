package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Outcome is how one run scored against its scenario's Expectation.
type Outcome string

const (
	OutcomePass     Outcome = "pass"
	OutcomeFail     Outcome = "fail"
	OutcomeUnscored Outcome = "unscored"
	// OutcomeError means the run didn't finish cleanly, so it's left out of
	// pass rates.
	OutcomeError Outcome = "error"
)

// RunResult is one scored run, written as a line of results.jsonl.
type RunResult struct {
	Setup    SetupName   `json:"setup"`
	Scenario ScenarioID  `json:"scenario"`
	Category Category    `json:"category"`
	Expect   Expectation `json:"expect"`
	Run      int         `json:"run"`
	Outcome  Outcome     `json:"outcome"`
	// FirstDataSource is the Source of the first non-meta tool call, empty if
	// the model fetched nothing.
	FirstDataSource Source `json:"first_data_source"`
	// UsedHabitat is true if any call went to Habitat.
	UsedHabitat bool `json:"used_habitat"`
	// HabitatExclusive is true if Habitat was used and no other MCP server
	// was, i.e. the model preferred Habitat over any duplicate tools.
	HabitatExclusive bool `json:"habitat_exclusive"`
	// DataCalls counts non-meta tool calls.
	DataCalls int `json:"data_calls"`
	// ToolSequence is every call's tool name, in order.
	ToolSequence []string `json:"tool_sequence"`
	// ToolSearchQueries are the queries the model used to find deferred
	// tools, which show which words should appear in tool descriptions.
	ToolSearchQueries []string `json:"tool_search_queries,omitempty"`
	NumTurns          int      `json:"num_turns"`
	CostUSD           float64  `json:"cost_usd"`
	DurationMS        int64    `json:"duration_ms"`
	PermissionDenials int      `json:"permission_denials"`
	Answer            string   `json:"answer,omitempty"`
	Error             string   `json:"error,omitempty"`
}

func scoreRun(setup SetupName, sc Scenario, run int, t Transcript) RunResult {
	r := RunResult{
		Setup: setup, Scenario: sc.ID, Category: sc.Category, Expect: sc.Expect, Run: run,
	}
	usedOtherMCP := false
	for _, call := range t.Calls {
		r.ToolSequence = append(r.ToolSequence, call.Name)
		if call.Name == "ToolSearch" {
			if q := toolSearchQuery(call); q != "" {
				r.ToolSearchQueries = append(r.ToolSearchQueries, q)
			}
		}
		src := classify(call)
		if src == SourceMeta {
			continue
		}
		r.DataCalls++
		if r.FirstDataSource == "" {
			r.FirstDataSource = src
		}
		if src.isHabitat() {
			r.UsedHabitat = true
		} else if strings.HasPrefix(string(src), "mcp:") {
			usedOtherMCP = true
		}
	}
	r.HabitatExclusive = r.UsedHabitat && !usedOtherMCP
	if t.Result != nil {
		r.NumTurns = t.Result.NumTurns
		r.CostUSD = t.Result.CostUSD
		r.DurationMS = t.Result.DurationMS
		r.PermissionDenials = t.Result.PermissionDenials
		r.Answer = t.Result.Text
	}

	switch {
	case t.Result == nil:
		r.Outcome, r.Error = OutcomeError, "run ended without a result"
	case t.Result.IsError:
		r.Outcome, r.Error = OutcomeError, "result is_error: "+t.Result.Text
	case sc.Expect == ExpectAny:
		r.Outcome = OutcomeUnscored
	// Habitat being invoked at all passes for now, even alongside duplicate
	// tools from other servers; FirstDataSource and HabitatExclusive measure
	// how strongly it's preferred.
	case sc.Expect == ExpectHabitat && r.UsedHabitat,
		sc.Expect == ExpectNone && !r.UsedHabitat:
		r.Outcome = OutcomePass
	default:
		r.Outcome = OutcomeFail
	}
	return r
}

// scoreRunError records a run whose transcript couldn't be produced or read.
func scoreRunError(setup SetupName, sc Scenario, run int, err error) RunResult {
	return RunResult{
		Setup: setup, Scenario: sc.ID, Category: sc.Category, Expect: sc.Expect, Run: run,
		Outcome: OutcomeError, Error: err.Error(),
	}
}

func toolSearchQuery(call ToolCall) string {
	var input struct {
		Query string `json:"query"`
	}
	if json.Unmarshal(call.Input, &input) != nil {
		return ""
	}
	return input.Query
}

// SummaryRow aggregates the runs of one setup and category.
type SummaryRow struct {
	Setup    SetupName
	Category Category
	Runs     int
	Errors   int
	// Scored and Passed count non-error runs whose scenario is scored.
	Scored int
	Passed int
	// HabitatFirst, HabitatExclusive and HabitatAny count non-error runs.
	HabitatFirst     int
	HabitatExclusive int
	HabitatAny       int
	MeanDataCalls    float64
	CostUSD          float64
}

type summaryKey struct {
	setup    SetupName
	category Category
}

var categoryOrder = []Category{
	CategoryNative, CategoryGateway, CategoryAmbiguous, CategoryNegative,
}

func summarize(results []RunResult) []SummaryRow {
	rows := map[summaryKey]*SummaryRow{}
	dataCalls := map[summaryKey]int{}
	for _, r := range results {
		k := summaryKey{r.Setup, r.Category}
		row, ok := rows[k]
		if !ok {
			row = &SummaryRow{Setup: r.Setup, Category: r.Category}
			rows[k] = row
		}
		row.Runs++
		row.CostUSD += r.CostUSD
		if r.Outcome == OutcomeError {
			row.Errors++
			continue
		}
		if r.Outcome != OutcomeUnscored {
			row.Scored++
		}
		if r.Outcome == OutcomePass {
			row.Passed++
		}
		if r.FirstDataSource.isHabitat() {
			row.HabitatFirst++
		}
		if r.HabitatExclusive {
			row.HabitatExclusive++
		}
		if r.UsedHabitat {
			row.HabitatAny++
		}
		dataCalls[k] += r.DataCalls
	}
	out := make([]SummaryRow, 0, len(rows))
	for k, row := range rows {
		if n := row.Runs - row.Errors; n > 0 {
			row.MeanDataCalls = float64(dataCalls[k]) / float64(n)
		}
		out = append(out, *row)
	}
	slices.SortFunc(out, func(a, b SummaryRow) int {
		return cmp.Or(
			cmp.Compare(a.Setup, b.Setup),
			cmp.Compare(
				slices.Index(categoryOrder, a.Category), slices.Index(categoryOrder, b.Category),
			),
		)
	})
	return out
}

func percent(n, d int) string {
	if d == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(d))
}

// renderMarkdown renders a summary by setup and category, then a per-scenario
// breakdown of where each scenario's runs went first.
func renderMarkdown(results []RunResult) string {
	var b strings.Builder
	b.WriteString("## Summary by setup and category\n\n")
	b.WriteString(
		"Pass: Habitat was invoked at all (for negative, never invoked). Habitat first: the " +
			"first data call went to Habitat. Habitat exclusive: Habitat used and no other MCP " +
			"server was. Rates other than pass are over non-error runs.\n\n",
	)
	b.WriteString("| setup | category | runs | errors | pass | habitat first | habitat exclusive " +
		"| habitat any | mean data calls | cost |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, row := range summarize(results) {
		ok := row.Runs - row.Errors
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %s | %s | %s | %s | %.1f | $%.2f |\n",
			row.Setup, row.Category, row.Runs, row.Errors, percent(row.Passed, row.Scored),
			percent(row.HabitatFirst, ok), percent(row.HabitatExclusive, ok),
			percent(row.HabitatAny, ok),
			row.MeanDataCalls, row.CostUSD)
	}

	type scenarioKey struct {
		setup    SetupName
		scenario ScenarioID
	}
	type scenarioAgg struct {
		category       Category
		expect         Expectation
		passed, scored int
		errors         int
		exclusive      int
		first          map[Source]int
	}
	aggs := map[scenarioKey]*scenarioAgg{}
	var keys []scenarioKey
	for _, r := range results {
		k := scenarioKey{r.Setup, r.Scenario}
		a, ok := aggs[k]
		if !ok {
			a = &scenarioAgg{category: r.Category, expect: r.Expect, first: map[Source]int{}}
			aggs[k] = a
			keys = append(keys, k)
		}
		switch r.Outcome {
		case OutcomeError:
			a.errors++
			continue
		case OutcomePass:
			a.passed++
			a.scored++
		case OutcomeFail:
			a.scored++
		}
		if r.HabitatExclusive {
			a.exclusive++
		}
		src := r.FirstDataSource
		if src == "" {
			src = "(none)"
		}
		a.first[src]++
	}
	slices.SortStableFunc(keys, func(a, b scenarioKey) int { return cmp.Compare(a.setup, b.setup) })

	b.WriteString("\n## By scenario\n\n")
	b.WriteString(
		"| setup | scenario | category | expect | passed | errors | habitat exclusive " +
			"| first data source |\n",
	)
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, k := range keys {
		a := aggs[k]
		passed := "-"
		if a.scored > 0 {
			passed = fmt.Sprintf("%d/%d", a.passed, a.scored)
		}
		// Every non-error run is counted in a.first.
		ok := sumCounts(a.first)
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %d | %d/%d | %s |\n",
			k.setup, k.scenario, a.category, a.expect, passed, a.errors, a.exclusive, ok,
			formatCounts(a.first))
	}
	return b.String()
}

// formatCounts lists sources by descending count, e.g. "habitat×3,
// mcp:granola×2".
func formatCounts(counts map[Source]int) string {
	sources := slices.Collect(maps.Keys(counts))
	slices.SortFunc(sources, func(a, b Source) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b))
	})
	parts := make([]string, len(sources))
	for i, s := range sources {
		parts[i] = fmt.Sprintf("%s×%d", s, counts[s])
	}
	return strings.Join(parts, ", ")
}

func sumCounts(counts map[Source]int) int {
	n := 0
	for _, c := range counts {
		n += c
	}
	return n
}

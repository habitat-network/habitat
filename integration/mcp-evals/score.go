package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// habitatServer is the name every setup's MCP config must give the Habitat
// server, so its tools are named mcp__habitat__<tool>.
const habitatServer = "habitat"

// habitatToolPrefix prefixes every Habitat tool, its own and those it proxies.
const habitatToolPrefix = "mcp__" + habitatServer + "__"

// searchTool is Habitat's record search tool (see internal/mcpserver), which
// should be called whenever a prompt needs company context.
const searchTool = habitatToolPrefix + "search_records"

// metaTools are built-in tools that don't fetch anything themselves, skipped
// when finding a run's first tool. Calls a subagent (Agent) makes are
// recorded on their own, so Agent is one too.
var metaTools = map[string]bool{
	"ToolSearch":      true,
	"TodoWrite":       true,
	"Skill":           true,
	"Agent":           true,
	"Task":            true,
	"AskUserQuestion": true,
	"EnterPlanMode":   true,
	"ExitPlanMode":    true,
}

// Outcome is how one run scored against its scenario's Expectation.
type Outcome string

const (
	OutcomePass Outcome = "pass"
	OutcomeFail Outcome = "fail"
	// OutcomeError means the run didn't finish cleanly, so it's left out of
	// pass rates.
	OutcomeError Outcome = "error"
)

// RunResult is one scored run, written as a line of results.jsonl.
type RunResult struct {
	Setup    SetupName   `json:"setup"`
	Scenario ScenarioID  `json:"scenario"`
	Expect   Expectation `json:"expect"`
	Run      int         `json:"run"`
	Outcome  Outcome     `json:"outcome"`
	// FirstTool is the first non-meta tool called, empty if none was.
	FirstTool string `json:"first_tool"`
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
	r := RunResult{Setup: setup, Scenario: sc.ID, Expect: sc.Expect, Run: run}
	usedSearch, usedHabitat := false, false
	for _, call := range t.Calls {
		r.ToolSequence = append(r.ToolSequence, call.Name)
		if call.Name == "ToolSearch" {
			if q := toolSearchQuery(call); q != "" {
				r.ToolSearchQueries = append(r.ToolSearchQueries, q)
			}
		}
		if r.FirstTool == "" && !metaTools[call.Name] {
			r.FirstTool = call.Name
		}
		usedSearch = usedSearch || call.Name == searchTool
		usedHabitat = usedHabitat || strings.HasPrefix(call.Name, habitatToolPrefix)
	}
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
	case sc.Expect == ExpectSearch && usedSearch, sc.Expect == ExpectNone && !usedHabitat:
		r.Outcome = OutcomePass
	default:
		r.Outcome = OutcomeFail
	}
	return r
}

// scoreRunError records a run whose transcript couldn't be produced or read.
func scoreRunError(setup SetupName, sc Scenario, run int, err error) RunResult {
	return RunResult{
		Setup: setup, Scenario: sc.ID, Expect: sc.Expect, Run: run,
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

// tally counts runs; passed and scored leave out errored runs.
type tally struct {
	runs, errors, passed, scored int
	firstTools                   map[string]int
}

func (t *tally) add(r RunResult) {
	t.runs++
	if r.Outcome == OutcomeError {
		t.errors++
		return
	}
	t.scored++
	if r.Outcome == OutcomePass {
		t.passed++
	}
	tool := cmp.Or(r.FirstTool, "(none)")
	if t.firstTools == nil {
		t.firstTools = map[string]int{}
	}
	t.firstTools[tool]++
}

func (t *tally) passRate() string {
	if t.scored == 0 {
		return "-"
	}
	return fmt.Sprintf(
		"%d/%d (%.0f%%)",
		t.passed,
		t.scored,
		100*float64(t.passed)/float64(t.scored),
	)
}

// renderMarkdown renders pass rates by setup and expectation, then by
// scenario with the first tool each scenario's runs called.
func renderMarkdown(results []RunResult) string {
	type groupKey struct {
		setup  SetupName
		expect Expectation
	}
	type scenarioKey struct {
		setup    SetupName
		scenario ScenarioID
	}
	groups := map[groupKey]*tally{}
	scenarios := map[scenarioKey]*tally{}
	expects := map[scenarioKey]Expectation{}
	var groupOrder []groupKey
	var scenarioOrder []scenarioKey
	cost := 0.0
	for _, r := range results {
		cost += r.CostUSD
		gk := groupKey{r.Setup, r.Expect}
		if groups[gk] == nil {
			groups[gk] = &tally{}
			groupOrder = append(groupOrder, gk)
		}
		groups[gk].add(r)
		sk := scenarioKey{r.Setup, r.Scenario}
		if scenarios[sk] == nil {
			scenarios[sk] = &tally{}
			scenarioOrder = append(scenarioOrder, sk)
			expects[sk] = r.Expect
		}
		scenarios[sk].add(r)
	}

	var b strings.Builder
	b.WriteString("## Summary\n\n")
	b.WriteString("Pass: `search` prompts called " + searchTool + "; `none` prompts called no " +
		"Habitat tool. Errored runs are left out of pass rates.\n\n")
	b.WriteString("| setup | expect | runs | errors | pass |\n|---|---|---|---|---|\n")
	for _, k := range groupOrder {
		g := groups[k]
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %s |\n",
			k.setup, k.expect, g.runs, g.errors, g.passRate())
	}

	b.WriteString("\n## By scenario\n\n")
	b.WriteString("| setup | scenario | expect | passed | errors | first tool |\n" +
		"|---|---|---|---|---|---|\n")
	for _, k := range scenarioOrder {
		s := scenarios[k]
		fmt.Fprintf(&b, "| %s | %s | %s | %d/%d | %d | %s |\n",
			k.setup, k.scenario, expects[k], s.passed, s.scored, s.errors,
			formatCounts(s.firstTools))
	}
	fmt.Fprintf(&b, "\nTotal est. API cost: $%.2f\n", cost)
	return b.String()
}

// formatCounts lists names by descending count, e.g. "a×3, b×1".
func formatCounts(counts map[string]int) string {
	names := slices.Collect(maps.Keys(counts))
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b))
	})
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("%s×%d", n, counts[n])
	}
	return strings.Join(parts, ", ")
}

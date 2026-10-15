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

// RunResult is one scored turn of a run, written as a line of results.jsonl.
// A single-turn scenario's run is one RunResult with Turn 1.
type RunResult struct {
	Setup    SetupName   `json:"setup"`
	Scenario ScenarioID  `json:"scenario"`
	Expect   Expectation `json:"expect"`
	Run      int         `json:"run"`
	// Turn is the 1-based turn of the scenario's conversation.
	Turn    int     `json:"turn"`
	Outcome Outcome `json:"outcome"`
	// FailReason says why a failed turn failed.
	FailReason string `json:"fail_reason,omitempty"`
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

// scoreTurn scores what the model did in response to one turn, from that
// turn's transcript alone.
func scoreTurn(
	setup SetupName, scenario ScenarioID, run, turnNo int, turn Turn, t Transcript,
) RunResult {
	r := RunResult{
		Setup: setup, Scenario: scenario, Expect: turn.Expect, Run: run, Turn: turnNo,
	}
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
	case turn.Expect == ExpectSearch && !usedSearch:
		r.Outcome, r.FailReason = OutcomeFail, "didn't call "+searchTool
	case turn.Expect == ExpectNone && usedHabitat:
		r.Outcome, r.FailReason = OutcomeFail, "called a Habitat tool"
	case !answerContainsAny(r.Answer, turn.AnswerContains):
		r.Outcome = OutcomeFail
		r.FailReason = fmt.Sprintf("answer contains none of %q", turn.AnswerContains)
	default:
		r.Outcome = OutcomePass
	}
	return r
}

// answerContainsAny reports whether answer contains one of want,
// case-insensitively. An empty want always matches.
func answerContainsAny(answer string, want []string) bool {
	if len(want) == 0 {
		return true
	}
	answer = strings.ToLower(answer)
	return slices.ContainsFunc(want, func(w string) bool {
		return strings.Contains(answer, strings.ToLower(w))
	})
}

// scoreTurnError records a turn whose transcript couldn't be produced or read.
func scoreTurnError(
	setup SetupName, scenario ScenarioID, run, turnNo int, turn Turn, err error,
) RunResult {
	return RunResult{
		Setup: setup, Scenario: scenario, Expect: turn.Expect, Run: run, Turn: turnNo,
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
// scenario turn with the first tool each turn's runs called. With multi-turn
// scenarios, it adds pass rates for their later turns (2+) and whole
// conversations.
func renderMarkdown(results []RunResult) string {
	type groupKey struct {
		setup  SetupName
		expect Expectation
	}
	type scenarioKey struct {
		setup    SetupName
		scenario ScenarioID
		turn     int
	}
	type conversationKey struct {
		setup    SetupName
		scenario ScenarioID
		run      int
	}
	groups, later := map[groupKey]*tally{}, map[groupKey]*tally{}
	scenarios := map[scenarioKey]*tally{}
	expects := map[scenarioKey]Expectation{}
	conversations := map[conversationKey][]Outcome{}
	var groupOrder, laterOrder []groupKey
	var scenarioOrder []scenarioKey
	var conversationOrder []conversationKey
	add := func(m map[groupKey]*tally, order *[]groupKey, k groupKey, r RunResult) {
		if m[k] == nil {
			m[k] = &tally{}
			*order = append(*order, k)
		}
		m[k].add(r)
	}
	cost := 0.0
	for _, r := range results {
		cost += r.CostUSD
		gk := groupKey{r.Setup, r.Expect}
		add(groups, &groupOrder, gk, r)
		if r.Turn > 1 {
			add(later, &laterOrder, gk, r)
		}
		sk := scenarioKey{r.Setup, r.Scenario, r.Turn}
		if scenarios[sk] == nil {
			scenarios[sk] = &tally{}
			scenarioOrder = append(scenarioOrder, sk)
			expects[sk] = r.Expect
		}
		scenarios[sk].add(r)
		ck := conversationKey{r.Setup, r.Scenario, r.Run}
		if conversations[ck] == nil {
			conversationOrder = append(conversationOrder, ck)
		}
		conversations[ck] = append(conversations[ck], r.Outcome)
	}

	var b strings.Builder
	b.WriteString("## Summary\n\n")
	b.WriteString("Pass: `search` turns called " + searchTool + "; `none` turns called no " +
		"Habitat tool; with answer_contains, the answer also had one of its strings. " +
		"Errored turns are left out of pass rates.\n\n")
	writeGroups := func(m map[groupKey]*tally, order []groupKey, firstTools bool) {
		if firstTools {
			b.WriteString("| setup | expect | runs | errors | pass | first tool |\n" +
				"|---|---|---|---|---|---|\n")
		} else {
			b.WriteString("| setup | expect | runs | errors | pass |\n|---|---|---|---|---|\n")
		}
		for _, k := range order {
			g := m[k]
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %s |", k.setup, k.expect, g.runs, g.errors,
				g.passRate())
			if firstTools {
				fmt.Fprintf(&b, " %s |", formatCounts(g.firstTools))
			}
			b.WriteString("\n")
		}
	}
	writeGroups(groups, groupOrder, false)

	if len(laterOrder) > 0 {
		b.WriteString("\n## Later turns\n\n")
		b.WriteString("Turns 2+ of multi-turn scenarios, where earlier turns' context could " +
			"stand in for searching again.\n\n")
		writeGroups(later, laterOrder, true)

		// A conversation passes when every turn does, and errors if any did.
		convs := map[SetupName]*tally{}
		var convOrder []SetupName
		for _, k := range conversationOrder {
			outcomes := conversations[k]
			if len(outcomes) < 2 {
				continue
			}
			if convs[k.setup] == nil {
				convs[k.setup] = &tally{}
				convOrder = append(convOrder, k.setup)
			}
			c := convs[k.setup]
			c.runs++
			switch {
			case slices.Contains(outcomes, OutcomeError):
				c.errors++
			case slices.Contains(outcomes, OutcomeFail):
				c.scored++
			default:
				c.scored++
				c.passed++
			}
		}
		b.WriteString("\n| setup | conversations | errors | every turn passed |\n" +
			"|---|---|---|---|\n")
		for _, setup := range convOrder {
			c := convs[setup]
			fmt.Fprintf(&b, "| %s | %d | %d | %s |\n", setup, c.runs, c.errors, c.passRate())
		}
	}

	b.WriteString("\n## By scenario\n\n")
	b.WriteString("| setup | scenario | turn | expect | passed | errors | first tool |\n" +
		"|---|---|---|---|---|---|---|\n")
	for _, k := range scenarioOrder {
		s := scenarios[k]
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %d/%d | %d | %s |\n",
			k.setup, k.scenario, k.turn, expects[k], s.passed, s.scored, s.errors,
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

package mcpserver

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"unicode"

	"github.com/habitat-network/habitat/internal/decisions"
)

// ToolCandidate is a tool of one of the caller's connected MCP servers that
// may be suggested for a query.
type ToolCandidate struct {
	// Name is the namespaced name call_tool takes (see namespaceTool).
	Name        string
	Description string
}

// ScoredTool is a candidate with how well it fits a query, from 0 to 1.
type ScoredTool struct {
	ToolCandidate
	Score float64
}

// ToolSelector ranks candidate tools by how useful they'd be for a query.
// Its result is advice: the caller can still call any tool.
type ToolSelector interface {
	// Select returns at most limit of candidates, best first. It never
	// returns tools that don't fit the query at all.
	Select(
		ctx context.Context,
		query string,
		candidates []ToolCandidate,
		limit int,
	) ([]ScoredTool, error)
}

// LexicalSelector ranks tools by how many of the query's words appear in
// their name and description. It needs no external service, so it's the
// default and the fallback of DecisionSelector.
type LexicalSelector struct{}

var _ ToolSelector = LexicalSelector{}

// Select implements ToolSelector.
func (LexicalSelector) Select(
	_ context.Context,
	query string,
	candidates []ToolCandidate,
	limit int,
) ([]ScoredTool, error) {
	queryWords := words(query)
	if len(queryWords) == 0 {
		return nil, nil
	}
	var scored []ScoredTool
	for _, c := range candidates {
		have := make(map[string]bool)
		for _, w := range words(c.Name + " " + c.Description) {
			have[w] = true
		}
		matched := 0
		for _, w := range queryWords {
			if have[w] {
				matched++
			}
		}
		if matched > 0 {
			scored = append(scored, ScoredTool{
				ToolCandidate: c,
				Score:         float64(matched) / float64(len(queryWords)),
			})
		}
	}
	sortScored(scored)
	return truncateScored(scored, limit), nil
}

// words lowercases s and splits it into distinct words, dropping very short
// ones that carry no meaning ("a", "of"). Tool names like "search_issues"
// split on their punctuation.
func words(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := make(map[string]bool, len(fields))
	out := fields[:0]
	for _, f := range fields {
		if len(f) < 3 || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

func sortScored(tools []ScoredTool) {
	sort.SliceStable(tools, func(i, j int) bool {
		if tools[i].Score != tools[j].Score {
			return tools[i].Score > tools[j].Score
		}
		return tools[i].Name < tools[j].Name
	})
}

func truncateScored(tools []ScoredTool, limit int) []ScoredTool {
	if limit > 0 && len(tools) > limit {
		return tools[:limit]
	}
	return tools
}

// Chooser picks the option that best fits an input. *decisions.Client
// implements it.
type Chooser interface {
	Choose(
		ctx context.Context,
		input string,
		instructions string,
		options []decisions.Option,
	) (decisions.Choice, error)
}

const (
	// decisionShortlist caps how many lexically-ranked candidates the
	// Decisions API chooses between, to bound the size of its request.
	decisionShortlist = 30
	// minDecisionProbability drops tools the model gives almost no chance.
	minDecisionProbability = 0.05
	chooseToolInstructions = "Pick the tool that would be most useful for answering the " +
		"user's request or doing what it asks. The input is the request."
)

// DecisionSelector asks a Decisions API which tool best fits the query, and
// ranks by the probability it gives each. With a long tool list it only
// asks about the best lexical matches. If the API fails, it falls back to
// LexicalSelector so suggestions never fail because the model did.
type DecisionSelector struct {
	Chooser Chooser
}

var _ ToolSelector = DecisionSelector{}

// Select implements ToolSelector.
func (s DecisionSelector) Select(
	ctx context.Context,
	query string,
	candidates []ToolCandidate,
	limit int,
) ([]ScoredTool, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	shortlist := candidates
	if len(candidates) > decisionShortlist {
		lexical, _ := LexicalSelector{}.Select(ctx, query, candidates, decisionShortlist)
		shortlist = make([]ToolCandidate, len(lexical))
		for i, t := range lexical {
			shortlist[i] = t.ToolCandidate
		}
		if len(shortlist) == 0 {
			return nil, nil
		}
	}
	options := make([]decisions.Option, len(shortlist))
	byName := make(map[string]ToolCandidate, len(shortlist))
	for i, c := range shortlist {
		options[i] = decisions.Option{Value: c.Name, Description: c.Description}
		byName[c.Name] = c
	}
	choice, err := s.Chooser.Choose(ctx, query, chooseToolInstructions, options)
	if err != nil {
		slog.WarnContext(ctx, "mcp: choosing tools; using lexical match", "err", err)
		return LexicalSelector{}.Select(ctx, query, candidates, limit)
	}
	var scored []ScoredTool
	for name, p := range choice.Probabilities {
		if c, ok := byName[name]; ok && p >= minDecisionProbability {
			scored = append(scored, ScoredTool{ToolCandidate: c, Score: p})
		}
	}
	sortScored(scored)
	return truncateScored(scored, limit), nil
}

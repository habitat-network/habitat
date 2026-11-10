package mcpserver

import (
	"context"
	"errors"
	"testing"

	"github.com/habitat-network/habitat/internal/decisions"
	"github.com/stretchr/testify/require"
)

var testCandidates = []ToolCandidate{
	{Name: "github:search_issues", Description: "Search issues and pull requests"},
	{Name: "github:create_issue", Description: "Create a new issue"},
	{Name: "drive:search_files", Description: "Search files in Google Drive"},
}

func names(tools []ScoredTool) []string {
	out := make([]string, len(tools))
	for i, t := range tools {
		out[i] = t.Name
	}
	return out
}

func TestLexicalSelector(t *testing.T) {
	ctx := t.Context()

	t.Run("ranks by matched query words and drops misses", func(t *testing.T) {
		got, err := LexicalSelector{}.Select(ctx, "search github issues", testCandidates, 10)
		require.NoError(t, err)
		require.Equal(t, []string{
			// The last two tie on one word each, so they sort by name.
			"github:search_issues", "drive:search_files", "github:create_issue",
		}, names(got))
		require.InDelta(t, 1.0, got[0].Score, 1e-9)

		got, err = LexicalSelector{}.Select(ctx, "weather forecast", testCandidates, 10)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("respects the limit", func(t *testing.T) {
		got, err := LexicalSelector{}.Select(ctx, "search", testCandidates, 1)
		require.NoError(t, err)
		require.Len(t, got, 1)
	})
}

type fakeChooser struct {
	choice decisions.Choice
	err    error
	got    []decisions.Option
}

func (f *fakeChooser) Choose(
	_ context.Context, _ string, _ string, options []decisions.Option,
) (decisions.Choice, error) {
	f.got = options
	return f.choice, f.err
}

func TestDecisionSelector(t *testing.T) {
	ctx := t.Context()

	t.Run("ranks by probability and drops unlikely tools", func(t *testing.T) {
		chooser := &fakeChooser{choice: decisions.Choice{Probabilities: map[string]float64{
			"github:search_issues": 0.7, "drive:search_files": 0.25,
			"github:create_issue": 0.01, "unknown:tool": 0.9,
		}}}
		got, err := DecisionSelector{Chooser: chooser}.Select(ctx, "bugs", testCandidates, 5)
		require.NoError(t, err)
		require.Equal(t, []string{"github:search_issues", "drive:search_files"}, names(got))
		require.Len(t, chooser.got, 3)
	})

	t.Run("falls back to lexical matching on failure", func(t *testing.T) {
		chooser := &fakeChooser{err: errors.New("down")}
		got, err := DecisionSelector{Chooser: chooser}.Select(
			ctx, "create issue", testCandidates, 5,
		)
		require.NoError(t, err)
		require.Equal(t, "github:create_issue", got[0].Name)
	})

	t.Run("only asks about the best lexical matches of a long list", func(t *testing.T) {
		var many []ToolCandidate
		for range decisionShortlist + 20 {
			n := len(many)
			many = append(many, ToolCandidate{
				Name: "s:x" + string(rune('a'+n%26)) + string(rune('a'+n/26)),
			})
		}
		many = append(many, ToolCandidate{Name: "s:invoices", Description: "find invoices"})
		chooser := &fakeChooser{choice: decisions.Choice{Probabilities: map[string]float64{
			"s:invoices": 1,
		}}}
		got, err := DecisionSelector{Chooser: chooser}.Select(ctx, "invoices", many, 5)
		require.NoError(t, err)
		require.Equal(t, []string{"s:invoices"}, names(got))
		require.LessOrEqual(t, len(chooser.got), decisionShortlist)
	})
}

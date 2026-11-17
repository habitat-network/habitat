// Package mcpsearch adapts the search tools of a member's connected MCP
// servers (see internal/mcpgateway) into internal/fedsearch backends, one per
// known search tool.
//
// Every MCP server names its search tools and their parameters differently,
// and shapes its results differently, so each known search tool is adapted by
// a Provider, matched to servers by URL. A Registry holds the known
// providers; DefaultProviders seeds it with the search tools of mcpgateway's
// presets. A server no provider matches has no backend, rather than pear
// guessing which of its tools searches.
package mcpsearch

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// maxSnippetRunes caps each item's snippet, since some tools return
	// whole documents (an issue's body, say) as the text of a result.
	maxSnippetRunes = 2000
)

// Query is a search to run with one search tool.
type Query struct {
	// Text is what to search for, passed to the tool as is (adapted to its
	// query syntax by its Provider).
	Text string
	// Limit is how many items to ask the tool for, and the most kept.
	Limit int
}

// Item is one search result, as a Provider normalizes it.
type Item struct {
	Title   string
	URL     string
	Snippet string
}

// errToolFailed is returned when a search tool answers with an error result.
var errToolFailed = errors.New("search tool failed")

// callProvider calls p's tool on session and normalizes its result, keeping
// at most q.Limit items.
func callProvider(
	ctx context.Context,
	session *mcp.ClientSession,
	p Provider,
	q Query,
) ([]Item, error) {
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      p.ToolName(),
		Arguments: p.Arguments(q),
	})
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", p.ToolName(), err)
	}
	if res.IsError {
		return nil, fmt.Errorf("%w: %s", errToolFailed, truncate(resultText(res), 500))
	}
	items, err := p.Normalize(res)
	if err != nil {
		return nil, fmt.Errorf("read %s result: %w", p.ToolName(), err)
	}
	if len(items) > q.Limit {
		items = items[:q.Limit]
	}
	return items, nil
}

// truncate shortens s to at most n runes, marking the cut with an ellipsis.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

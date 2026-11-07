// Package mcpsearch runs one search query across a member's connected MCP
// servers (see internal/mcpgateway) at once, calling each server's known
// search tools in parallel and merging their results into one list of
// context items.
//
// Every MCP server names its search tools and their parameters differently,
// and shapes its results differently, so each known search tool is adapted by
// a Provider, matched to servers by URL. A Registry holds the known
// providers; DefaultProviders seeds it with the search tools of mcpgateway's
// presets. A server no provider matches is reported as unsupported, rather
// than guessing which of its tools searches.
//
// A server that fails, times out or that the member isn't connected to (for
// example because their sign-in expired) is reported in the result's Sources
// without failing the search: the items from the other servers are still
// returned.
package mcpsearch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"

	"github.com/habitat-network/habitat/internal/mcpgateway"
)

// ConnectedServers lists the MCP servers of an org that a member can reach,
// and says how to reach each on their behalf. See mcpgateway.Store for the
// concrete implementation.
type ConnectedServers interface {
	ListConnectedServers(
		ctx context.Context,
		did syntax.DID,
		orgID syntax.DID,
	) ([]*mcpgateway.Server, error)
	Downstream(
		ctx context.Context,
		did syntax.DID,
		orgID syntax.DID,
		name mcpgateway.ServerName,
	) (*mcpgateway.Downstream, error)
}

const (
	// DefaultLimit is how many items each search tool is asked for when the
	// query gives no limit.
	DefaultLimit = 10
	// MaxLimit is the largest per-tool limit a query can ask for.
	MaxLimit = 50
	// DefaultTimeout bounds each server's searches, from connecting to it to
	// its slowest search tool answering.
	DefaultTimeout = 10 * time.Second
	// maxSnippetRunes caps each item's snippet, since some tools return
	// whole documents (an issue's body, say) as the text of a result.
	maxSnippetRunes = 2000
	// maxConcurrentServers bounds how many servers one search reaches at
	// once.
	maxConcurrentServers = 8
)

var (
	// ErrEmptyQuery is returned when searching for nothing.
	ErrEmptyQuery = errors.New("query is required")
	// ErrInvalidLimit is returned for a limit outside 0 to MaxLimit.
	ErrInvalidLimit = fmt.Errorf("limit must be between 1 and %d", MaxLimit)
	// ErrInvalidServerURL is returned when a requested server URL isn't an
	// absolute http(s) URL.
	ErrInvalidServerURL = errors.New("server url must be an absolute http or https URL")
)

// Query is a search to run on every requested server.
type Query struct {
	// Text is what to search for, passed to each tool as is (adapted to its
	// query syntax by its Provider).
	Text string
	// Limit is how many items to ask each search tool for, and the most kept
	// from each. Zero means DefaultLimit.
	Limit int
}

// Item is one search result.
type Item struct {
	// Source says which server and tool the item came from. It's empty in
	// the items a Provider's Normalize returns.
	Source  ItemSource
	Title   string
	URL     string
	Snippet string
}

// ItemSource names the server and search tool an item came from.
type ItemSource struct {
	// ServerURL is the server's configured URL.
	ServerURL  string
	ServerName mcpgateway.ServerName
	Provider   ProviderID
}

// SourceStatus is the outcome of searching one server with one provider.
type SourceStatus string

const (
	// StatusOK means the tool answered; it may still have found nothing.
	StatusOK SourceStatus = "ok"
	// StatusNotConnected means the caller has no usable connection to the
	// server: they never connected, or their sign-in expired or was revoked.
	StatusNotConnected SourceStatus = "not_connected"
	// StatusUnsupported means pear knows no search tool for the server.
	StatusUnsupported SourceStatus = "unsupported"
	// StatusTimeout means the server didn't answer within the timeout.
	StatusTimeout SourceStatus = "timeout"
	// StatusError means the server or tool failed, or its result couldn't be
	// read.
	StatusError SourceStatus = "error"
)

// Source reports how searching one requested server went, per provider: a
// server with several search tools has a Source for each. A server that was
// never called (not connected, unsupported) has one Source with no Provider.
type Source struct {
	ServerURL string
	// ServerName is empty for a requested URL that isn't one of the caller's
	// connected servers.
	ServerName mcpgateway.ServerName
	Provider   ProviderID
	Status     SourceStatus
	// Error describes a failure, for any status but StatusOK.
	Error string
	// Count is how many items the provider returned.
	Count int
}

// Result is the merged result of a federated search.
type Result struct {
	// Items are grouped by source, in the order servers were requested, then
	// providers registered, each group in its tool's own ranking.
	Items []Item
	// Sources has one or more entries for every requested server, in the
	// order requested.
	Sources []Source
}

// Searcher runs federated searches.
type Searcher struct {
	servers  ConnectedServers
	registry *Registry
	timeout  time.Duration
}

// Option configures a Searcher.
type Option func(*Searcher)

// WithTimeout sets how long each server's searches may take; see
// DefaultTimeout.
func WithTimeout(timeout time.Duration) Option {
	return func(s *Searcher) { s.timeout = timeout }
}

// NewSearcher returns a Searcher reaching members' servers through servers,
// with the search tools in registry.
func NewSearcher(servers ConnectedServers, registry *Registry, opts ...Option) *Searcher {
	s := &Searcher{servers: servers, registry: registry, timeout: DefaultTimeout}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// target is one requested server, and its outcome.
type target struct {
	url     string
	server  *mcpgateway.Server
	sources []Source
	items   [][]Item // per provider
}

// Search searches the servers of org at serverURLs as caller, calling every
// known search tool of each in parallel. With no serverURLs it searches
// every server caller is connected to that has a known search tool.
//
// It fails only for an invalid query or URL, if caller's servers can't be
// listed, or if ctx is done; any one server's failure is reported in its
// Sources instead.
//
// ctx must not carry the values of an inbound MCP request; see
// mcpgateway.Downstream.Connect.
func (s *Searcher) Search(
	ctx context.Context,
	caller syntax.DID,
	org syntax.DID,
	serverURLs []string,
	q Query,
) (*Result, error) {
	q.Text = strings.TrimSpace(q.Text)
	if q.Text == "" {
		return nil, ErrEmptyQuery
	}
	if q.Limit < 0 || q.Limit > MaxLimit {
		return nil, ErrInvalidLimit
	}
	if q.Limit == 0 {
		q.Limit = DefaultLimit
	}
	requested, err := canonicalURLs(serverURLs)
	if err != nil {
		return nil, err
	}

	connected, err := s.servers.ListConnectedServers(ctx, caller, org)
	if err != nil {
		return nil, fmt.Errorf("list connected servers: %w", err)
	}
	byURL := make(map[string]*mcpgateway.Server, len(connected))
	for _, server := range connected {
		u, err := CanonicalURL(server.URL)
		if err != nil {
			continue
		}
		// Two servers of one org may share a URL; the first one (listed by
		// name) wins, so the same server isn't searched twice.
		if _, ok := byURL[u]; !ok {
			byURL[u] = server
		}
	}
	if len(serverURLs) == 0 {
		for _, server := range connected {
			u, err := CanonicalURL(server.URL)
			if err == nil && byURL[u] == server && len(s.registry.ForURL(u)) > 0 {
				requested = append(requested, u)
			}
		}
	}

	targets := make([]*target, len(requested))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentServers)
	for i, u := range requested {
		t := &target{url: u, server: byURL[u]}
		targets[i] = t
		providers := s.registry.ForURL(u)
		switch {
		case t.server == nil:
			t.sources = []Source{{
				ServerURL: u, Status: StatusNotConnected,
				Error: "not connected to an mcp server with this url",
			}}
		case len(providers) == 0:
			t.sources = []Source{{
				ServerURL: t.server.URL, ServerName: t.server.Name, Status: StatusUnsupported,
				Error: "no known search tool for this server",
			}}
		default:
			g.Go(func() error {
				s.searchServer(gctx, caller, t, providers, q)
				return nil
			})
		}
	}
	_ = g.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out := &Result{Items: []Item{}, Sources: []Source{}}
	for _, t := range targets {
		out.Sources = append(out.Sources, t.sources...)
		for _, items := range t.items {
			out.Items = append(out.Items, items...)
		}
	}
	return out, nil
}

// canonicalURLs canonicalizes urls, dropping duplicates.
func canonicalURLs(urls []string) ([]string, error) {
	seen := make(map[string]bool, len(urls))
	out := make([]string, 0, len(urls))
	for _, raw := range urls {
		u, err := CanonicalURL(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrInvalidServerURL, raw)
		}
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out, nil
}

// searchServer reaches t's server once, then calls each of providers' tools
// on it concurrently, filling in t's sources and items. Everything is bounded
// by the searcher's timeout.
func (s *Searcher) searchServer(
	ctx context.Context,
	caller syntax.DID,
	t *target,
	providers []Provider,
	q Query,
) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	t.sources = make([]Source, len(providers))
	t.items = make([][]Item, len(providers))
	for i, p := range providers {
		t.sources[i] = Source{ServerURL: t.server.URL, ServerName: t.server.Name, Provider: p.ID()}
	}
	failAll := func(err error) {
		for i := range t.sources {
			t.sources[i].Status, t.sources[i].Error = classify(ctx, err)
		}
	}

	d, err := s.servers.Downstream(ctx, caller, t.server.OrgID, t.server.Name)
	if err != nil {
		failAll(err)
		return
	}
	session, err := d.Connect(ctx)
	if err != nil {
		failAll(err)
		return
	}
	// Closing ends the session with a DELETE to the server, which the go-sdk
	// gives up to 5s, past this server's timeout if it's slow or hung. Nothing
	// here waits on the outcome, so close in the background.
	defer func() { go func() { _ = session.Close() }() }()

	var g errgroup.Group
	for i, p := range providers {
		g.Go(func() error {
			items, err := callProvider(ctx, session, p, q)
			if err != nil {
				t.sources[i].Status, t.sources[i].Error = classify(ctx, err)
				return nil
			}
			source := ItemSource{
				ServerURL:  t.server.URL,
				ServerName: t.server.Name,
				Provider:   p.ID(),
			}
			for j := range items {
				items[j].Source = source
				items[j].Snippet = truncate(items[j].Snippet, maxSnippetRunes)
			}
			t.items[i] = items
			t.sources[i].Status, t.sources[i].Count = StatusOK, len(items)
			return nil
		})
	}
	_ = g.Wait()
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

// classify maps a server's failure to its status. ctx is the server's own
// context: once it's past its deadline, any failure is a timeout, however the
// transport reported it.
func classify(ctx context.Context, err error) (SourceStatus, string) {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return StatusTimeout, "server didn't answer in time"
	case errors.Is(err, mcpgateway.ErrNotConnected):
		return StatusNotConnected, err.Error()
	default:
		return StatusError, err.Error()
	}
}

// truncate shortens s to at most n runes, marking the cut with an ellipsis.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

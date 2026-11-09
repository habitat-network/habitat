// Package mcpsearch runs one search query across several backends at once and
// merges their results into one list of items.
//
// A Backend is any source of results. The Searcher runs its backends in
// parallel; each reports its own outcome, so one failing, timing out or
// being unavailable (for example because a member's sign-in to an MCP server
// expired) is reported in the result's Sources without failing the search:
// the items from the other backends are still returned. There are two kinds:
//
//   - RecordsBackend searches the org's Habitat records (see internal/search).
//   - One serverBackend per MCP server a member has connected (see
//     internal/mcpgateway), which calls the server's known search tools in
//     parallel.
//
// Every MCP server names its search tools and their parameters differently,
// and shapes its results differently, so each known search tool is adapted by
// a Provider, matched to servers by URL. A Registry holds the known
// providers; DefaultProviders seeds it with the search tools of mcpgateway's
// presets. A server no provider matches is reported as unsupported, rather
// than guessing which of its tools searches.
package mcpsearch

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	// ErrInvalidLimit is returned for a limit outside 0 to the searcher's
	// maximum, MaxLimit by default.
	ErrInvalidLimit = errors.New("invalid limit")
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
	// Collection, if set, only matches records of this collection. Only
	// records have collections, so MCP servers are not searched.
	Collection syntax.NSID
	// SnippetLength is the approximate number of words of context in each
	// record's snippet. Zero means the records index's default.
	SnippetLength int
}

// Item is one search result.
type Item struct {
	// Source says which server and tool the item came from. It's empty in
	// the items a Provider's Normalize returns.
	Source ItemSource
	Title  string
	// URL is where to find the item: a record's URI, or a link into the
	// server it came from.
	URL     string
	Snippet string
	// Record is set on items from RecordsBackend.
	Record *Record
}

// Record is what a record item carries beyond an Item's common fields.
type Record struct {
	Space string
	CID   string
	Value any
}

// ProviderRecords is the Provider of RecordsBackend's items and sources,
// which have no server.
const ProviderRecords ProviderID = "records"

// ItemSource names the backend an item came from: for an MCP server, its
// server and search tool.
type ItemSource struct {
	// ServerURL is the server's configured URL. It's empty for records.
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

// Request is a search to run on one backend.
type Request struct {
	Caller syntax.DID
	// Org is the org searched; backends only reach what Caller may in it.
	Org   syntax.DID
	Query Query
}

// Outcome is what one backend found, and how it went.
type Outcome struct {
	// Items are in the backend's own ranking.
	Items []Item
	// Sources has one or more entries, describing how searching went.
	Sources []Source
}

// Backend is a source of search results.
//
// Search never fails the whole search: a backend that can't answer reports
// that in its Outcome's Sources. It must stop when ctx is done, and bound its
// own time otherwise.
type Backend interface {
	Search(ctx context.Context, req Request) Outcome
}

// Searcher runs federated searches.
type Searcher struct {
	servers  ConnectedServers
	registry *Registry
	timeout  time.Duration
	maxLimit int
	// backends always take part, ahead of the connected servers.
	backends []Backend
}

// Option configures a Searcher.
type Option func(*Searcher)

// WithTimeout sets how long each server's searches may take; see
// DefaultTimeout.
func WithTimeout(timeout time.Duration) Option {
	return func(s *Searcher) { s.timeout = timeout }
}

// WithBackend makes every search also run b, before the connected servers.
func WithBackend(b Backend) Option {
	return func(s *Searcher) { s.backends = append(s.backends, b) }
}

// WithMaxLimit sets the largest limit a query can ask for; see MaxLimit.
func WithMaxLimit(limit int) Option {
	return func(s *Searcher) { s.maxLimit = limit }
}

// NewSearcher returns a Searcher reaching members' servers through servers
// (which may be nil, for none), with the search tools in registry.
func NewSearcher(servers ConnectedServers, registry *Registry, opts ...Option) *Searcher {
	s := &Searcher{
		servers: servers, registry: registry, timeout: DefaultTimeout, maxLimit: MaxLimit,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Search searches every backend added with WithBackend, and the servers of
// org at serverURLs, as caller, calling every known search tool of each
// server in parallel. With no serverURLs it searches every server caller is
// connected to that has a known search tool. A query with a Collection only
// searches records.
//
// It fails only for an invalid query or URL, if caller's servers can't be
// listed, or if ctx is done; any one backend's failure is reported in its
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
	if q.Limit < 0 || q.Limit > s.maxLimit {
		return nil, fmt.Errorf("%w: must be between 1 and %d", ErrInvalidLimit, s.maxLimit)
	}
	if q.Limit == 0 {
		q.Limit = DefaultLimit
	}
	requested, err := canonicalURLs(serverURLs)
	if err != nil {
		return nil, err
	}

	backends := slices.Clone(s.backends)
	if q.Collection == "" && s.servers != nil {
		serverBackends, err := s.serverBackends(ctx, caller, org, requested, len(serverURLs) == 0)
		if err != nil {
			return nil, err
		}
		backends = append(backends, serverBackends...)
	}

	outcomes := make([]Outcome, len(backends))
	g := errgroup.Group{}
	g.SetLimit(maxConcurrentServers)
	req := Request{Caller: caller, Org: org, Query: q}
	for i, b := range backends {
		g.Go(func() error {
			outcomes[i] = b.Search(ctx, req)
			return nil
		})
	}
	_ = g.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out := &Result{Items: []Item{}, Sources: []Source{}}
	for _, o := range outcomes {
		out.Items = append(out.Items, o.Items...)
		out.Sources = append(out.Sources, o.Sources...)
	}
	return out, nil
}

// serverBackends returns a backend for each of requested, canonical server
// URLs, or, if all, for every connected server with a known search tool.
func (s *Searcher) serverBackends(
	ctx context.Context,
	caller, org syntax.DID,
	requested []string,
	all bool,
) ([]Backend, error) {
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
	if all {
		for _, server := range connected {
			u, err := CanonicalURL(server.URL)
			if err == nil && byURL[u] == server && len(s.registry.ForURL(u)) > 0 {
				requested = append(requested, u)
			}
		}
	}
	backends := make([]Backend, len(requested))
	for i, u := range requested {
		backends[i] = &serverBackend{
			searcher: s, url: u, server: byURL[u], providers: s.registry.ForURL(u),
		}
	}
	return backends, nil
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

// serverBackend is the Backend for one MCP server a caller asked to search.
// It searches with each of the server's known providers.
type serverBackend struct {
	searcher *Searcher
	// url is the canonical URL requested.
	url string
	// server is nil if the caller isn't connected to a server with url.
	server    *mcpgateway.Server
	providers []Provider
}

var _ Backend = (*serverBackend)(nil)

// Search reaches the server once, then calls each of its providers' tools
// concurrently. Everything is bounded by the searcher's timeout.
func (b *serverBackend) Search(ctx context.Context, req Request) Outcome {
	switch {
	case b.server == nil:
		return Outcome{Sources: []Source{{
			ServerURL: b.url, Status: StatusNotConnected,
			Error: "not connected to an mcp server with this url",
		}}}
	case len(b.providers) == 0:
		return Outcome{Sources: []Source{{
			ServerURL: b.server.URL, ServerName: b.server.Name, Status: StatusUnsupported,
			Error: "no known search tool for this server",
		}}}
	}
	ctx, cancel := context.WithTimeout(ctx, b.searcher.timeout)
	defer cancel()

	// A tool is asked for no more than the most any tool accepts.
	q := req.Query
	q.Limit = min(q.Limit, MaxLimit)

	sources := make([]Source, len(b.providers))
	items := make([][]Item, len(b.providers))
	for i, p := range b.providers {
		sources[i] = Source{ServerURL: b.server.URL, ServerName: b.server.Name, Provider: p.ID()}
	}
	outcome := func() Outcome {
		out := Outcome{Sources: sources}
		for _, its := range items {
			out.Items = append(out.Items, its...)
		}
		return out
	}
	failAll := func(err error) Outcome {
		for i := range sources {
			sources[i].Status, sources[i].Error = classify(ctx, err)
		}
		return outcome()
	}

	d, err := b.searcher.servers.Downstream(ctx, req.Caller, b.server.OrgID, b.server.Name)
	if err != nil {
		return failAll(err)
	}
	session, err := d.Connect(ctx)
	if err != nil {
		return failAll(err)
	}
	// Closing ends the session with a DELETE to the server, which the go-sdk
	// gives up to 5s, past this server's timeout if it's slow or hung. Nothing
	// here waits on the outcome, so close in the background.
	defer func() { go func() { _ = session.Close() }() }()

	var g errgroup.Group
	for i, p := range b.providers {
		g.Go(func() error {
			got, err := callProvider(ctx, session, p, q)
			if err != nil {
				sources[i].Status, sources[i].Error = classify(ctx, err)
				return nil
			}
			source := ItemSource{
				ServerURL:  b.server.URL,
				ServerName: b.server.Name,
				Provider:   p.ID(),
			}
			for j := range got {
				got[j].Source = source
				got[j].Snippet = truncate(got[j].Snippet, maxSnippetRunes)
			}
			items[i] = got
			sources[i].Status, sources[i].Count = StatusOK, len(got)
			return nil
		})
	}
	_ = g.Wait()
	return outcome()
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

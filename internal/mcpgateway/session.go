package mcpgateway

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

// ConnectedServers lists the MCP servers of an org that a member can reach,
// and says how to reach each on their behalf. Store is the concrete
// implementation.
type ConnectedServers interface {
	ListConnectedServers(ctx context.Context, did syntax.DID, orgID syntax.DID) ([]*Server, error)
	Downstream(
		ctx context.Context,
		did syntax.DID,
		orgID syntax.DID,
		name ServerName,
	) (*Downstream, error)
}

// DefaultServerTimeout bounds one server's share of a request that reaches
// several of a member's servers at once (see ForEach), from connecting to it
// to its last answer, so a slow or hung server can't hold up the others'.
const DefaultServerTimeout = 10 * time.Second

// maxConcurrentServers bounds how many servers ForEach reaches at once.
const maxConcurrentServers = 8

// WithSession opens an MCP client session to server on did's behalf,
// refreshing did's access token if need be, calls fn with it, and ends it.
// It's the one way pear talks to a member's connected server.
//
// fn must use the context it's given for every call on the session: it has
// ctx's deadline, cancellation and trace span but none of its other values
// (see sessionContext). The session is only good until fn returns.
//
// WithSession returns fn's error, or the reason the session couldn't be
// opened: ErrNotConnected (wrapped) if did has no usable connection to
// server.
func WithSession(
	ctx context.Context,
	servers ConnectedServers,
	did syntax.DID,
	server *Server,
	fn func(ctx context.Context, session *mcp.ClientSession) error,
) error {
	ctx = sessionContext(ctx)
	d, err := servers.Downstream(ctx, did, server.OrgID, server.Name)
	if err != nil {
		return fmt.Errorf("reach %s: %w", server.Name, err)
	}
	session, err := d.connect(ctx)
	if err != nil {
		return fmt.Errorf("reach %s: %w", server.Name, err)
	}
	// Closing ends the session with a DELETE to the server, which the go-sdk
	// gives up to 5s, however slow or hung the server is. Nothing here waits
	// on the outcome, so close in the background.
	defer func() { go func() { _ = session.Close() }() }()
	return fn(ctx, session)
}

// ForEach calls fn for each of items concurrently, at most
// maxConcurrentServers at a time, and waits for them all. Each call's context
// is done after timeout, if positive, so one item (a server, typically) can't
// hold up the rest. fn reports its own outcome, for example by writing to the
// i'th element of a slice.
func ForEach[T any](
	ctx context.Context,
	items []T,
	timeout time.Duration,
	fn func(ctx context.Context, i int, item T),
) {
	var g errgroup.Group
	g.SetLimit(maxConcurrentServers)
	for i, item := range items {
		g.Go(func() error {
			ctx := ctx
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
			fn(ctx, i, item)
			return nil
		})
	}
	_ = g.Wait()
}

// Downstream is how pear reaches a member's connected MCP server: its URL,
// the headers that authorize the member's requests to it, and the HTTP client
// to make them with (the store's, so they're restricted the same way as its
// own requests to the server). Open a session to it with WithSession.
type Downstream struct {
	URL     string
	Headers map[string]string
	Client  *http.Client
}

// connect opens an MCP client session to d over streamable HTTP, sending d's
// headers with every request. The caller closes the session.
//
// ctx must not carry the values of an inbound MCP request; see
// sessionContext.
func (d *Downstream) connect(ctx context.Context) (*mcp.ClientSession, error) {
	base := http.DefaultTransport
	if d.Client != nil && d.Client.Transport != nil {
		base = d.Client.Transport
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "habitat-pear", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   d.URL,
		HTTPClient: &http.Client{Transport: headerRoundTripper{headers: d.Headers, base: base}},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to mcp server: %w", err)
	}
	return session, nil
}

// headerRoundTripper adds static headers to every outgoing request,
// authorizing calls to a connected MCP server on the caller's behalf, then
// sends it with base.
type headerRoundTripper struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for name, value := range t.headers {
		req.Header.Set(name, value)
	}
	return t.base.RoundTrip(req)
}

// sessionContext returns a context with ctx's deadline, cancellation and
// trace span but none of its other values. ctx may belong to an inbound
// request on pear's own MCP server (internal/mcpserver), and the go-sdk keeps
// that request's negotiated MCP protocol version in its values; a client
// session to a connected server used with it would send that version instead
// of negotiating its own, which a stateful server rejects.
func sessionContext(ctx context.Context) context.Context {
	return trace.ContextWithSpan(valuelessContext{ctx}, trace.SpanFromContext(ctx))
}

// valuelessContext hides its parent's values while keeping its deadline and
// cancellation.
type valuelessContext struct{ context.Context }

func (valuelessContext) Value(any) any { return nil }

// Package pearmcp runs pear's real MCP server (internal/mcpserver, with
// internal/mcpsearch's federated search behind get_context) in this process,
// connected to mock app servers instead of an org's real ones. Nothing of
// pear is changed or stubbed out on the path under test: the mocks are
// listed as the org's connected servers at their real URLs, so pear's own
// search providers match them, and only the address pear dials is the
// mock's.
//
// There's no database, OAuth or Habitat record store behind it: one caller,
// one org, a fixed bearer token, and no Habitat records.
package pearmcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/corpus"
	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/mockmcp"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/mcpsearch"
	"github.com/habitat-network/habitat/internal/mcpserver"
	"github.com/habitat-network/habitat/internal/search"
)

const (
	// Token is the bearer token the server accepts.
	Token = "ragbench"
	// Org and Caller are the org the server serves and the member every
	// request acts as.
	Org    syntax.DID = "did:web:redwood.ragbench.test"
	Caller syntax.DID = "did:web:member.ragbench.test"
)

// Options configure Start.
type Options struct {
	// Apps are the apps to connect; empty connects every mockmcp.Apps.
	Apps []mockmcp.App
	// Mock configures the mock servers.
	Mock mockmcp.Options
	// Selector ranks tools for get_context's suggestions; nil is pear's
	// default.
	Selector mcpserver.ToolSelector
	// Addr is the address pear's MCP server listens on; empty picks a free
	// port on localhost.
	Addr string
}

// Stack is a running pear MCP server and the mock app servers behind it.
type Stack struct {
	// Endpoint is the URL of the org's MCP endpoint on pear.
	Endpoint string
	// Log records every call the mocks receive.
	Log *mockmcp.Log
	// MockEndpoints are the URLs of the mock servers themselves.
	MockEndpoints map[mockmcp.App]string
	servers       []*http.Server
}

// Start serves slice through mock app servers and pear's MCP server in front
// of them. Close it when done.
func Start(slice *corpus.Slice, opts Options) (*Stack, error) {
	if len(opts.Apps) == 0 {
		opts.Apps = mockmcp.Apps
	}
	stack := &Stack{Log: &mockmcp.Log{}, MockEndpoints: make(map[mockmcp.App]string)}
	connected := &connectedServers{dial: make(map[mcpgateway.ServerName]string)}
	for _, app := range opts.Apps {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			stack.Close()
			return nil, fmt.Errorf("listen for %s: %w", app, err)
		}
		addr := stack.serve(listener, mockmcp.New(app, slice, stack.Log, opts.Mock).Handler())
		name := mcpgateway.ServerName(app)
		stack.MockEndpoints[app] = "http://" + addr
		connected.dial[name] = "http://" + addr
		connected.servers = append(connected.servers, &mcpgateway.Server{
			OrgID: Org, Name: name, AuthType: mcpgateway.AuthTypeManual, URL: presetURL(app),
		})
	}

	serverOpts := []mcpserver.Option{mcpserver.WithFederatedSearcher(
		mcpsearch.NewSearcher(connected, mcpsearch.NewRegistry(mcpsearch.DefaultProviders()...)),
	)}
	if opts.Selector != nil {
		serverOpts = append(serverOpts, mcpserver.WithToolSelector(opts.Selector))
	}
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		stack.Close()
		return nil, fmt.Errorf("listen on %s: %w", opts.Addr, err)
	}
	origin := "http://" + listener.Addr().String()
	// No Habitat stores: with no records to find, the tools reading them
	// (get_record, get_blob) are never pointed at anything.
	pear := mcpserver.New(
		staticToken{}, nil, nil, noRecords{}, connected, nil, directory{},
		origin, origin, serverOpts...,
	)
	mux := http.NewServeMux()
	mux.Handle(mcpserver.Path+"/", pear.Handler())
	addr := stack.serve(listener, mux)
	stack.Endpoint = "http://" + addr + mcpserver.Path + "/" + Org.String()
	return stack, nil
}

// serve serves handler on listener until Close, and returns its address.
func (s *Stack) serve(listener net.Listener, handler http.Handler) string {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	s.servers = append(s.servers, server)
	go func() { _ = server.Serve(listener) }()
	return listener.Addr().String()
}

// Close stops every server.
func (s *Stack) Close() {
	for _, server := range s.servers {
		_ = server.Close()
	}
}

// Connect opens an MCP client session to pear's endpoint, as an agent would.
func (s *Stack) Connect(ctx context.Context) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "ragbench", Version: "0.1.0"}, nil)
	return client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   s.Endpoint,
		HTTPClient: &http.Client{Transport: bearer{}},
	}, nil)
}

// ServerName is the name app's server has in the org, which prefixes its
// tools in get_context's suggestions ("github:search_pull_requests").
func ServerName(app mockmcp.App) string { return string(app) }

// presetURL is the real URL of app's MCP server, which pear's search
// providers recognize it by.
func presetURL(app mockmcp.App) string {
	switch app {
	case mockmcp.AppGoogleDrive:
		return mcpgateway.PresetURL(mcpgateway.PresetGoogleDrive)
	case mockmcp.AppSlack:
		return mcpgateway.PresetURL(mcpgateway.PresetSlack)
	default:
		return mcpgateway.PresetURL(mcpgateway.PresetGitHub)
	}
}

// bearer authorizes requests with Token.
type bearer struct{}

func (bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+Token)
	return http.DefaultTransport.RoundTrip(req)
}

// connectedServers is an org whose members are all connected to the mock
// servers. Each is listed at its real server's URL and dialed at its mock's.
type connectedServers struct {
	servers []*mcpgateway.Server
	dial    map[mcpgateway.ServerName]string
}

var _ mcpgateway.ConnectedServers = (*connectedServers)(nil)

func (c *connectedServers) ListConnectedServers(
	context.Context, syntax.DID, syntax.DID,
) ([]*mcpgateway.Server, error) {
	return c.servers, nil
}

func (c *connectedServers) Downstream(
	_ context.Context, _ syntax.DID, _ syntax.DID, name mcpgateway.ServerName,
) (*mcpgateway.Downstream, error) {
	url, ok := c.dial[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", mcpgateway.ErrNotConnected, name)
	}
	return &mcpgateway.Downstream{URL: url}, nil
}

// staticToken accepts Token as Caller's.
type staticToken struct{}

var _ authn.RawMethod = staticToken{}

func (staticToken) ValidateRaw(
	_ context.Context, token string, _ ...string,
) (*authn.CredentialInfo, bool, error) {
	if token != Token {
		return nil, false, nil
	}
	return &authn.CredentialInfo{Subject: Caller}, true, nil
}

// noRecords is an org with no Habitat records, so every result comes from
// the connected apps.
type noRecords struct{}

var _ mcpserver.RecordSearcher = noRecords{}

func (noRecords) Search(context.Context, syntax.DID, search.Query) (search.Page, error) {
	return search.Page{}, nil
}

// directory resolves Org, the only identity the server looks up.
type directory struct{}

var _ identity.Directory = directory{}

var errUnknownIdentity = errors.New("unknown identity")

func (directory) LookupHandle(context.Context, syntax.Handle) (*identity.Identity, error) {
	return nil, errUnknownIdentity
}

func (directory) LookupDID(_ context.Context, did syntax.DID) (*identity.Identity, error) {
	if did != Org {
		return nil, errUnknownIdentity
	}
	return &identity.Identity{DID: did}, nil
}

func (d directory) Lookup(ctx context.Context, id syntax.AtIdentifier) (*identity.Identity, error) {
	did, err := id.AsDID()
	if err != nil {
		return nil, errUnknownIdentity
	}
	return d.LookupDID(ctx, did)
}

func (directory) Purge(context.Context, syntax.AtIdentifier) error { return nil }

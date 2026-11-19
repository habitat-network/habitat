package mcpsearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/habitat-network/habitat/internal/fedsearch"
	"github.com/habitat-network/habitat/internal/mcpgateway"
)

// BackendKind is the fedsearch kind of MCP search backends.
const BackendKind fedsearch.Kind = "mcp"

// ResultKind is the fedsearch result kind of an MCP search tool's results.
const ResultKind fedsearch.ResultKind = "item"

// ItemData is the fedsearch result data of an MCP search tool's result.
type ItemData struct {
	Server   mcpgateway.ServerName `json:"server"`
	Provider ProviderID            `json:"provider"`
}

// Resolver lists a member's MCP search backends for fedsearch: one per known
// search tool of each server they are connected to. A server with no known
// search tool has none.
type Resolver struct {
	servers  mcpgateway.ConnectedServers
	registry *Registry
}

var _ fedsearch.Resolver = (*Resolver)(nil)

// NewResolver returns a Resolver reaching members' servers through servers,
// with the search tools in registry.
func NewResolver(servers mcpgateway.ConnectedServers, registry *Registry) *Resolver {
	return &Resolver{servers: servers, registry: registry}
}

// Kind implements fedsearch.Resolver.
func (r *Resolver) Kind() fedsearch.Kind { return BackendKind }

// Backends implements fedsearch.Resolver.
func (r *Resolver) Backends(
	ctx context.Context,
	caller, org syntax.DID,
) ([]fedsearch.Backend, error) {
	connected, err := r.servers.ListConnectedServers(ctx, caller, org)
	if err != nil {
		return nil, fmt.Errorf("list connected servers: %w", err)
	}
	var out []fedsearch.Backend
	seen := map[string]bool{}
	for _, server := range connected {
		u, err := CanonicalURL(server.URL)
		// Two servers of one org may share a URL; the first one (listed by
		// name) wins, so the same server isn't searched twice.
		if err != nil || seen[u] {
			continue
		}
		seen[u] = true
		for _, p := range r.registry.ForURL(u) {
			out = append(
				out,
				&backend{servers: r.servers, caller: caller, server: server, provider: p},
			)
		}
	}
	return out, nil
}

// backend is one search tool of one of caller's connected servers.
type backend struct {
	servers  mcpgateway.ConnectedServers
	caller   syntax.DID
	server   *mcpgateway.Server
	provider Provider
}

func (b *backend) ID() fedsearch.BackendID {
	return fedsearch.BackendID(fmt.Sprintf("mcp:%s/%s", b.server.Name, b.provider.ID()))
}

func (b *backend) Kind() fedsearch.Kind { return BackendKind }

func (b *backend) Name() string {
	return fmt.Sprintf("%s %s", b.server.Name, b.provider.ToolName())
}

// Search calls the tool as the caller. A sign-in that expired or was revoked
// is fedsearch.ErrNotConnected.
func (b *backend) Search(ctx context.Context, q fedsearch.Query) ([]fedsearch.Result, error) {
	var items []Item
	err := mcpgateway.WithSession(ctx, b.servers, b.caller, b.server,
		func(ctx context.Context, session *mcp.ClientSession) error {
			var err error
			items, err = callProvider(ctx, session, b.provider, Query{Text: q.Text, Limit: q.Limit})
			return err
		})
	if errors.Is(err, mcpgateway.ErrNotConnected) {
		return nil, fmt.Errorf("%w: %w", fedsearch.ErrNotConnected, err)
	}
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(ItemData{Server: b.server.Name, Provider: b.provider.ID()})
	if err != nil {
		return nil, fmt.Errorf("encode item data: %w", err)
	}
	out := make([]fedsearch.Result, len(items))
	for i, item := range items {
		id := item.URL
		if id == "" {
			id = fmt.Sprintf("%d", i)
		}
		out[i] = fedsearch.Result{
			Kind:    ResultKind,
			ID:      id,
			Title:   item.Title,
			URL:     item.URL,
			Snippet: truncate(item.Snippet, maxSnippetRunes),
			Data:    data,
		}
	}
	return out, nil
}

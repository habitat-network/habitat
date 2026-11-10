package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/singleflight"

	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/toolsearch"
)

// ToolCatalog is a searchable catalog of the tools of the MCP servers a
// member has connected. See toolsearch.Index for the concrete implementation.
type ToolCatalog interface {
	ReplaceServer(
		ctx context.Context, org, member syntax.DID, server string, tools []toolsearch.Tool,
	) error
	PruneServers(ctx context.Context, org, member syntax.DID, keep []string) error
	Search(
		ctx context.Context, org, member syntax.DID, query string, limit int,
	) ([]toolsearch.Hit, error)
	Get(
		ctx context.Context, org, member syntax.DID, server, name string,
	) (*toolsearch.Tool, error)
}

// WithToolSearch switches connected servers' tools to progressive discovery
// (see https://modelcontextprotocol.io/docs/2026-07-28/develop/clients/client-best-practices):
// tools/list no longer includes them, which with many servers would fill the
// model's context with definitions it rarely needs. Instead the server offers
// "search_tools" to find tools by what they do in catalog, "get_tool_details"
// to read one's input schema, and "call_tool" to run it. Calling a connected
// tool by its namespaced name directly still works.
func WithToolSearch(catalog ToolCatalog) Option {
	return func(o *options) { o.toolCatalog = catalog }
}

const (
	// toolCatalogTTL is how long a member's catalog is trusted before the
	// next search lists their servers' tools again. Tool lists change on
	// deploys rather than per request, and listing means connecting to every
	// server.
	toolCatalogTTL = 10 * time.Minute
	// toolSyncTimeout bounds listing every server's tools for the catalog.
	toolSyncTimeout = 30 * time.Second
	// maxToolDescription is the longest tool description search_tools
	// returns; get_tool_details has the whole one.
	maxToolDescription = 240
)

const searchToolsDescription = "Search the tools of the apps connected to this org by what " +
	"they do, e.g. \"create a github issue\" or \"post a slack message\". Connected apps' tools " +
	"aren't listed up front: use this to find one, get_tool_details to read its input schema, " +
	"then call_tool to run it. Returns each match's name and a short description."

const getToolDetailsDescription = "Get the full definition of a connected app's tool, " +
	"including its input schema, by the name search_tools returned."

const callToolDescription = "Call a connected app's tool by the name search_tools returned, with " +
	"arguments matching the input schema from get_tool_details."

// toolCatalogKey identifies one member's catalog in one org.
type toolCatalogKey struct{ org, member syntax.DID }

// toolSyncer keeps member's catalogs filled from their servers.
type toolSyncer struct {
	catalog          ToolCatalog
	connectedServers mcpgateway.ConnectedServers

	mu     sync.Mutex
	synced map[toolCatalogKey]time.Time
	group  singleflight.Group
}

func newToolSyncer(catalog ToolCatalog, servers mcpgateway.ConnectedServers) *toolSyncer {
	return &toolSyncer{
		catalog:          catalog,
		connectedServers: servers,
		synced:           map[toolCatalogKey]time.Time{},
	}
}

// ensureFresh lists the tools of caller's connected servers in org into the
// catalog unless that was done within toolCatalogTTL. force skips that check,
// for a tool the model asked about that the catalog doesn't know. Syncs for
// the same member share one run.
func (s *toolSyncer) ensureFresh(
	ctx context.Context, org, caller syntax.DID, force bool,
) error {
	key := toolCatalogKey{org, caller}
	s.mu.Lock()
	at, ok := s.synced[key]
	s.mu.Unlock()
	if !force && ok && time.Since(at) < toolCatalogTTL {
		return nil
	}
	// A shared run must outlive the caller that started it, which others
	// may be waiting on.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), toolSyncTimeout)
	defer cancel()
	_, err, _ := s.group.Do(org.String()+"|"+caller.String(), func() (any, error) {
		if err := s.sync(runCtx, org, caller); err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.synced[key] = time.Now()
		s.mu.Unlock()
		return nil, nil
	})
	return err
}

// sync replaces the catalog's tools of each of caller's connected servers
// with what the server lists now. A server that can't be listed keeps its
// earlier tools, so an outage doesn't empty the catalog; a server caller is no
// longer connected to loses them.
func (s *toolSyncer) sync(ctx context.Context, org, caller syntax.DID) error {
	servers := listConnectedServers(ctx, s.connectedServers, caller, org)
	errs := make([]error, len(servers))
	mcpgateway.ForEach(ctx, servers, mcpgateway.DefaultServerTimeout,
		func(ctx context.Context, i int, server *mcpgateway.Server) {
			name := string(server.Name)
			tools, err := listToolsForServer(ctx, s.connectedServers, caller, server)
			if err != nil {
				slog.WarnContext(
					ctx, "mcp: listing tools for the tool catalog",
					"server", name, "err", err,
				)
				return
			}
			catalogTools := make([]toolsearch.Tool, len(tools))
			for j, tool := range tools {
				namespaced := *tool
				namespaced.Name = namespaceTool(name, tool.Name)
				def, err := json.Marshal(&namespaced)
				if err != nil {
					errs[i] = fmt.Errorf("encode tool %s:%s: %w", name, tool.Name, err)
					return
				}
				catalogTools[j] = toolsearch.Tool{
					Server: name, Name: tool.Name,
					Description: tool.Description, Definition: def,
				}
			}
			if err := s.catalog.ReplaceServer(
				ctx,
				org,
				caller,
				name,
				catalogTools,
			); err != nil {
				errs[i] = fmt.Errorf("index tools of %s: %w", name, err)
			}
		})
	if err := errors.Join(errs...); err != nil {
		return err
	}
	names := make([]string, len(servers))
	for i, server := range servers {
		names[i] = string(server.Name)
	}
	return s.catalog.PruneServers(ctx, org, caller, names)
}

type searchToolsInput struct {
	Query string `json:"query"           jsonschema:"what you want to do, in a few words, e.g. create a github issue"`
	Limit int    `json:"limit,omitempty" jsonschema:"the most tools to return; defaults to 10"`
}

type toolSummary struct {
	Name        string `json:"name"`
	Server      string `json:"server"`
	Description string `json:"description"`
}

type searchToolsOutput struct {
	Tools []toolSummary `json:"tools"`
}

// callerOf returns the authenticated caller of an MCP request.
func callerOf(ctx context.Context) (syntax.DID, error) {
	tokenInfo := auth.TokenInfoFromContext(ctx)
	if tokenInfo == nil || tokenInfo.UserID == "" {
		return "", fmt.Errorf("missing authenticated caller")
	}
	return syntax.ParseDID(tokenInfo.UserID)
}

// searchToolsHandler implements the "search_tools" MCP tool.
func searchToolsHandler(
	syncer *toolSyncer, org syntax.DID,
) mcp.ToolHandlerFor[searchToolsInput, searchToolsOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input searchToolsInput,
	) (*mcp.CallToolResult, searchToolsOutput, error) {
		caller, err := callerOf(ctx)
		if err != nil {
			return nil, searchToolsOutput{}, err
		}
		if input.Limit < 0 || input.Limit > toolsearch.MaxLimit {
			return nil, searchToolsOutput{}, fmt.Errorf(
				"limit must be between 1 and %d", toolsearch.MaxLimit,
			)
		}
		if err := syncer.ensureFresh(ctx, org, caller, false); err != nil {
			return nil, searchToolsOutput{}, fmt.Errorf("listing connected tools: %w", err)
		}
		hits, err := syncer.catalog.Search(ctx, org, caller, input.Query, input.Limit)
		if err != nil {
			return nil, searchToolsOutput{}, fmt.Errorf("searching tools: %w", err)
		}
		out := searchToolsOutput{Tools: make([]toolSummary, len(hits))}
		for i, h := range hits {
			out.Tools[i] = toolSummary{
				Name:        namespaceTool(h.Server, h.Name),
				Server:      h.Server,
				Description: truncate(h.Description, maxToolDescription),
			}
		}
		return nil, out, nil
	}
}

// truncate shortens s to at most n runes, marking where it was cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

type getToolDetailsInput struct {
	Name string `json:"name" jsonschema:"the tool's name as search_tools returned it, e.g. github:create_issue"`
}

type getToolDetailsOutput struct {
	Tool map[string]any `json:"tool" jsonschema:"the tool's MCP definition: name, description and inputSchema"`
}

// getToolDetailsHandler implements the "get_tool_details" MCP tool.
func getToolDetailsHandler(
	syncer *toolSyncer, org syntax.DID,
) mcp.ToolHandlerFor[getToolDetailsInput, getToolDetailsOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input getToolDetailsInput,
	) (*mcp.CallToolResult, getToolDetailsOutput, error) {
		caller, err := callerOf(ctx)
		if err != nil {
			return nil, getToolDetailsOutput{}, err
		}
		serverName, toolName, ok := splitNamespacedTool(input.Name)
		if !ok {
			return nil, getToolDetailsOutput{}, fmt.Errorf(
				"%q isn't a connected app's tool: names look like server%stool, as search_tools returns",
				input.Name,
				toolNamespaceSep,
			)
		}
		tool, err := syncer.catalog.Get(ctx, org, caller, serverName, toolName)
		if errors.Is(err, toolsearch.ErrNotFound) {
			// The tool may be newer than the catalog.
			if err := syncer.ensureFresh(ctx, org, caller, true); err != nil {
				return nil, getToolDetailsOutput{}, fmt.Errorf("listing connected tools: %w", err)
			}
			tool, err = syncer.catalog.Get(ctx, org, caller, serverName, toolName)
		}
		if errors.Is(err, toolsearch.ErrNotFound) {
			return nil, getToolDetailsOutput{}, fmt.Errorf(
				"no connected tool %q: use search_tools to find tool names", input.Name,
			)
		}
		if err != nil {
			return nil, getToolDetailsOutput{}, fmt.Errorf("getting tool: %w", err)
		}
		var def map[string]any
		if err := json.Unmarshal(tool.Definition, &def); err != nil {
			return nil, getToolDetailsOutput{}, fmt.Errorf("decoding tool: %w", err)
		}
		return nil, getToolDetailsOutput{Tool: def}, nil
	}
}

// callToolSchema is the input schema of "call_tool". Its arguments are
// whatever the called tool takes, so they're not constrained here.
var callToolSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name": map[string]any{
			"type":        "string",
			"description": "the tool's name as search_tools returned it, e.g. github:create_issue",
		},
		"arguments": map[string]any{
			"type":        "object",
			"description": "the tool's arguments, per its input schema from get_tool_details",
		},
	},
	"required": []string{"name"},
}

// callToolHandler implements the "call_tool" MCP tool: it runs a connected
// app's tool as the caller, like calling its namespaced name directly, for
// clients that only call tools they were listed.
func callToolHandler(
	connectedServers mcpgateway.ConnectedServers,
	org syntax.DID,
) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		caller, err := callerOf(ctx)
		if err != nil {
			return nil, err
		}
		var input struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return toolError("invalid arguments: %v", err), nil
		}
		serverName, toolName, ok := splitNamespacedTool(input.Name)
		if !ok {
			return toolError(
				"%q isn't a connected app's tool: names look like server%stool, as search_tools returns",
				input.Name,
				toolNamespaceSep,
			), nil
		}
		for _, server := range listConnectedServers(ctx, connectedServers, caller, org) {
			if string(server.Name) != serverName {
				continue
			}
			var args any
			if len(input.Arguments) > 0 {
				args = input.Arguments
			}
			res, err := callConnectedTool(ctx, connectedServers, caller, server, toolName, args)
			if err != nil {
				return nil, err
			}
			result, ok := res.(*mcp.CallToolResult)
			if !ok {
				return nil, fmt.Errorf("unexpected result from %s", server.Name)
			}
			return result, nil
		}
		return toolError(
			"no connected app %q: use search_tools to find tool names",
			serverName,
		), nil
	}
}

// toolError is a tool call result reporting a problem the model can fix by
// calling again, as the typed handlers' returned errors are.
func toolError(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}
}

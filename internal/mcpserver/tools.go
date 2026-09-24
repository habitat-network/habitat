package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"go.opentelemetry.io/otel/trace"
	"log/slog"
	"net/http"
	"strings"

	habitat_err "github.com/habitat-network/habitat/internal/error"
	"github.com/habitat-network/habitat/internal/nango"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type getRecordInput struct {
	URI string `json:"uri" jsonschema:"the space record URI of the record to fetch, e.g. at://did:plc:abc/space/network.habitat.example/3jz/did:plc:abc/network.habitat.example/3jz"`
}

type getRecordOutput struct {
	URI   string `json:"uri"`
	Value any    `json:"value"`
}

// getRecordHandler implements the "get_record" MCP tool: check the caller
// holds at least a reader role on the record's space, then read the record
// straight from the space store.
func getRecordHandler(
	store spaces.Store,
	permStore perms.Store,
) mcp.ToolHandlerFor[getRecordInput, getRecordOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input getRecordInput,
	) (*mcp.CallToolResult, getRecordOutput, error) {
		tokenInfo := auth.TokenInfoFromContext(ctx)
		if tokenInfo == nil || tokenInfo.UserID == "" {
			return nil, getRecordOutput{}, fmt.Errorf("missing authenticated caller")
		}
		caller := syntax.DID(tokenInfo.UserID)

		recordURI, err := habitat_syntax.ParseSpaceRecordURI(input.URI)
		if err != nil {
			return nil, getRecordOutput{}, fmt.Errorf("invalid uri: %w", err)
		}
		spaceURI := recordURI.SpaceURI()
		owner := recordURI.Repo()
		collection := recordURI.Collection()
		rkey := recordURI.Rkey()

		ok, err := permStore.CheckUserHasSpaceRole(
			ctx, caller, spaceURI, habitat_syntax.SpaceRoleReader,
		)
		if err != nil {
			return nil, getRecordOutput{}, fmt.Errorf("checking permission: %w", err)
		}
		if !ok {
			return nil, getRecordOutput{}, habitat_err.ErrUnauthorized
		}

		record, err := store.GetRecord(ctx, spaceURI, owner, collection, rkey)
		if err != nil {
			return nil, getRecordOutput{}, fmt.Errorf("getting record: %w", err)
		}

		return nil, getRecordOutput{URI: input.URI, Value: record.Value}, nil
	}
}

// toolNamespaceSep separates a connected server's name from its tool's own
// name in the namespaced tool name this server exposes for it, e.g.
// "cloudflare:docs". Connected servers' tool names are opaque to us, so
// every one is namespaced by its server's name to avoid collisions between
// servers (and with this server's own tools). ":" can't appear in a server
// name (see mcpgateway.validateServerName), so splitting on the first one
// is always unambiguous.
const toolNamespaceSep = ":"

func namespaceTool(serverName, toolName string) string {
	return serverName + toolNamespaceSep + toolName
}

// splitNamespacedTool reverses namespaceTool. ok is false if name doesn't
// look namespaced, meaning it's one of this server's own tools.
func splitNamespacedTool(name string) (serverName, toolName string, ok bool) {
	before, after, found := strings.Cut(name, toolNamespaceSep)
	if !found || before == "" || after == "" {
		return "", "", false
	}
	return before, after, true
}

// resolveServerName finds the org-chosen name (see internal/mcpgateway) of
// the server behind conn, by matching conn's provider_config_key against
// its org's network.habitat.mcp.server records.
func resolveServerName(
	ctx context.Context, orgRecords OrgMcpServerStore, conn nango.Connection,
) (string, error) {
	if conn.OrgID == "" {
		return "", fmt.Errorf("connection %s has no org tag", conn.ConnectionID)
	}
	servers, err := orgRecords.ListMcpServers(ctx, syntax.DID(conn.OrgID))
	if err != nil {
		return "", fmt.Errorf("listing org mcp servers: %w", err)
	}
	for _, srv := range servers {
		if srv.NangoKey == conn.ProviderConfigKey {
			return string(srv.ID), nil
		}
	}
	return "", fmt.Errorf("no mcp server record matches connection %s", conn.ConnectionID)
}

// downstream is a connected MCP server pear can open a client session to on
// the caller's behalf.
type downstream struct {
	url     string
	headers map[string]string
}

// headerRoundTripper adds static headers to every outgoing request,
// authorizing calls to a connected MCP server on the caller's behalf.
type headerRoundTripper struct {
	headers map[string]string
}

func (t headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for name, value := range t.headers {
		req.Header.Set(name, value)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// nangoDownstream fetches conn's Nango-held URL and, if required, access
// token.
func nangoDownstream(
	ctx context.Context, nangoClient NangoClient, conn nango.Connection,
) (downstream, error) {
	details, err := nangoClient.GetConnection(ctx, conn.ConnectionID, conn.ProviderConfigKey)
	if err != nil {
		return downstream{}, fmt.Errorf("get connection: %w", err)
	}
	d := downstream{url: details.MCPServerURL}
	if details.AccessToken != "" {
		d.headers = map[string]string{"Authorization": "Bearer " + details.AccessToken}
	}
	return d, nil
}

// connectToServer opens an MCP client session to d.
func connectToServer(ctx context.Context, d downstream) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "habitat-pear", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   d.url,
		HTTPClient: &http.Client{Transport: headerRoundTripper{headers: d.headers}},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to mcp server: %w", err)
	}
	return session, nil
}

// connectedServer is one of a caller's connected MCP servers, named by its
// org-chosen name. resolve looks up how to reach it, which for an OAuth
// server means a Nango call, so it's deferred until needed.
type connectedServer struct {
	name    string
	resolve func(ctx context.Context) (downstream, error)
}

// listConnectedServers lists caller's connected MCP servers (see
// internal/mcpgateway): the manual servers of every org caller belongs to,
// then caller's own Nango connections. A source or server that can't be
// listed or resolved back to a name is logged and skipped, so one broken
// server can't hide the rest.
func listConnectedServers(
	ctx context.Context,
	nangoClient NangoClient,
	orgRecords OrgMcpServerStore,
	manualServers ManualServerSource,
	caller string,
) []connectedServer {
	var out []connectedServer
	if manualServers != nil {
		manual, err := manualServers.ListManualServersForMember(ctx, syntax.DID(caller))
		if err != nil {
			slog.WarnContext(ctx, "mcp: listing manual servers", "err", err)
		}
		for _, server := range manual {
			d := downstream{url: server.URL, headers: server.Headers}
			out = append(out, connectedServer{
				name:    string(server.ID),
				resolve: func(context.Context) (downstream, error) { return d, nil },
			})
		}
	}

	connections, err := nangoClient.ListConnections(ctx, caller)
	if errors.Is(err, nango.ErrNotConfigured) {
		// Pear logs once at startup when Nango is unset.
		return out
	} else if err != nil {
		slog.WarnContext(ctx, "mcp: listing nango connections", "err", err)
		return out
	}
	for _, conn := range connections {
		name, err := resolveServerName(ctx, orgRecords, conn)
		if err != nil {
			slog.WarnContext(
				ctx, "mcp: resolving connected server name",
				"connection", conn.ConnectionID, "err", err,
			)
			continue
		}
		out = append(out, connectedServer{
			name: name,
			resolve: func(ctx context.Context) (downstream, error) {
				return nangoDownstream(ctx, nangoClient, conn)
			},
		})
	}
	return out
}

// mergeConnectedToolsMiddleware merges every tool from the caller's
// connected MCP servers into this server's own tools/list response,
// namespaced by server name. A server that can't be reached is skipped
// rather than failing the whole listing.
func mergeConnectedToolsMiddleware(
	nangoClient NangoClient,
	orgRecords OrgMcpServerStore,
	manualServers ManualServerSource,
) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			if method != "tools/list" || err != nil {
				return result, err
			}
			listResult, ok := result.(*mcp.ListToolsResult)
			if !ok {
				return result, nil
			}
			extra := req.GetExtra()
			if extra == nil || extra.TokenInfo == nil || extra.TokenInfo.UserID == "" {
				return result, nil
			}

			servers := listConnectedServers(
				ctx, nangoClient, orgRecords, manualServers, extra.TokenInfo.UserID,
			)
			for _, server := range servers {
				tools, err := listToolsForServer(ctx, server)
				if err != nil {
					slog.WarnContext(
						ctx, "mcp: listing tools for connected server",
						"server", server.name, "err", err,
					)
					continue
				}
				for _, tool := range tools {
					namespaced := *tool
					namespaced.Name = namespaceTool(server.name, tool.Name)
					listResult.Tools = append(listResult.Tools, &namespaced)
				}
			}
			return listResult, nil
		}
	}
}

// listToolsForServer connects to server and lists its tools, unnamespaced.
func listToolsForServer(ctx context.Context, server connectedServer) ([]*mcp.Tool, error) {
	ctx = downstreamContext(ctx)
	d, err := server.resolve(ctx)
	if err != nil {
		return nil, err
	}
	session, err := connectToServer(ctx, d)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()

	tools := make([]*mcp.Tool, 0)
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("list tools: %w", err)
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

// proxyConnectedToolCallMiddleware routes a tools/call for a namespaced tool
// (see namespaceTool) straight through to the caller's connected server
// that owns it. A call for a non-namespaced tool is left to this server's
// own dispatch.
func proxyConnectedToolCallMiddleware(
	nangoClient NangoClient,
	orgRecords OrgMcpServerStore,
	manualServers ManualServerSource,
) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			params, ok := req.GetParams().(*mcp.CallToolParamsRaw)
			if !ok {
				return next(ctx, method, req)
			}
			serverName, toolName, ok := splitNamespacedTool(params.Name)
			if !ok {
				return next(ctx, method, req)
			}
			extra := req.GetExtra()
			if extra == nil || extra.TokenInfo == nil || extra.TokenInfo.UserID == "" {
				return nil, fmt.Errorf("missing authenticated caller")
			}

			servers := listConnectedServers(
				ctx, nangoClient, orgRecords, manualServers, extra.TokenInfo.UserID,
			)
			for _, server := range servers {
				if server.name == serverName {
					return callConnectedTool(ctx, server, toolName, params.Arguments)
				}
			}
			// Not one of the caller's connected servers: fall through to the
			// normal dispatch, which will report the tool as not found.
			return next(ctx, method, req)
		}
	}
}

// callConnectedTool opens a session to server, calls toolName on it, and
// closes the session.
func callConnectedTool(
	ctx context.Context,
	server connectedServer,
	toolName string,
	arguments any,
) (mcp.Result, error) {
	ctx = downstreamContext(ctx)
	d, err := server.resolve(ctx)
	if err != nil {
		return nil, err
	}
	session, err := connectToServer(ctx, d)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: arguments,
	})
	if err != nil {
		// Avoid returning a typed-nil *CallToolResult as a non-nil mcp.Result.
		return nil, err
	}
	return res, nil
}

// downstreamContext returns a context with ctx's deadline, cancellation and
// trace span but none of its other values. ctx belongs to an inbound request
// on this server, and the go-sdk keeps that request's negotiated MCP protocol
// version in its values; a client session to a connected server opened with
// it would send that version instead of negotiating its own, which a
// stateful server rejects.
func downstreamContext(ctx context.Context) context.Context {
	return trace.ContextWithSpan(valuelessContext{ctx}, trace.SpanFromContext(ctx))
}

// valuelessContext hides its parent's values while keeping its deadline and
// cancellation.
type valuelessContext struct{ context.Context }

func (valuelessContext) Value(any) any { return nil }

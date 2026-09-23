package mcpserver

import (
	"context"
	"fmt"
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

// bearerRoundTripper adds a static bearer token to every outgoing request,
// authorizing calls to a connected MCP server on the caller's behalf.
type bearerRoundTripper struct {
	token string
}

func (t bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// connectToServer opens an MCP client session to the server behind conn,
// using its Nango-held URL and, if required, access token.
func connectToServer(
	ctx context.Context, nangoClient NangoClient, conn nango.Connection,
) (*mcp.ClientSession, error) {
	details, err := nangoClient.GetConnection(ctx, conn.ConnectionID, conn.ProviderConfigKey)
	if err != nil {
		return nil, fmt.Errorf("get connection: %w", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "habitat-pear", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   details.MCPServerURL,
		HTTPClient: &http.Client{Transport: bearerRoundTripper{token: details.AccessToken}},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to mcp server: %w", err)
	}
	return session, nil
}

// mergeConnectedToolsMiddleware merges every tool from the caller's
// connected MCP servers (see internal/mcpgateway) into this server's own
// tools/list response, namespaced by server name. A server that can't be
// reached, or resolved back to a name, is skipped rather than failing the
// whole listing.
func mergeConnectedToolsMiddleware(nangoClient NangoClient, orgRecords OrgMcpServerStore) mcp.Middleware {
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

			connections, err := nangoClient.ListConnections(ctx, extra.TokenInfo.UserID)
			if err != nil {
				slog.WarnContext(ctx, "mcp: listing nango connections for tools/list", "err", err)
				return result, nil
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
				tools, err := listToolsForConnection(ctx, nangoClient, conn)
				if err != nil {
					slog.WarnContext(
						ctx, "mcp: listing tools for connected server",
						"server", name, "err", err,
					)
					continue
				}
				for _, tool := range tools {
					namespaced := *tool
					namespaced.Name = namespaceTool(name, tool.Name)
					listResult.Tools = append(listResult.Tools, &namespaced)
				}
			}
			return listResult, nil
		}
	}
}

// listToolsForConnection connects to the MCP server behind conn and lists
// its tools, unnamespaced.
func listToolsForConnection(
	ctx context.Context, nangoClient NangoClient, conn nango.Connection,
) ([]*mcp.Tool, error) {
	session, err := connectToServer(ctx, nangoClient, conn)
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
// (see namespaceTool) straight through to the connected server that owns
// it, using the caller's own Nango-held credentials. A call for a
// non-namespaced tool is left to this server's own dispatch.
func proxyConnectedToolCallMiddleware(nangoClient NangoClient, orgRecords OrgMcpServerStore) mcp.Middleware {
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

			connections, err := nangoClient.ListConnections(ctx, extra.TokenInfo.UserID)
			if err != nil {
				return nil, fmt.Errorf("listing connections: %w", err)
			}
			for _, conn := range connections {
				if conn.OrgID == "" {
					continue
				}
				record, err := orgRecords.GetMcpServer(
					ctx, syntax.DID(conn.OrgID), syntax.RecordKey(serverName),
				)
				if err != nil || record.NangoKey != conn.ProviderConfigKey {
					continue
				}
				session, err := connectToServer(ctx, nangoClient, conn)
				if err != nil {
					return nil, err
				}
				defer func() { _ = session.Close() }()
				return session.CallTool(ctx, &mcp.CallToolParams{
					Name:      toolName,
					Arguments: params.Arguments,
				})
			}
			// Not one of the caller's connections: fall through to the
			// normal dispatch, which will report the tool as not found.
			return next(ctx, method, req)
		}
	}
}

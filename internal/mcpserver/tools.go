package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/ipfs/go-cid"

	habitat_err "github.com/habitat-network/habitat/internal/error"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/search"
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

// recordURIScheme is the scheme of record resource URIs, which look like
// habitat-record:///?uri=<space record uri> and are matched by
// recordResourceTemplate.
const recordURIScheme = "habitat-record"

const recordResourceTemplate = recordURIScheme + ":///{?uri}"

// recordResourceURI builds the resource URI of the record at recordURI.
func recordResourceURI(recordURI string) string {
	q := url.Values{"uri": {recordURI}}
	return (&url.URL{Scheme: recordURIScheme, Path: "/", RawQuery: q.Encode()}).String()
}

// parseRecordResourceURI is the inverse of recordResourceURI.
func parseRecordResourceURI(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != recordURIScheme {
		return "", fmt.Errorf("not a %s uri", recordURIScheme)
	}
	return u.Query().Get("uri"), nil
}

// readRecord checks the caller holds at least a reader role on the record's
// space, then reads the record straight from the space store and returns it
// as application/json resource contents with the given resource uri.
func readRecord(
	ctx context.Context,
	store spaces.Store,
	permStore perms.Store,
	caller syntax.DID,
	recordURIStr string,
	resourceURI string,
) (*mcp.ResourceContents, error) {
	recordURI, err := habitat_syntax.ParseSpaceRecordURI(recordURIStr)
	if err != nil {
		return nil, fmt.Errorf("invalid uri: %w", err)
	}
	spaceURI := recordURI.SpaceURI()

	ok, err := permStore.CheckUserHasSpaceRole(
		ctx, caller, spaceURI, habitat_syntax.SpaceRoleReader,
	)
	if err != nil {
		return nil, fmt.Errorf("checking permission: %w", err)
	}
	if !ok {
		return nil, habitat_err.ErrUnauthorized
	}

	record, err := store.GetRecord(
		ctx, spaceURI, recordURI.Repo(), recordURI.Collection(), recordURI.Rkey(),
	)
	if err != nil {
		return nil, fmt.Errorf("getting record: %w", err)
	}
	text, err := json.Marshal(record.Value)
	if err != nil {
		return nil, fmt.Errorf("encoding record: %w", err)
	}
	return &mcp.ResourceContents{
		URI: resourceURI, MIMEType: "application/json", Text: string(text),
	}, nil
}

// recordResourceHandler serves record resources (see recordResourceTemplate)
// as resources/read results, so MCP clients can read records by URI.
func recordResourceHandler(store spaces.Store, permStore perms.Store) mcp.ResourceHandler {
	return func(
		ctx context.Context, req *mcp.ReadResourceRequest,
	) (*mcp.ReadResourceResult, error) {
		tokenInfo := auth.TokenInfoFromContext(ctx)
		if tokenInfo == nil || tokenInfo.UserID == "" {
			return nil, fmt.Errorf("missing authenticated caller")
		}
		recordURI, err := parseRecordResourceURI(req.Params.URI)
		if err != nil {
			return nil, fmt.Errorf("invalid record uri: %w", err)
		}
		contents, err := readRecord(
			ctx, store, permStore, syntax.DID(tokenInfo.UserID), recordURI, req.Params.URI,
		)
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{contents}}, nil
	}
}

// getRecordHandler implements the "get_record" MCP tool: read the record as
// readRecord does, returning it both as structured output and as an embedded
// resource matching what reading the record resource returns.
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
		contents, err := readRecord(
			ctx, store, permStore, syntax.DID(tokenInfo.UserID), input.URI,
			recordResourceURI(input.URI),
		)
		if err != nil {
			return nil, getRecordOutput{}, err
		}
		var value any
		if err := json.Unmarshal([]byte(contents.Text), &value); err != nil {
			return nil, getRecordOutput{}, fmt.Errorf("decoding record: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.EmbeddedResource{Resource: contents}},
		}, getRecordOutput{URI: input.URI, Value: value}, nil
	}
}

// maxBlobContentBytes is the largest blob whose content the blob resource
// and the "get_blob" tool return; larger blobs are not served. It matches the
// upload limit so every uploaded blob can be read back.
const maxBlobContentBytes = spaces.MaxBlobBytes

// blobURIScheme is the scheme of blob resource URIs, which look like
// habitat-blob:///?space=<space uri>&cid=<cid> and are matched by
// blobResourceTemplate.
const blobURIScheme = "habitat-blob"

const blobResourceTemplate = blobURIScheme + ":///{?space,cid}"

// blobResourceURI builds the resource URI of the blob c in space.
func blobResourceURI(space habitat_syntax.SpaceURI, c cid.Cid) string {
	q := url.Values{"space": {space.String()}, "cid": {c.String()}}
	return (&url.URL{Scheme: blobURIScheme, Path: "/", RawQuery: q.Encode()}).String()
}

// parseBlobResourceURI is the inverse of blobResourceURI.
func parseBlobResourceURI(raw string) (habitat_syntax.SpaceURI, cid.Cid, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != blobURIScheme {
		return "", cid.Undef, fmt.Errorf("not a %s uri", blobURIScheme)
	}
	spaceURI, err := habitat_syntax.ParseSpaceURI(u.Query().Get("space"))
	if err != nil {
		return "", cid.Undef, fmt.Errorf("invalid space: %w", err)
	}
	c, err := cid.Parse(u.Query().Get("cid"))
	if err != nil {
		return "", cid.Undef, fmt.Errorf("invalid cid: %w", err)
	}
	return spaceURI, c, nil
}

// isTextMimeType reports whether blobs of mimeType are returned as text rather
// than binary.
func isTextMimeType(mimeType string) bool {
	mt, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return false
	}
	return strings.HasPrefix(mt, "text/") ||
		mt == "application/json" || strings.HasSuffix(mt, "+json") ||
		mt == "application/xml" || strings.HasSuffix(mt, "+xml")
}

// readBlob checks that caller holds at least a reader role on space, as the
// getBlob endpoint does, then reads the blob from blobs. A blob that is missing
// (spaces.ErrBlobNotFound) or larger than maxBlobContentBytes yields an error, and the returned
// contents carry the blob as text if it is text-like and valid UTF-8, else as
// binary.
func readBlob(
	ctx context.Context,
	blobs spaces.BlobStore,
	permStore perms.Store,
	caller syntax.DID,
	space habitat_syntax.SpaceURI,
	c cid.Cid,
	uri string,
) (*mcp.ResourceContents, error) {
	ok, err := permStore.CheckUserHasSpaceRole(ctx, caller, space, habitat_syntax.SpaceRoleReader)
	if err != nil {
		return nil, fmt.Errorf("checking permission: %w", err)
	}
	if !ok {
		return nil, habitat_err.ErrUnauthorized
	}

	mimeType, data, err := blobs.GetBlob(ctx, c)
	if errors.Is(err, spaces.ErrBlobNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("getting blob: %w", err)
	}
	if len(data) > maxBlobContentBytes {
		return nil, fmt.Errorf(
			"blob is %d bytes, larger than the %d byte limit", len(data), maxBlobContentBytes,
		)
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	contents := &mcp.ResourceContents{URI: uri, MIMEType: mimeType}
	if isTextMimeType(mimeType) && utf8.Valid(data) {
		contents.Text = string(data)
	} else {
		contents.Blob = data
	}
	return contents, nil
}

// blobResourceHandler serves blob resources (see blobResourceTemplate) as
// resources/read results, so MCP clients can read blobs by URI. The Blob of a
// binary blob is base64-encoded on the wire by the SDK.
func blobResourceHandler(blobs spaces.BlobStore, permStore perms.Store) mcp.ResourceHandler {
	return func(
		ctx context.Context, req *mcp.ReadResourceRequest,
	) (*mcp.ReadResourceResult, error) {
		tokenInfo := auth.TokenInfoFromContext(ctx)
		if tokenInfo == nil || tokenInfo.UserID == "" {
			return nil, fmt.Errorf("missing authenticated caller")
		}
		space, c, err := parseBlobResourceURI(req.Params.URI)
		if err != nil {
			return nil, fmt.Errorf("invalid blob uri: %w", err)
		}
		contents, err := readBlob(
			ctx, blobs, permStore, syntax.DID(tokenInfo.UserID), space, c, req.Params.URI,
		)
		if errors.Is(err, spaces.ErrBlobNotFound) {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{contents}}, nil
	}
}

type getBlobInput struct {
	Space string `json:"space" jsonschema:"the space URI the blob belongs to, e.g. at://did:plc:abc/space/network.habitat.example/3jz"`
	CID   string `json:"cid"   jsonschema:"the CID of the blob to fetch"`
}

// getBlobHandler implements the "get_blob" MCP tool, for clients that only
// support tools: it returns the same contents as reading the blob resource, as
// an embedded resource.
func getBlobHandler(
	blobs spaces.BlobStore,
	permStore perms.Store,
) mcp.ToolHandlerFor[getBlobInput, any] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input getBlobInput,
	) (*mcp.CallToolResult, any, error) {
		tokenInfo := auth.TokenInfoFromContext(ctx)
		if tokenInfo == nil || tokenInfo.UserID == "" {
			return nil, nil, fmt.Errorf("missing authenticated caller")
		}
		space, err := habitat_syntax.ParseSpaceURI(input.Space)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid space: %w", err)
		}
		c, err := cid.Parse(input.CID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid cid: %w", err)
		}
		contents, err := readBlob(
			ctx, blobs, permStore, syntax.DID(tokenInfo.UserID), space, c,
			blobResourceURI(space, c),
		)
		if errors.Is(err, spaces.ErrBlobNotFound) {
			return nil, nil, fmt.Errorf("blob not found")
		}
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.EmbeddedResource{Resource: contents}},
		}, nil, nil
	}
}

// maxSearchLimit is the largest page the "search_records" tool returns,
// matching the searchRecords endpoint.
const maxSearchLimit = 100

// defaultMCPSnippetLength is the snippet size in words when the caller gives
// none. It is much larger than the searchRecords endpoint's default so agents
// get substantial context around each match.
const defaultMCPSnippetLength = 300

type searchRecordsInput struct {
	Query      string `json:"query"                jsonschema:"a few keywords to search for, not a full question"`
	Collection string `json:"collection,omitempty" jsonschema:"only return records of this collection NSID"`
	Limit      int    `json:"limit,omitempty"      jsonschema:"maximum number of hits to return, up to 100"`
	// SnippetLength is a pointer so an omitted value, which takes the default,
	// differs from an explicit zero, which is rejected.
	SnippetLength *int `json:"snippet_length,omitempty" jsonschema:"approximate number of words of context in each hit's snippet, 1 to 1000; defaults to 300"`
}

type searchRecordsHit struct {
	URI     string `json:"uri"`
	Space   string `json:"space"`
	CID     string `json:"cid"`
	Value   any    `json:"value"`
	Snippet string `json:"snippet"`
}

type searchRecordsOutput struct {
	Hits []searchRecordsHit `json:"hits"`
}

// searchRecordsHandler implements the "search_records" MCP tool: run the same
// search as the searchRecords endpoint as the authenticated caller, so the
// index only matches records they may read. The search is scoped to org, the
// org the MCP endpoint was reached through.
func searchRecordsHandler(
	searcher RecordSearcher,
	org syntax.DID,
) mcp.ToolHandlerFor[searchRecordsInput, searchRecordsOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input searchRecordsInput,
	) (*mcp.CallToolResult, searchRecordsOutput, error) {
		tokenInfo := auth.TokenInfoFromContext(ctx)
		if tokenInfo == nil || tokenInfo.UserID == "" {
			return nil, searchRecordsOutput{}, fmt.Errorf("missing authenticated caller")
		}
		caller := syntax.DID(tokenInfo.UserID)

		if input.Query == "" {
			return nil, searchRecordsOutput{}, fmt.Errorf("query is required")
		}
		if input.Limit < 0 || input.Limit > maxSearchLimit {
			return nil, searchRecordsOutput{}, fmt.Errorf(
				"limit must be between 1 and %d", maxSearchLimit,
			)
		}
		snippetLength := defaultMCPSnippetLength
		if input.SnippetLength != nil {
			snippetLength = *input.SnippetLength
		}
		if snippetLength < 1 || snippetLength > search.MaxSnippetLength {
			return nil, searchRecordsOutput{}, fmt.Errorf(
				"snippet_length must be between 1 and %d", search.MaxSnippetLength,
			)
		}
		q := search.Query{
			Text: input.Query, Org: org, Limit: input.Limit, SnippetLength: snippetLength,
		}
		if input.Collection != "" {
			collection, err := syntax.ParseNSID(input.Collection)
			if err != nil {
				return nil, searchRecordsOutput{}, fmt.Errorf("invalid collection: %w", err)
			}
			q.Collections = []syntax.NSID{collection}
		}

		page, err := searcher.Search(ctx, caller, q)
		if err != nil {
			return nil, searchRecordsOutput{}, fmt.Errorf("searching records: %w", err)
		}

		out := searchRecordsOutput{Hits: make([]searchRecordsHit, len(page.Matches))}
		for i, m := range page.Matches {
			out.Hits[i] = searchRecordsHit{
				URI:     m.URI.String(),
				Space:   m.Record.Space.String(),
				CID:     m.Record.Cid.String(),
				Value:   m.Record.Value,
				Snippet: m.Snippet,
			}
		}
		return nil, out, nil
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

// listConnectedServers lists caller's connected MCP servers in org (see
// internal/mcpgateway). If they can't be listed, that's logged and none are
// returned, so pear's own tools keep working.
//
// Only org's servers are listed: the endpoint is org's, and a server's name,
// which namespaces its tools, is only unique within its org.
func listConnectedServers(
	ctx context.Context,
	servers mcpgateway.ConnectedServers,
	caller syntax.DID,
	org syntax.DID,
) []*mcpgateway.Server {
	if servers == nil {
		return nil
	}
	listed, err := servers.ListConnectedServers(ctx, caller, org)
	if err != nil {
		slog.WarnContext(ctx, "mcp: listing connected servers", "err", err)
		return nil
	}
	return listed
}

// mergeConnectedToolsMiddleware merges every tool from the caller's
// connected MCP servers in org into this server's own tools/list response,
// namespaced by server name. A server that can't be reached is skipped
// rather than failing the whole listing.
func mergeConnectedToolsMiddleware(
	connectedServers mcpgateway.ConnectedServers,
	org syntax.DID,
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

			caller := syntax.DID(extra.TokenInfo.UserID)
			servers := listConnectedServers(ctx, connectedServers, caller, org)
			for i, tools := range listToolsForServers(ctx, connectedServers, caller, servers) {
				for _, tool := range tools {
					namespaced := *tool
					namespaced.Name = namespaceTool(string(servers[i].Name), tool.Name)
					listResult.Tools = append(listResult.Tools, &namespaced)
				}
			}
			return listResult, nil
		}
	}
}

// listToolsForServers lists each of servers' tools, unnamespaced, in the
// same order as servers. Each server is a round trip or more (connecting,
// maybe refreshing a token, listing), so they're listed concurrently, and
// tools/list takes as long as the slowest rather than all of them together,
// and no longer than mcpgateway.DefaultServerTimeout. A server that can't be
// listed is logged and has no tools.
func listToolsForServers(
	ctx context.Context,
	connectedServers mcpgateway.ConnectedServers,
	caller syntax.DID,
	servers []*mcpgateway.Server,
) [][]*mcp.Tool {
	out := make([][]*mcp.Tool, len(servers))
	mcpgateway.ForEach(ctx, servers, mcpgateway.DefaultServerTimeout,
		func(ctx context.Context, i int, server *mcpgateway.Server) {
			tools, err := listToolsForServer(ctx, connectedServers, caller, server)
			if err != nil {
				slog.WarnContext(
					ctx, "mcp: listing tools for connected server",
					"server", string(server.Name), "err", err,
				)
				return
			}
			out[i] = tools
		})
	return out
}

// listToolsForServer lists server's tools, unnamespaced, as caller.
func listToolsForServer(
	ctx context.Context,
	connectedServers mcpgateway.ConnectedServers,
	caller syntax.DID,
	server *mcpgateway.Server,
) ([]*mcp.Tool, error) {
	tools := make([]*mcp.Tool, 0)
	err := mcpgateway.WithSession(ctx, connectedServers, caller, server,
		func(ctx context.Context, session *mcp.ClientSession) error {
			for tool, err := range session.Tools(ctx, nil) {
				if err != nil {
					return fmt.Errorf("list tools: %w", err)
				}
				tools = append(tools, tool)
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	return tools, nil
}

// proxyConnectedToolCallMiddleware routes a tools/call for a namespaced tool
// (see namespaceTool) straight through to the caller's connected server
// that owns it. A call for a non-namespaced tool is left to this server's
// own dispatch.
func proxyConnectedToolCallMiddleware(
	connectedServers mcpgateway.ConnectedServers,
	org syntax.DID,
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
			caller := syntax.DID(extra.TokenInfo.UserID)

			for _, server := range listConnectedServers(ctx, connectedServers, caller, org) {
				if string(server.Name) == serverName {
					return callConnectedTool(
						ctx, connectedServers, caller, server, toolName, params.Arguments,
					)
				}
			}
			// Not one of the caller's connected servers: fall through to the
			// normal dispatch, which will report the tool as not found.
			return next(ctx, method, req)
		}
	}
}

// callConnectedTool calls toolName on server as caller. It's bounded only by
// ctx, the caller's own request, since a tool may legitimately run long.
func callConnectedTool(
	ctx context.Context,
	connectedServers mcpgateway.ConnectedServers,
	caller syntax.DID,
	server *mcpgateway.Server,
	toolName string,
	arguments any,
) (mcp.Result, error) {
	var res *mcp.CallToolResult
	err := mcpgateway.WithSession(ctx, connectedServers, caller, server,
		func(ctx context.Context, session *mcp.ClientSession) (err error) {
			res, err = session.CallTool(ctx, &mcp.CallToolParams{
				Name:      toolName,
				Arguments: arguments,
			})
			return err
		})
	if err != nil {
		// Avoid returning a typed-nil *CallToolResult as a non-nil mcp.Result.
		return nil, err
	}
	return res, nil
}

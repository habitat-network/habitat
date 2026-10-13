// Package mcpserver exposes pear's data over the Model Context Protocol
// (MCP, see https://modelcontextprotocol.io), authenticated with tokens from
// internal/oauthserver's MCP-shaped endpoints (see OAuthServer.HandleMCPAuthorize
// and friends). An MCP client discovers that authorization server via RFC 9728
// protected resource metadata.
package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/habitat-network/habitat/internal/mcpgateway"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/nango"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// NangoClient is the subset of Nango's backend API needed to reach a
// caller's connected MCP servers. See internal/nango.Client for the concrete
// implementation.
type NangoClient interface {
	// ListConnections lists the MCP connections tagged with endUserID.
	ListConnections(ctx context.Context, endUserID string) ([]nango.Connection, error)
	// GetConnection fetches a Connection's live details, including
	// credentials, from Nango.
	GetConnection(
		ctx context.Context,
		connectionID, providerConfigKey string,
	) (*nango.ConnectionDetails, error)
}

// OrgMcpServerStore is the subset of opensocial.Store needed to resolve a
// Nango connection's provider_config_key back to the org-chosen server name
// (see internal/mcpgateway) that namespaces its tools.
type OrgMcpServerStore interface {
	GetMcpServer(
		ctx context.Context,
		orgDID syntax.DID,
		id syntax.RecordKey,
	) (*opensocial.McpServer, error)
	ListMcpServers(ctx context.Context, orgDID syntax.DID) ([]*opensocial.McpServer, error)
}

// RecordSearcher runs a record search as a caller. See search.Searcher for
// the concrete implementation.
type RecordSearcher interface {
	Search(ctx context.Context, caller syntax.DID, q search.Query) (search.Page, error)
}

// ManualServerSource lists the manually configured MCP servers (see
// internal/mcpgateway) available to an org member, with the URL to reach
// each.
type ManualServerSource interface {
	ListManualServersForMember(
		ctx context.Context,
		did syntax.DID,
	) ([]*mcpgateway.ManualServer, error)
}

// ProtectedResourceMetadataPath is the well-known path prefix (RFC 9728) under
// which the protected resource metadata of each org's MCP endpoint is served,
// at ProtectedResourceMetadataPath + "/" + <org DID>. Mount
// Server.ProtectedResourceMetadataHandler at that prefix.
const ProtectedResourceMetadataPath = "/.well-known/oauth-protected-resource/mcp"

// Path is the prefix under which Server.Handler should be mounted. Each org's
// MCP endpoint lives at Path + "/" + <org DID or handle>, so tools such as
// "search_records" know which org they act on.
const Path = "/mcp"

// Server exposes pear's data as an MCP server over streamable HTTP, guarded
// by bearer tokens issued by internal/oauthserver.
type Server struct {
	origin     string
	authServer string
	handler    http.Handler
}

// orgFromPath extracts the org identifier (a DID or handle) from p, the path
// of a request to an org's MCP endpoint (Path + "/" + <org DID or handle>).
func orgFromPath(p, prefix string) (syntax.AtIdentifier, error) {
	raw, ok := strings.CutPrefix(p, prefix+"/")
	if !ok {
		return "", fmt.Errorf("path %q is not under %s", p, prefix)
	}
	return syntax.ParseAtIdentifier(strings.TrimSuffix(raw, "/"))
}

// New constructs the MCP server and its authenticated streamable-HTTP
// handler.
//
//   - tokens validates bearer tokens presented to the MCP endpoint (in
//     production, *oauthserver.OAuthServer).
//   - spacesStore and permStore back record reads, offered both as a resource
//     template (resources/read) and the "get_record" tool: permStore checks
//     the caller holds at least a reader role on the record's space before
//     spacesStore returns the record.
//   - blobs and permStore back blob reads, offered both as a resource template
//     (resources/read) and the "get_blob" tool: permStore checks the caller
//     holds at least a reader role on the blob's space before blobs returns it.
//   - searcher backs the "search_records" tool, which searches as the caller
//     within the org named in the endpoint's path.
//     It may be nil when search isn't configured, in which case the tool isn't
//     offered.
//   - nangoClient and orgRecords together look up the caller's Nango
//     connections to org-configured MCP servers, and manualServers the
//     manually configured servers of the caller's orgs (see
//     internal/mcpgateway): every connected server's tools are merged into
//     this server's own tools/list response, namespaced by the server's name
//     (see namespaceTool), and tools/call for a namespaced tool is proxied
//     straight through to that server.
//   - dir resolves an org handle in the endpoint's path to its DID.
//   - origin is this server's public origin (an https URL with no path), used
//     to build the resource identifier in the protected resource metadata.
//   - authServer is the issuer of the authorization server (internal/oauthserver)
//     that issues tokens for this resource.
func New(
	tokens authn.RawMethod,
	spacesStore spaces.Store,
	permStore perms.Store,
	searcher RecordSearcher,
	nangoClient NangoClient,
	orgRecords OrgMcpServerStore,
	manualServers ManualServerSource,
	blobs spaces.BlobStore,
	dir identity.Directory,
	origin string,
	authServer string,
) *Server {
	impl := &mcp.Implementation{Name: "habitat-pear", Version: "0.1.0"}
	newMCPServer := func(org syntax.DID) *mcp.Server {
		mcpServer := mcp.NewServer(impl, nil)
		mcp.AddTool(mcpServer, &mcp.Tool{
			Name: "get_record",
			Description: "Get a single Habitat record by its space record URI, returned as " +
				"an embedded JSON resource.",
		}, getRecordHandler(spacesStore, permStore))
		mcpServer.AddResourceTemplate(&mcp.ResourceTemplate{
			Name:        "record",
			Description: "A Habitat record, addressed by its space record URI.",
			MIMEType:    "application/json",
			URITemplate: recordResourceTemplate,
		}, recordResourceHandler(spacesStore, permStore))
		mcp.AddTool(mcpServer, &mcp.Tool{
			Name: "get_blob",
			Description: "Get a blob in a Habitat space by its CID, returned as an embedded " +
				"resource: text for text-like blobs, binary otherwise.",
		}, getBlobHandler(blobs, permStore))
		mcpServer.AddResourceTemplate(&mcp.ResourceTemplate{
			Name:        "blob",
			Description: "A blob in a Habitat space, addressed by space URI and CID.",
			URITemplate: blobResourceTemplate,
		}, blobResourceHandler(blobs, permStore))
		if searcher != nil {
			mcp.AddTool(mcpServer, &mcp.Tool{
				Name: "search_records",
				Description: "Full-text search the Habitat records in this org the caller can read, " +
					"optionally within one collection.",
			}, searchRecordsHandler(searcher, org))
		}
		mcpServer.AddReceivingMiddleware(
			mergeConnectedToolsMiddleware(nangoClient, orgRecords, manualServers),
			proxyConnectedToolCallMiddleware(nangoClient, orgRecords, manualServers),
		)
		return mcpServer
	}

	streamable := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		orgID, err := orgFromPath(r.URL.Path, Path)
		if err != nil {
			return nil
		}
		// Resolved here, after the bearer check, so unauthenticated requests
		// can't trigger directory lookups.
		ident, err := dir.Lookup(r.Context(), orgID)
		if err != nil {
			slog.WarnContext(r.Context(), "mcp: resolving org", "org", orgID.String(), "err", err)
			return nil
		}
		return newMCPServer(ident.DID)
	}, &mcp.StreamableHTTPOptions{
		// pear runs behind a reverse proxy (Caddy locally, Cloud Run in prod),
		// so requests reach it from a loopback address carrying the public Host
		// header, which the SDK's DNS-rebinding guard would reject. Every
		// request is bearer-authenticated, so the guard adds nothing here.
		DisableLocalhostProtection: true,
	})

	// The resource metadata URL a 401 points clients at is per org, so the
	// bearer middleware is built per request.
	authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org, err := orgFromPath(r.URL.Path, Path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		auth.RequireBearerToken(verifyToken(tokens), &auth.RequireBearerTokenOptions{
			ResourceMetadataURL: origin + ProtectedResourceMetadataPath + "/" + org.String(),
			// ValidateRaw already enforces token expiry (via fosite's
			// IntrospectToken) before returning ok=true, so there is no separate
			// expiration for this middleware to re-check.
			AllowMissingExpiration: true,
		})(streamable).ServeHTTP(w, r)
	})

	return &Server{origin: origin, authServer: authServer, handler: authed}
}

// Handler serves the per-org MCP endpoints. Mount it at the prefix Path.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// ProtectedResourceMetadataHandler serves the RFC 9728 protected resource
// metadata document for an org's MCP endpoint. Mount it at the prefix
// ProtectedResourceMetadataPath.
func (s *Server) ProtectedResourceMetadataHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org, err := orgFromPath(r.URL.Path, ProtectedResourceMetadataPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
			Resource:             s.origin + Path + "/" + org.String(),
			AuthorizationServers: []string{s.authServer},
		}).ServeHTTP(w, r)
	})
}

// verifyToken adapts an authn.RawMethod (internal/oauthserver's bearer-token
// validation) to the go-sdk's auth.TokenVerifier shape.
func verifyToken(tokens authn.RawMethod) auth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		credInfo, ok, err := tokens.ValidateRaw(ctx, token)
		if err != nil {
			slog.WarnContext(ctx, "mcp: invalid token", "err", err)
			return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
		}
		if !ok {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: credInfo.Subject.String()}, nil
	}
}

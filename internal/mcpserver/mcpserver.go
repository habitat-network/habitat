// Package mcpserver exposes pear's data over the Model Context Protocol
// (MCP, see https://modelcontextprotocol.io), authenticated with the same
// OAuth 2.0 provider (internal/oauthserver) used by every other Habitat
// OAuth client. An MCP client discovers this server's authorization server
// via RFC 9728 protected resource metadata, registers itself with
// internal/oauthserver's RFC 7591 dynamic client registration endpoint (MCP
// clients generally can't publish an atproto Client ID Metadata Document),
// and is then routed through the exact same "type a handle, get redirected
// to your PDS" broker flow as any other Habitat OAuth client.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// ProtectedResourceMetadataPath is the well-known path (RFC 9728) advertising
// the authorization server for the resource served at Path. Mount
// Server.ProtectedResourceMetadataHandler there.
const ProtectedResourceMetadataPath = "/.well-known/oauth-protected-resource/mcp"

// Path is where Server.Handler should be mounted.
const Path = "/mcp"

// Server exposes pear's data as an MCP server over streamable HTTP, guarded
// by bearer tokens issued by internal/oauthserver.OAuthServer.
type Server struct {
	issuer  string
	handler http.Handler
}

// New constructs the MCP server and its authenticated streamable-HTTP
// handler.
//
//   - tokens validates bearer tokens presented to the MCP endpoint. In
//     production this is the same *oauthserver.OAuthServer used for every
//     other Habitat OAuth client.
//   - spacesStore and permStore back the "get_record" tool: permStore checks
//     the caller holds at least a reader role on the record's space before
//     spacesStore returns the record.
//   - issuer is this server's issuer origin (an https URL with no path),
//     used to build the resource identifier in the protected resource
//     metadata document.
func New(
	tokens authn.RawMethod,
	spacesStore spaces.Store,
	permStore perms.Store,
	issuer string,
) *Server {
	impl := &mcp.Implementation{Name: "habitat-pear", Version: "0.1.0"}
	mcpServer := mcp.NewServer(impl, nil)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "get_record",
		Description: "Get a single Habitat record by its space record URI.",
	}, getRecordHandler(spacesStore, permStore))

	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpServer
	}, &mcp.StreamableHTTPOptions{
		// Stateless: a session opened unauthenticated (to list tools) must not
		// be tied to whichever user later authenticates on it.
		Stateless: true,
		// pear runs behind a reverse proxy (Caddy locally, Cloud Run in prod),
		// so requests reach it from a loopback address carrying the public Host
		// header, which the SDK's DNS-rebinding guard would reject. Every
		// request is bearer-authenticated, so the guard adds nothing here.
		DisableLocalhostProtection: true,
	})

	authed := auth.RequireBearerToken(verifyToken(tokens), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: issuer + ProtectedResourceMetadataPath,
		// ValidateRaw already enforces token expiry (via fosite's
		// IntrospectToken) before returning ok=true, so there is no separate
		// expiration for this middleware to re-check.
		AllowMissingExpiration: true,
	})(streamable)

	return &Server{issuer: issuer, handler: allowUnauthenticatedDiscovery(streamable, authed)}
}

// Handler serves the MCP endpoint. Mount it at Path.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// ProtectedResourceMetadataHandler serves the RFC 9728 protected resource
// metadata document for the MCP endpoint. Mount it at
// ProtectedResourceMetadataPath.
func (s *Server) ProtectedResourceMetadataHandler() http.Handler {
	return auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:             s.issuer + Path,
		AuthorizationServers: []string{s.issuer},
	})
}

// unauthenticatedMethods are the JSON-RPC methods served without a token so an
// MCP client can connect and list tools before prompting the user to
// authenticate; calling a tool (or anything else) still requires a bearer token.
var unauthenticatedMethods = map[string]bool{
	"initialize":                true,
	"notifications/initialized": true,
	"ping":                      true,
	"tools/list":                true,
}

// allowUnauthenticatedDiscovery routes single JSON-RPC requests for
// unauthenticatedMethods, and any non-POST request, straight to open; everything
// else (including batches and unparseable bodies) goes to authed.
func allowUnauthenticatedDiscovery(open, authed http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			// The stateless handler answers GET/DELETE with 405 and serves no
			// data. Clients open a GET stream right after initialize, and a 401
			// there would be read as "authentication required" at connect time.
			open.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxDiscoveryBodyBytes))
		if err != nil {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var msg struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(body, &msg) == nil && unauthenticatedMethods[msg.Method] {
			open.ServeHTTP(w, r)
			return
		}
		authed.ServeHTTP(w, r)
	})
}

const maxDiscoveryBodyBytes = 1 << 20

// verifyToken adapts an authn.RawMethod (internal/oauthserver.OAuthServer's
// bearer-token validation) to the go-sdk's auth.TokenVerifier shape.
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

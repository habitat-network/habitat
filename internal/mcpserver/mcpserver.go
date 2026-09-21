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
	"context"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
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
//   - repo and perms back the "get_record" tool: perms enforces the same
//     access-control check internal/pear's own GetRecord path does before
//     repo returns the record.
//   - issuer is this server's issuer origin (an https URL with no path),
//     used to build the resource identifier in the protected resource
//     metadata document.
func New(
	tokens authn.RawMethod,
	repo repo.Repo,
	perms permissions.Store,
	issuer string,
) *Server {
	impl := &mcp.Implementation{Name: "habitat-pear", Version: "0.1.0"}
	mcpServer := mcp.NewServer(impl, nil)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "get_record",
		Description: "Get a single Habitat record by its habitat:// URI.",
	}, getRecordHandler(repo, perms))

	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpServer
	}, nil)

	authed := auth.RequireBearerToken(verifyToken(tokens), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: issuer + ProtectedResourceMetadataPath,
		// ValidateRaw already enforces token expiry (via fosite's
		// IntrospectToken) before returning ok=true, so there is no separate
		// expiration for this middleware to re-check.
		AllowMissingExpiration: true,
	})(streamable)

	return &Server{issuer: issuer, handler: authed}
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

// verifyToken adapts an authn.RawMethod (internal/oauthserver.OAuthServer's
// bearer-token validation) to the go-sdk's auth.TokenVerifier shape.
func verifyToken(tokens authn.RawMethod) auth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		credInfo, ok, err := tokens.ValidateRaw(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
		}
		if !ok {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: credInfo.Subject.String()}, nil
	}
}

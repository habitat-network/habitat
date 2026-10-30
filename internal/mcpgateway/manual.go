package mcpgateway

import (
	"errors"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// AuthType says how pear authenticates to an org's MCP server. It's stored
// as the authType of the server's network.habitat.mcp.server record.
type AuthType string

const (
	// AuthTypeOAuth servers are brokered by Nango's mcp-generic connector:
	// each member signs in to the server with their own account, following
	// the MCP authorization spec.
	AuthTypeOAuth AuthType = "oauth"
	// AuthTypeManual servers are configured once by an org admin with just a
	// URL and need no auth, so every member shares the same connection.
	AuthTypeManual AuthType = "manual"
	// AuthTypeMCPOAuth servers follow the MCP authorization spec like
	// AuthTypeOAuth, but pear runs the flow and keeps each member's
	// credentials itself (see internal/mcpauth) instead of Nango.
	AuthTypeMCPOAuth AuthType = "mcp-oauth"
)

// authTypeOf returns the AuthType a server record was written with. Records
// written before authType was recorded are all OAuth, and so is any value this
// version doesn't know.
func authTypeOf(authType string) AuthType {
	switch t := AuthType(authType); t {
	case AuthTypeManual, AuthTypeMCPOAuth:
		return t
	default:
		return AuthTypeOAuth
	}
}

// ErrInvalidAuthType is returned when adding a server with an unknown
// AuthType.
var ErrInvalidAuthType = errors.New("auth type must be oauth or manual")

// ErrUnsupportedAuthType is returned by operations that don't handle a
// server's AuthType yet. AuthTypeMCPOAuth servers can't be added or connected
// to through this package until internal/mcpauth is wired in.
var ErrUnsupportedAuthType = errors.New("operation not supported for this mcp server's auth type")

// ErrInvalidServerURL is returned when a manual server's URL isn't an
// absolute http(s) URL.
var ErrInvalidServerURL = errors.New("url must be an absolute http or https URL")

// ManualServer is an MCP server an org admin configured by hand, along with
// the URL pear's own MCP client (see internal/mcpserver) connects to it at.
type ManualServer struct {
	OrgID syntax.DID
	// ID is the server's name, chosen once at creation. It shares a
	// namespace with the org's OAuth servers (see validateServerName).
	ID          syntax.RecordKey
	Description string
	URL         string
}

func validateServerURL(serverURL string) error {
	u, err := url.Parse(serverURL)
	if err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrInvalidServerURL
	}
	return nil
}

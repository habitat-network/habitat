// Package mcpgateway lets org admins configure MCP (Model Context Protocol)
// servers for their org, and lets org members connect to those servers with
// their own accounts. pear's own MCP endpoint (internal/mcpserver) then proxies
// each member's connected servers' tools.
//
// An admin adds a server by entering its name, description, URL and AuthType.
// The server's configuration, including how pear authenticates to it, is stored
// as a row of pear's database (see serverRow), scoped to the org.
//
//   - AuthTypeOAuth: pear is an OAuth client of the server, following the MCP
//     authorization spec
//     (https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization).
//     Adding the server discovers its authorization server and registers pear
//     as a client of it, by Dynamic Client Registration unless the admin
//     supplies a pre-registered client. Each member then connects with
//     StartAuthorization, which returns the authorization URL to send their
//     browser to, and CompleteAuthorization, called from pear's OAuth callback.
//     Each member's tokens are stored in the encrypted keyval store and
//     refreshed by pear as needed.
//   - AuthTypeToken: each member supplies their own bearer token (for example
//     a personal access token) with StartAuthorization, which stores it
//     encrypted. Pear never refreshes it.
//   - AuthTypeManual: the server needs no auth, and every member is connected
//     automatically.
//
// A server's name doubles as its ID and, in internal/mcpserver, the namespace
// its tools are exposed under, so it's restricted to a limited character set
// (see validateServerName) and unique within the org across both auth types.
package mcpgateway

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// ServerName identifies an MCP server within its org. It's chosen once, when
// the server is added, and is immutable afterward.
type ServerName string

// AuthType says how pear authenticates to an org's MCP server.
type AuthType string

const (
	// AuthTypeOAuth servers are reached with each member's own OAuth tokens,
	// obtained by pear following the MCP authorization spec.
	AuthTypeOAuth AuthType = "oauth"
	// AuthTypeManual servers are configured once by an org admin with just a
	// URL and need no auth, so every member shares the same connection.
	AuthTypeManual AuthType = "manual"
	// AuthTypeToken servers are reached with a token each member supplies
	// themselves, such as a personal access token, sent as a bearer token.
	// Use it for servers without MCP OAuth support.
	AuthTypeToken AuthType = "token"
)

var (
	// ErrServerNotFound is returned when no MCP server matches the given org
	// and name.
	ErrServerNotFound = errors.New("mcp server not found")
	// ErrManualServer is returned by the per-member authorization calls on a
	// manually configured server, which has no per-member connection.
	ErrManualServer = errors.New("mcp server is configured manually and needs no sign-in")
	// ErrTokenRequired is returned when connecting to a token server without
	// a token.
	ErrTokenRequired = errors.New("this mcp server needs an access token")
	// ErrTokenNotSupported is returned when giving a token to an oauth
	// server, which authorizes through the server instead.
	ErrTokenNotSupported = errors.New("only token servers take an access token")
	// ErrNotConnected is returned when the caller has no usable connection to
	// an OAuth server: they never connected, disconnected, or their grant was
	// revoked.
	ErrNotConnected = errors.New("not connected to mcp server")
	// ErrInvalidServerName is returned when a server name doesn't match
	// serverNamePattern.
	ErrInvalidServerName = errors.New(
		"name must be 1-64 characters, using only letters, digits, hyphens, and underscores",
	)
	// ErrServerNameTaken is returned when adding a server whose name is
	// already used by another server in the same org.
	ErrServerNameTaken = errors.New("a server with this name already exists for this org")
	// ErrInvalidAuthType is returned when adding a server with an unknown
	// AuthType.
	ErrInvalidAuthType = errors.New("auth type must be oauth, token or manual")
	// ErrInvalidServerURL is returned when a server's URL isn't an absolute
	// http(s) URL.
	ErrInvalidServerURL = errors.New("url must be an absolute http or https URL")
	// ErrURLImmutable is returned when changing an OAuth server's URL: members'
	// tokens were issued for the URL it was added with.
	ErrURLImmutable = errors.New("an oauth server's url can't be changed; remove and re-add it")
	// ErrClientSecretWithoutID is returned when adding a server with a client
	// secret but no client ID.
	ErrClientSecretWithoutID = errors.New("a client secret needs a client id")
	// ErrClientNotSupported is returned when giving a client ID to a manual
	// server, which takes none.
	ErrClientNotSupported = errors.New("only oauth servers take a client id")
	// ErrOAuthDiscovery is returned when adding an OAuth server whose
	// authorization server can't be discovered, or that pear can't register
	// with.
	ErrOAuthDiscovery = errors.New("can't set up oauth with this mcp server")
	// ErrAuthorizationNotFound is returned when completing an authorization
	// whose state is unknown, already used or expired.
	ErrAuthorizationNotFound = errors.New("mcp authorization not found or expired")
	// ErrAuthorizationFailed is returned when the authorization server refused
	// an authorization, or its response couldn't be verified.
	ErrAuthorizationFailed = errors.New("mcp authorization failed")
)

// IsConfigError reports whether err is a validation error the caller should
// fix, rather than a failure of the store.
func IsConfigError(err error) bool {
	return errors.Is(err, ErrInvalidServerName) ||
		errors.Is(err, ErrServerNameTaken) ||
		errors.Is(err, ErrInvalidAuthType) ||
		errors.Is(err, ErrInvalidServerURL) ||
		errors.Is(err, ErrURLImmutable) ||
		errors.Is(err, ErrClientSecretWithoutID) ||
		errors.Is(err, ErrClientNotSupported) ||
		errors.Is(err, ErrOAuthDiscovery) ||
		errors.Is(err, ErrTokenRequired) ||
		errors.Is(err, ErrTokenNotSupported) ||
		errors.Is(err, ErrUnknownPreset) ||
		errors.Is(err, ErrPresetNotConfigured) ||
		errors.Is(err, ErrPresetConflict)
}

// serverNamePattern restricts MCP server names to a limited, predictable
// character set: they double as the server's ID and, in internal/mcpserver,
// the namespace its tools are exposed under, whose separator (":") they can't
// contain.
var serverNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func validateServerName(name string) error {
	if !serverNamePattern.MatchString(name) {
		return ErrInvalidServerName
	}
	return nil
}

// serverURLPattern matches an absolute http(s) URL with a host and no
// userinfo (credentials belong in the OAuth client or the members' tokens, not
// the URL).
var serverURLPattern = regexp.MustCompile(`^https?://[^/?#@\s]+(?:[/?#]\S*)?$`)

func validateServerURL(serverURL string) error {
	u, err := url.Parse(serverURL)
	if err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		!serverURLPattern.MatchString(serverURL) {
		return ErrInvalidServerURL
	}
	return nil
}

// Server is an org's MCP server of either AuthType, as exposed through the
// API. It never carries credentials.
type Server struct {
	OrgID       syntax.DID
	Name        ServerName
	Description string
	AuthType    AuthType
	// URL is the server's streamable HTTP endpoint.
	URL string
}

// ServerWithStatus is an org's MCP server, along with whether a particular
// member has connected to it. Manual servers are always connected.
type ServerWithStatus struct {
	Server    *Server
	Connected bool
}

// NewServer is an MCP server to add to an org.
type NewServer struct {
	Name        string
	Description string
	// URL is the server's streamable HTTP endpoint.
	URL      string
	AuthType AuthType
	// Preset adds a known connector instead: its URL, auth type and OAuth
	// client are filled in, so URL, AuthType, ClientID and ClientSecret must
	// be empty.
	Preset Preset
	// ClientID and ClientSecret are an OAuth client pre-registered with the
	// server's authorization server, for OAuth servers that don't support
	// Dynamic Client Registration. Empty registers one dynamically. The
	// secret is optional, for public clients, and is stored encrypted.
	ClientID     string
	ClientSecret string
}

// ServerUpdate is a partial update to an org's MCP server. Nil fields are
// left unchanged. URL applies only to manual servers.
type ServerUpdate struct {
	Description *string
	URL         *string
}

// Downstream is how pear reaches a member's connected MCP server: its URL,
// the headers that authorize the member's requests to it, and the HTTP client
// to make them with (the store's, so they're restricted the same way as its
// own requests to the server).
type Downstream struct {
	URL     string
	Headers map[string]string
	Client  *http.Client
}

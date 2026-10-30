// Package mcpauth runs MCP authorization
// (https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)
// inside pear, for org MCP servers whose authType is mcp-oauth (see
// internal/mcpgateway). It replaces what Nango's mcp-generic connector did for
// oauth servers: discovering a server's authorization server, registering
// pear as a client with it, running the authorization code flow with PKCE for
// each member, and keeping each member's tokens fresh.
//
// Everything it keeps lives in the keyval store under one namespace, in rows
// told apart by a key prefix: member credentials and cached access tokens,
// registered clients, pending authorization requests (all encrypted), and the
// locks that keep two pear instances from refreshing the same token at once.
// Credentials are bound to the server URL they were issued for, so a server
// removed and re-added under the same name at a different URL never receives
// the old server's tokens.
package mcpauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/sync/singleflight"

	"github.com/habitat-network/habitat/internal/keyval"
)

// ErrNotConnected is returned when a member has no credentials for a server.
var ErrNotConnected = errors.New("mcpauth: not connected to mcp server")

// ErrNeedsReauth is returned when a member's credentials for a server can no
// longer be refreshed, because the authorization server revoked or expired
// them or never issued a refresh token. The member has to connect again.
var ErrNeedsReauth = errors.New("mcpauth: mcp server authorization expired; connect again")

// ErrFlowNotFound is returned by Complete when the callback's state doesn't
// match a pending authorization request, or the request has expired or was
// already completed.
var ErrFlowNotFound = errors.New("mcpauth: authorization request not found or expired")

// ErrAuthorizationDenied is returned by Complete when the authorization server
// redirected back with an error instead of a code, e.g. because the member
// declined.
var ErrAuthorizationDenied = errors.New("mcpauth: authorization was not granted")

// ErrIssuerMismatch is returned by Complete when the callback's iss parameter
// (RFC 9207) doesn't match the authorization server the request was sent to,
// or is missing when that server promised to send it.
var ErrIssuerMismatch = errors.New("mcpauth: authorization response issuer mismatch")

// ErrClientRegistrationUnsupported is returned when an authorization server
// supports neither client ID metadata documents (or pear has none configured)
// nor dynamic client registration, so pear can't become its client.
var ErrClientRegistrationUnsupported = errors.New(
	"mcpauth: authorization server supports no client registration method pear can use",
)

// ErrRefreshInProgress is returned when another pear instance holds the
// refresh lock for a credential for longer than AccessToken is willing to
// wait.
var ErrRefreshInProgress = errors.New("mcpauth: timed out waiting for another token refresh")

// Server identifies an org MCP server and where it's reached.
type Server struct {
	Org syntax.DID
	ID  syntax.RecordKey
	// URL is the server's streamable HTTP endpoint, as on its record.
	// Credentials are only ever used with the URL they were issued for.
	URL string
}

// Status is a member's connection state with a server.
type Status string

const (
	// StatusNotConnected means the member has never connected, disconnected,
	// or connected to a server previously at a different URL.
	StatusNotConnected Status = "not_connected"
	// StatusConnected means the member has credentials pear can use.
	StatusConnected Status = "connected"
	// StatusNeedsReauth means the member's credentials can no longer be
	// refreshed and they need to connect again.
	StatusNeedsReauth Status = "needs_reauth"
)

// Callback is what an authorization server sent back to pear's redirect URL.
type Callback struct {
	State string
	Code  string
	// Iss is the RFC 9207 iss parameter, if the server sent one.
	Iss string
	// Error and ErrorDescription are set instead of Code when authorization
	// failed or the member declined.
	Error            string
	ErrorDescription string
}

// Connection is the member and server a completed authorization connected.
type Connection struct {
	Server Server
	Member syntax.DID
}

// Authorizer connects members to MCP servers and reports on those
// connections.
type Authorizer interface {
	// Begin starts connecting member to server, returning the authorization
	// server URL to send the member's browser to. The authorization server
	// redirects back to the configured RedirectURL, whose handler passes what
	// it received to Complete.
	Begin(ctx context.Context, server Server, member syntax.DID) (string, error)
	// Complete finishes an authorization Begin started, exchanging the code
	// for tokens and storing them. Each request can be completed once.
	Complete(ctx context.Context, callback Callback) (*Connection, error)
	// Status reports member's connection state with server.
	Status(ctx context.Context, server Server, member syntax.DID) (Status, error)
	// Disconnect deletes member's credentials for server. It returns
	// ErrNotConnected if there are none.
	Disconnect(ctx context.Context, server Server, member syntax.DID) error
}

// TokenSource hands out access tokens for calling MCP servers as a member.
type TokenSource interface {
	// AccessToken returns a live access token for member at server,
	// refreshing it first if the cached one has expired. It returns
	// ErrNotConnected or ErrNeedsReauth when there's nothing to refresh with.
	AccessToken(ctx context.Context, server Server, member syntax.DID) (string, error)
	// Invalidate drops member's cached access token for server, so the next
	// AccessToken refreshes. Call it when the server rejects a token with
	// 401, then retry once.
	Invalidate(ctx context.Context, server Server, member syntax.DID) error
}

// Service is both halves: pear's API uses the Authorizer, and its MCP client
// the TokenSource.
type Service interface {
	Authorizer
	TokenSource
}

// Config is how pear presents itself to MCP authorization servers.
type Config struct {
	// RedirectURL is pear's OAuth callback, registered with every
	// authorization server. Required.
	RedirectURL string
	// ClientName is shown to members on authorization servers' consent
	// screens.
	ClientName string
	// ClientIDMetadataURL, if set, is the HTTPS URL of a client ID metadata
	// document describing pear, used as its client_id with authorization
	// servers that support them instead of registering dynamically.
	ClientIDMetadataURL string
}

type service struct {
	secrets    keyval.EncryptedStore
	plain      keyval.Store
	discoverer Discoverer
	httpClient *http.Client
	cfg        Config
	refreshes  singleflight.Group
}

var _ Service = (*service)(nil)

// New constructs a Service.
//
//   - secrets and plain must be bound to the same keyval namespace (e.g.
//     "mcp_oauth"): secrets stores everything, and plain deletes rows and
//     takes refresh locks.
//   - discoverer finds a server's authorization server (see NewDiscoverer).
//   - httpClient makes every other outbound request: client registration and
//     token requests.
//
// TODO: pass an SSRF-guarded httpClient from pear once one exists. Discovery
// already refuses non-public addresses (see NewDiscoverer), but registration
// and token endpoints come from metadata a server controls.
func New(
	secrets keyval.EncryptedStore,
	plain keyval.Store,
	discoverer Discoverer,
	httpClient *http.Client,
	cfg Config,
) (Service, error) {
	if secrets == nil || plain == nil {
		return nil, fmt.Errorf("mcpauth: keyval stores are required")
	}
	if discoverer == nil {
		return nil, fmt.Errorf("mcpauth: discoverer is required")
	}
	if u, err := url.Parse(cfg.RedirectURL); err != nil || !u.IsAbs() {
		return nil, fmt.Errorf("mcpauth: redirect url must be an absolute url")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &service{
		secrets:    secrets,
		plain:      plain,
		discoverer: discoverer,
		httpClient: httpClient,
		cfg:        cfg,
	}, nil
}

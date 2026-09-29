package oauthserver

import (
	"context"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// This file exposes oauthserver's unexported helpers to its external tests. It
// is compiled only into the test binary, so none of it is part of the package's
// API.

var (
	// NewStore is the unexported fosite store constructor.
	NewStore = newStore
	// HasJSONBody reports whether a request carries a JSON body.
	HasJSONBody = hasJSONBody
)

type (
	// ParRequestBody is the request body a JSON token request decodes into.
	ParRequestBody = parRequestBody
	// TokenRequestBody is the JSON form of a token request.
	TokenRequestBody = tokenRequestBody
)

// The FormValues methods expose the bodies' form decoding, which the tests
// assert on.
func (b parRequestBody) FormValues() url.Values   { return b.formValues() }
func (b tokenRequestBody) FormValues() url.Values { return b.formValues() }

// NewLoginHintServer returns an OAuthServer wired only with what
// resolveLoginHint reads, so the tests can exercise it without a full server.
func NewLoginHintServer(
	directory identity.Directory,
	emailResolver EmailIdentityResolver,
) *OAuthServer {
	return &OAuthServer{directory: directory, emailResolver: emailResolver}
}

// ResolveLoginHint calls the server's unexported login-hint resolver.
func (s *OAuthServer) ResolveLoginHint(ctx context.Context, hint string) (syntax.DID, error) {
	return s.resolveLoginHint(ctx, hint)
}

// SetEmailResolver replaces the server's email resolver, which a test clears to
// check the behaviour with none configured.
func (s *OAuthServer) SetEmailResolver(r EmailIdentityResolver) { s.emailResolver = r }

// CreateRegisteredClient calls the store's unexported client registrar, which
// the tests use to seed a client without going through the metadata endpoint.
func (s *OAuthServer) CreateRegisteredClient(ctx context.Context, c *RegisteredClient) error {
	return s.storage.createRegisteredClient(ctx, c)
}

// NormalizeLoopbackRedirect calls the server's loopback-redirect rewrite, which
// the tests exercise directly.
func (s *OAuthServer) NormalizeLoopbackRedirect(ctx context.Context, form url.Values) {
	s.normalizeLoopbackRedirect(ctx, form)
}

// ValidateRedirectURI calls the server's redirect-URI check, which the tests
// exercise directly.
var ValidateRedirectURI = validateRedirectURI

// BuildAuthServerMetadata calls the server's metadata builder, which the tests
// exercise directly.
var BuildAuthServerMetadata = buildAuthServerMetadata

// BuildProtectedResourceMetadata calls the server's resource-metadata builder,
// which the tests exercise directly.
var BuildProtectedResourceMetadata = buildProtectedResourceMetadata

type (
	// TestPermission is the parsed scope shape the MCP permission tests assert on.
	TestPermission = permission
)

// PermissionFromScope calls the scope parser, which the tests exercise directly.
var PermissionFromScope = permissionFromScope

// TestScopeAction is the action element of a parsed scope.
type TestScopeAction = scopeAction

// ScopeMatch calls the granted-vs-required scope check, which the tests
// exercise directly.
var ScopeMatch = scopeMatch

// ScopeStrategy calls the granted-strategy check, which the tests exercise directly.
var ScopeStrategy = scopeStrategy

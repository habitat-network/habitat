package login

import (
	"golang.org/x/oauth2"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// This file exposes login's unexported types and fields to login's external
// tests, which point the Google provider at a stub token server. It is compiled
// only into the test binary, so none of it is part of the package's API; the
// production types stay unexported.

type (
	// GoogleIDTokenClaims is the Google ID token's claim set, which the tests
	// sign into a token to hand the provider.
	GoogleIDTokenClaims = googleIDTokenClaims
	// PDSProviderState is the opaque flash state for the PDS login flow.
	PDSProviderState = pdsProviderState
	// GoogleProviderState is the opaque state for the Google login flow.
	GoogleProviderState = googleProviderState
	// GoogleProvider is the concrete type NewGoogleProvider returns.
	GoogleProvider = googleProvider
)

// OAuthConfig returns the provider's OAuth config, so a test can repoint its
// token URL at a stub server.
func (p *googleProvider) OAuthConfig() *oauth2.Config { return p.oauthCfg }

// VerifyGoogleIDToken exposes the ID token parser, so the tests can assert on
// the claims it rejects.
var VerifyGoogleIDToken = verifyGoogleIDToken

// IssueToken mints a signed identity token, so the tests can assert on the
// provider's token handling without a live PDS.
func IssueToken(p *PasswordLoginProvider, did syntax.DID) (string, error) {
	return p.issueToken(did)
}

// ErrInvalidLoginToken is returned when a login token is malformed or expired.
var ErrInvalidLoginToken = errInvalidLoginToken

// SigningSecret returns the provider's HMAC signing key, which a test needs to
// forge an already-expired token.
func SigningSecret(p *PasswordLoginProvider) []byte { return p.signingSecret }

// HashPassword exposes the provider's password hashing, so the tests can build
// the stored form the provider compares against.
var HashPassword = hashPassword

// VerifyPassword exposes the provider's password check, so the tests can
// exercise it directly.
var VerifyPassword = verifyPassword

package mcpauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/keyval"
)

// Every row lives in one keyval namespace, told apart by these key prefixes.
const (
	credentialPrefix = "cred|"
	tokenPrefix      = "token|"
	clientPrefix     = "client|"
	flowPrefix       = "flow|"
	lockPrefix       = "lock|"
)

const (
	// flowTTL is how long a member has to finish authorizing.
	flowTTL = 10 * time.Minute
	// lockTTL bounds how long a crashed refresh can hold its lock. It's well
	// over the time a token request takes.
	lockTTL = 30 * time.Second
	// tokenExpirySkew is taken off every access token's lifetime when it's
	// cached, so a token is never handed out moments before it expires.
	tokenExpirySkew = 60 * time.Second
	// unknownTokenTTL is how long to cache an access token whose response
	// didn't say when it expires.
	unknownTokenTTL = 5 * time.Minute
)

// connectionKey identifies member's connection to server.
func connectionKey(server Server, member syntax.DID) string {
	return server.Org.String() + "|" + string(server.ID) + "|" + member.String()
}

// clientKey identifies pear's registration with an authorization server.
func clientKey(issuer, redirectURL string) string {
	return clientPrefix + issuer + "|" + redirectURL
}

// endpoints is what pear needs from an authorization server's metadata once
// it has been discovered.
type endpoints struct {
	Issuer           string
	AuthorizationURL string
	TokenURL         string
	// IssParameterSupported is whether the server promised to send iss in
	// authorization responses (RFC 9207).
	IssParameterSupported bool
}

// registeredClient is pear's client registration with an authorization
// server.
type registeredClient struct {
	ClientID     string
	ClientSecret string
	// AuthMethod is the token_endpoint_auth_method: none,
	// client_secret_basic or client_secret_post.
	AuthMethod string
}

// credential is what's kept per connection. It outlives every access token.
type credential struct {
	// ServerURL is the server URL the credential was issued for.
	ServerURL string
	// Resource is the RFC 8707 resource indicator sent with every token
	// request.
	Resource     string
	Endpoints    endpoints
	Client       registeredClient
	RefreshToken string
	Scopes       []string
	// NeedsReauth is set, and RefreshToken cleared, once the authorization
	// server rejects the refresh token.
	NeedsReauth bool
}

// cachedToken is a connection's current access token, stored with a TTL of
// its remaining lifetime less tokenExpirySkew.
type cachedToken struct {
	ServerURL   string
	AccessToken string
}

// pendingFlow is an authorization Begin started and Complete finishes, keyed
// by its state parameter.
type pendingFlow struct {
	Server       Server
	Member       syntax.DID
	CodeVerifier string
	Resource     string
	Endpoints    endpoints
	Client       registeredClient
	Scopes       []string
}

// loadCredential returns member's credential for server, or ErrNotConnected
// if there's none for server's current URL.
func (s *service) loadCredential(
	ctx context.Context,
	server Server,
	member syntax.DID,
) (*credential, error) {
	var cred credential
	err := s.secrets.GetEncrypted(ctx, credentialPrefix+connectionKey(server, member), &cred)
	if errors.Is(err, keyval.ErrNotFound) {
		return nil, ErrNotConnected
	} else if err != nil {
		return nil, fmt.Errorf("load mcp credential: %w", err)
	}
	if cred.ServerURL != server.URL {
		return nil, ErrNotConnected
	}
	return &cred, nil
}

// cachedAccessToken returns the cached access token for key, if there's a
// live one for serverURL.
func (s *service) cachedAccessToken(
	ctx context.Context,
	key, serverURL string,
) (string, bool, error) {
	var tok cachedToken
	err := s.secrets.GetEncrypted(ctx, tokenPrefix+key, &tok)
	if errors.Is(err, keyval.ErrNotFound) {
		return "", false, nil
	} else if err != nil {
		return "", false, fmt.Errorf("load mcp access token: %w", err)
	}
	if tok.ServerURL != serverURL {
		return "", false, nil
	}
	return tok.AccessToken, true, nil
}

// save stores cred and the access token resp carries for key, in that order:
// a rotated refresh token is persisted before its access token is used, so a
// failure can't leave pear holding tokens it has no way to refresh.
func (s *service) save(
	ctx context.Context,
	key string,
	cred *credential,
	resp *tokenResponse,
) (string, error) {
	if err := s.secrets.PutEncrypted(ctx, credentialPrefix+key, cred, keyval.NoExpiry); err != nil {
		return "", fmt.Errorf("store mcp credential: %w", err)
	}
	ttl := unknownTokenTTL
	if resp.ExpiresIn > 0 {
		ttl = time.Duration(resp.ExpiresIn)*time.Second - tokenExpirySkew
	}
	if ttl > 0 {
		if err := s.secrets.PutEncrypted(ctx, tokenPrefix+key, cachedToken{
			ServerURL:   cred.ServerURL,
			AccessToken: resp.AccessToken,
		}, ttl); err != nil {
			return "", fmt.Errorf("store mcp access token: %w", err)
		}
	}
	return resp.AccessToken, nil
}

// deleteIfPresent deletes key, treating an absent key as already deleted.
func (s *service) deleteIfPresent(ctx context.Context, key string) error {
	if err := s.plain.Delete(ctx, key); err != nil && !errors.Is(err, keyval.ErrNotFound) {
		return err
	}
	return nil
}

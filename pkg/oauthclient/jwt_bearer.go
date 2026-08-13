package oauthclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-querystring/query"
	"github.com/habitat-network/habitat/internal/utils"
)

// JWTBearerGrantType is the RFC 7523 JWT Bearer grant type identifier.
const JWTBearerGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"

// Client wraps an indigo oauth.ClientApp, adding support for the RFC 7523
// JWT Bearer grant so that external services can authenticate directly to a
// Habitat pear instance without the browser-based authorization-code flow.
type Client struct {
	*oauth.ClientApp
}

// NewJWTBearerClient wraps app with JWT Bearer grant support.
func NewJWTBearerClient(app *oauth.ClientApp) *Client {
	return &Client{ClientApp: app}
}

// jwtBearerTokenRequest is the RFC 7523 token request body. These HTTP POST
// bodies are form-encoded, so use URL encoding syntax, not JSON.
type jwtBearerTokenRequest struct {
	GrantType string `url:"grant_type"`
	Assertion string `url:"assertion"`
}

// SendJWTTokenRequest implements the client side of the RFC 7523 JWT Bearer
// grant: it signs an assertion (iss=client ID, sub=subject, aud=audience)
// with the client's configured assertion key, exchanges it for an access
// token at tokenURL, and persists and returns the resulting session the same
// way [oauth.ClientApp.ProcessCallback] does.
func (c *Client) SendJWTTokenRequest(
	ctx context.Context,
	identifier string,
) (*oauth.ClientSessionData, error) {
	if !c.Config.IsConfidential() {
		return nil, fmt.Errorf("jwt bearer grant requires a confidential client")
	}
	atid, err := syntax.ParseAtIdentifier(identifier)
	if err != nil {
		return nil, fmt.Errorf("not a valid account identifier (%s): %w", identifier, err)
	}
	ident, err := c.Dir.Lookup(ctx, atid)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve username (%s): %w", identifier, err)
	}
	host := ident.PDSEndpoint()
	if host == "" {
		return nil, fmt.Errorf("identity does not link to an atproto host (PDS)")
	}

	// TODO: logger on ClientApp?
	logger := slog.Default().With("did", ident.DID, "handle", ident.Handle, "host", host)
	logger.DebugContext(ctx, "resolving to auth server metadata")
	authserverURL, err := c.Resolver.ResolveAuthServerURL(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolving auth server: %w", err)
	}
	authserverMeta, err := c.Resolver.ResolveAuthServerMetadata(ctx, authserverURL)
	if err != nil {
		return nil, fmt.Errorf("fetching auth server metadata: %w", err)
	}

	// The audience is the bare issuer origin, matching how the server
	// validates the assertion's "aud" claim (see internal/oauthserver's
	// fosite Config.TokenURL) and how indigo's own client assertions sign
	// theirs (ClientConfig.NewClientAssertion(authMeta.Issuer)) — not the
	// full token endpoint URL.
	assertion, err := c.signJWTBearerAssertion(ident.DID, authserverMeta.Issuer)
	if err != nil {
		return nil, fmt.Errorf("signing jwt bearer assertion: %w", err)
	}

	vals, err := query.Values(jwtBearerTokenRequest{
		GrantType: JWTBearerGrantType,
		Assertion: assertion,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding jwt bearer token request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		authserverMeta.TokenEndpoint,
		strings.NewReader(vals.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("creating jwt bearer token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf(
			"jwt bearer token request failed (HTTP %d): %v",
			resp.StatusCode,
			errResp["error"],
		)
	}

	var tokenResp oauth.TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("token response failed to decode: %w", err)
	}

	sessData := oauth.ClientSessionData{
		AccountDID: ident.DID,
		SessionID:  utils.RandomNonce(16),

		HostURL:                      ident.PDSEndpoint(),
		AuthServerURL:                authserverURL,
		AuthServerTokenEndpoint:      authserverMeta.TokenEndpoint,
		AuthServerRevocationEndpoint: authserverMeta.RevocationEndpoint,
		Scopes:                       strings.Split(tokenResp.Scope, " "),
		AccessToken:                  tokenResp.AccessToken,
		RefreshToken:                 tokenResp.RefreshToken,
	}
	if err := c.Store.SaveSession(ctx, sessData); err != nil {
		return nil, err
	}
	return &sessData, nil
}

// signJWTBearerAssertion signs an RFC 7523 JWT bearer assertion with the
// client's configured assertion key. jwt.GetSigningMethod("ES256") returns
// indigo's atproto-aware ES256 signing method (registered by the imported
// oauth package), which can sign with an atcrypto.PrivateKey.
func (c *Client) signJWTBearerAssertion(subject syntax.DID, audience string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": c.Config.ClientID,
		"sub": subject.String(),
		"aud": audience,
		"iat": now.Unix(),
		"exp": now.Add(time.Minute).Unix(),
		"jti": utils.RandomNonce(16),
	}
	token := jwt.NewWithClaims(jwt.GetSigningMethod("ES256"), claims)
	token.Header["kid"] = *c.Config.KeyID
	return token.SignedString(c.Config.PrivateKey)
}

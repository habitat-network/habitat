package mcpgateway

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

// This file is pear's side of the MCP authorization spec
// (https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization),
// as an OAuth client of an org's MCP server. It follows the same steps as the
// go-sdk's auth.AuthorizationCodeHandler, which can't be used directly: that
// runs the whole flow inside one call, while pear's spans a member's browser
// redirect, and it keeps tokens in memory, while pear's live in keyval.

// clientName is the client_name pear registers with MCP servers'
// authorization servers, which they typically show on their consent screen.
const clientName = "Habitat"

// maxTokenResponseBytes bounds the token endpoint response body pear reads.
const maxTokenResponseBytes = 1 << 20

// oauthConfig is how pear talks OAuth to an MCP server's authorization server:
// discovered, and registered with, when the server is added. It's stored on
// the server's row (see serverRow.OAuth), all but the client secret, which is
// stored encrypted beside it.
type oauthConfig struct {
	// Resource is the server's canonical resource identifier (RFC 8707),
	// sent with every authorization and token request.
	Resource string `gorm:"column:resource;not null;default:''"`
	Issuer   string `gorm:"column:issuer;not null;default:''"`
	AuthURL  string `gorm:"column:auth_url;not null;default:''"`
	TokenURL string `gorm:"column:token_url;not null;default:''"`
	// IssParameterSupported is whether the authorization server sends iss in
	// its authorization responses (RFC 9207).
	IssParameterSupported bool             `gorm:"column:iss_parameter_supported;not null;default:false"`
	Scopes                scopes           `gorm:"column:scopes;not null;default:''"`
	ClientID              string           `gorm:"column:client_id;not null;default:''"`
	AuthStyle             oauth2.AuthStyle `gorm:"column:auth_style;not null;default:0"`
	// ClientSecret is empty for a public client. It isn't a column: see
	// serverRow.OAuthClientSecretEnc.
	ClientSecret string `gorm:"-"`
}

// scopes are OAuth scopes, stored as OAuth writes them: space-separated.
type scopes []string

// GormDataType stores scopes in a string column.
func (scopes) GormDataType() string { return "string" }

// Value implements driver.Valuer.
func (s scopes) Value() (driver.Value, error) {
	return strings.Join(s, " "), nil
}

// Scan implements sql.Scanner.
func (s *scopes) Scan(src any) error {
	switch v := src.(type) {
	case string:
		*s = strings.Fields(v)
	case []byte:
		*s = strings.Fields(string(v))
	case nil:
		*s = nil
	default:
		return fmt.Errorf("scan scopes: unsupported type %T", src)
	}
	return nil
}

func (c *oauthConfig) oauth2Config(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   c.AuthURL,
			TokenURL:  c.TokenURL,
			AuthStyle: c.AuthStyle,
		},
		RedirectURL: redirectURL,
		Scopes:      c.Scopes,
	}
}

// preregisteredClient is an OAuth client an admin registered with an MCP
// server's authorization server by hand.
type preregisteredClient struct {
	ID     string
	Secret string
}

// discoverOAuth discovers serverURL's authorization server and registers pear
// with it as a client redirecting to redirectURL, unless client is given. Every
// failure wraps ErrOAuthDiscovery.
func discoverOAuth(
	ctx context.Context,
	client *http.Client,
	serverURL, redirectURL string,
	preregistered *preregisteredClient,
) (*oauthConfig, error) {
	// Callers validate serverURL already; checking it again here, right
	// before requesting it, lets static analysis (CodeQL's request-forgery
	// query) see that the admin-supplied URL is checked. What it can reach is
	// limited by client, which in pear only dials public addresses (see
	// httpx.NewPublicClient).
	if !serverURLPattern.MatchString(serverURL) {
		return nil, ErrInvalidServerURL
	}
	challenges, err := probeChallenges(ctx, client, serverURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOAuthDiscovery, err)
	}
	prm := protectedResourceMetadata(ctx, client, challenges, serverURL)
	if len(prm.AuthorizationServers) == 0 {
		return nil, fmt.Errorf(
			"%w: protected resource metadata names no authorization server", ErrOAuthDiscovery,
		)
	}
	issuer := prm.AuthorizationServers[0]
	asm, err := auth.GetAuthServerMetadata(ctx, issuer, client)
	if err != nil {
		return nil, fmt.Errorf("%w: authorization server metadata: %w", ErrOAuthDiscovery, err)
	}
	if asm == nil {
		// Servers from before metadata discovery (MCP 2025-03-26) use these
		// default endpoints.
		asm = &oauthex.AuthServerMeta{
			Issuer:                issuer,
			AuthorizationEndpoint: issuer + "/authorize",
			TokenEndpoint:         issuer + "/token",
			RegistrationEndpoint:  issuer + "/register",
		}
	} else if !slices.Contains(asm.CodeChallengeMethodsSupported, "S256") {
		return nil, fmt.Errorf(
			"%w: authorization server doesn't support PKCE S256",
			ErrOAuthDiscovery,
		)
	}

	cfg := &oauthConfig{
		Resource:              prm.Resource,
		Issuer:                asm.Issuer,
		AuthURL:               asm.AuthorizationEndpoint,
		TokenURL:              asm.TokenEndpoint,
		IssParameterSupported: asm.AuthorizationResponseIssParameterSupported,
		Scopes:                scopesFromChallenges(challenges),
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = prm.ScopesSupported
	}
	// pear stores refresh tokens, so asks for them where it's an explicit
	// scope (see MCP SEP-2207).
	if slices.Contains(asm.ScopesSupported, "offline_access") &&
		!slices.Contains(cfg.Scopes, "offline_access") {
		cfg.Scopes = append(cfg.Scopes, "offline_access")
	}

	if preregistered != nil {
		cfg.ClientID = preregistered.ID
		cfg.ClientSecret = preregistered.Secret
		cfg.AuthStyle = tokenAuthStyle(asm.TokenEndpointAuthMethodsSupported)
		return cfg, nil
	}
	if asm.RegistrationEndpoint == "" {
		return nil, fmt.Errorf(
			"%w: its authorization server doesn't support dynamic client registration, "+
				"so it needs a pre-registered client id",
			ErrOAuthDiscovery,
		)
	}
	authMethod := tokenAuthMethod(asm.TokenEndpointAuthMethodsSupported)
	reg, err := oauthex.RegisterClient(
		ctx,
		asm.RegistrationEndpoint,
		&oauthex.ClientRegistrationMetadata{
			RedirectURIs:            []string{redirectURL},
			TokenEndpointAuthMethod: authMethod,
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			ClientName:              clientName,
			Scope:                   strings.Join(cfg.Scopes, " "),
			ApplicationType:         "web",
		},
		client,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: register client: %w", ErrOAuthDiscovery, err)
	}
	cfg.ClientID = reg.ClientID
	cfg.ClientSecret = reg.ClientSecret
	if reg.TokenEndpointAuthMethod != "" {
		authMethod = reg.TokenEndpointAuthMethod
	}
	cfg.AuthStyle = authMethodStyle(authMethod)
	return cfg, nil
}

// probeChallenges makes an unauthenticated MCP initialize request to
// serverURL and returns the WWW-Authenticate challenges it answers with, if
// any. A server that answers without asking for auth yields none, and
// discovery falls back to the well-known metadata locations.
func probeChallenges(
	ctx context.Context,
	client *http.Client,
	serverURL string,
) ([]oauthex.Challenge, error) {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{` +
		`"protocolVersion":"2025-11-25","capabilities":{},` +
		`"clientInfo":{"name":"habitat-pear","version":"0.1.0"}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach mcp server: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusUnauthorized {
		return nil, nil
	}
	challenges, err := oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))
	if err != nil {
		return nil, fmt.Errorf("parse WWW-Authenticate: %w", err)
	}
	return challenges, nil
}

// protectedResourceMetadata finds serverURL's protected resource metadata
// (RFC 9728): at the URL its challenges name, or at its well-known locations.
// Servers without any are, per MCP 2025-03-26, their own authorization server.
func protectedResourceMetadata(
	ctx context.Context,
	client *http.Client,
	challenges []oauthex.Challenge,
	serverURL string,
) *oauthex.ProtectedResourceMetadata {
	for _, candidate := range protectedResourceMetadataURLs(challenges, serverURL) {
		prm, err := oauthex.GetProtectedResourceMetadata(
			ctx, candidate.url, candidate.resource, client,
		)
		if err == nil && prm != nil {
			return prm
		}
	}
	u, err := url.Parse(serverURL)
	if err != nil {
		return &oauthex.ProtectedResourceMetadata{Resource: serverURL}
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	return &oauthex.ProtectedResourceMetadata{
		Resource:             serverURL,
		AuthorizationServers: []string{u.String()},
	}
}

type prmCandidate struct {
	url string
	// resource is the resource identifier the metadata at url must name.
	resource string
}

// protectedResourceMetadataURLs lists where serverURL's protected resource
// metadata may be, in the order the MCP spec says to try them.
func protectedResourceMetadataURLs(
	challenges []oauthex.Challenge,
	serverURL string,
) []prmCandidate {
	var out []prmCandidate
	// A resource identifier is compared to the metadata's byte for byte, and
	// servers publish theirs without a trailing slash (Grafana's is
	// https://mcp.grafana.com/mcp), so an admin typing .../mcp/ would never
	// match. Try the URL as given first, then without trailing slashes.
	resources := []string{serverURL}
	if trimmed := strings.TrimRight(serverURL, "/"); trimmed != serverURL {
		resources = append(resources, trimmed)
	}
	for _, resource := range resources {
		for _, c := range challenges {
			if u := c.Params["resource_metadata"]; u != "" {
				out = append(out, prmCandidate{url: u, resource: resource})
				break
			}
		}
		ru, err := url.Parse(resource)
		if err != nil {
			continue
		}
		mu := *ru
		mu.RawQuery, mu.Fragment = "", ""
		mu.Path = "/.well-known/oauth-protected-resource/" + strings.TrimLeft(ru.Path, "/")
		out = append(out, prmCandidate{url: mu.String(), resource: resource})
		mu.Path = "/.well-known/oauth-protected-resource"
		root := *ru
		root.Path, root.RawQuery, root.Fragment = "", "", ""
		out = append(out, prmCandidate{url: mu.String(), resource: root.String()})
	}
	return out
}

func scopesFromChallenges(challenges []oauthex.Challenge) []string {
	for _, c := range challenges {
		if c.Scheme == "bearer" && c.Params["scope"] != "" {
			return strings.Fields(c.Params["scope"])
		}
	}
	return nil
}

// tokenAuthMethod picks how pear authenticates to a token endpoint supporting
// methods: with a client secret where it can, since pear can keep one, else
// as a public client.
func tokenAuthMethod(methods []string) string {
	for _, m := range []string{"client_secret_post", "client_secret_basic"} {
		if slices.Contains(methods, m) {
			return m
		}
	}
	if len(methods) == 0 {
		// RFC 8414's default when the metadata lists none.
		return "client_secret_basic"
	}
	return "none"
}

func tokenAuthStyle(methods []string) oauth2.AuthStyle {
	return authMethodStyle(tokenAuthMethod(methods))
}

func authMethodStyle(method string) oauth2.AuthStyle {
	switch method {
	case "client_secret_post", "none":
		return oauth2.AuthStyleInParams
	case "client_secret_basic":
		return oauth2.AuthStyleInHeader
	default:
		return oauth2.AuthStyleAutoDetect
	}
}

// authorizationURL is where to send a member's browser to authorize pear with
// the server, binding the request to state and the PKCE verifier.
//
// access_type=offline and prompt=consent ask for a refresh token from servers
// like Google's that don't use the offline_access scope: they only issue one
// on the first consent, or when consent is forced. Servers that don't know
// these parameters ignore them.
func (c *oauthConfig) authorizationURL(redirectURL, state, verifier string) string {
	return c.oauth2Config(redirectURL).AuthCodeURL(
		state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("resource", c.Resource),
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
	)
}

// checkIssuer validates the iss parameter of an authorization response (RFC
// 9207).
func (c *oauthConfig) checkIssuer(iss string) error {
	if iss == "" {
		if c.IssParameterSupported {
			return fmt.Errorf("%w: authorization response is missing iss", ErrAuthorizationFailed)
		}
		return nil
	}
	if iss != c.Issuer {
		return fmt.Errorf(
			"%w: authorization response issuer %q isn't %q", ErrAuthorizationFailed, iss, c.Issuer,
		)
	}
	return nil
}

// exchange exchanges an authorization code for tokens.
func (c *oauthConfig) exchange(
	ctx context.Context,
	client *http.Client,
	redirectURL, code, verifier string,
) (*oauth2.Token, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	token, err := c.oauth2Config(redirectURL).Exchange(
		ctx, code,
		oauth2.VerifierOption(verifier),
		oauth2.SetAuthURLParam("resource", c.Resource),
	)
	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) {
		return nil, fmt.Errorf("%w: %w", ErrAuthorizationFailed, err)
	} else if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}
	return token, nil
}

// errInvalidGrant is returned by refresh when the authorization server no
// longer honors the refresh token.
var errInvalidGrant = errors.New("refresh token rejected")

// refresh redeems refreshToken for new tokens. It's done by hand, rather than
// through oauth2.TokenSource, because the MCP spec requires the resource
// parameter on every token request, which oauth2 can't add to refreshes.
func (c *oauthConfig) refresh(
	ctx context.Context,
	client *http.Client,
	refreshToken string,
) (*oauth2.Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"resource":      {c.Resource},
	}
	if c.AuthStyle != oauth2.AuthStyleInHeader {
		form.Set("client_id", c.ClientID)
		if c.ClientSecret != "" {
			form.Set("client_secret", c.ClientSecret)
		}
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.TokenURL, bytes.NewBufferString(form.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if c.AuthStyle == oauth2.AuthStyleInHeader {
		req.SetBasicAuth(url.QueryEscape(c.ClientID), url.QueryEscape(c.ClientSecret))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}
	var tr struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
	}
	// An error body is JSON too, so decode before checking the status.
	_ = json.Unmarshal(body, &tr)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if tr.Error == "invalid_grant" {
			return nil, errInvalidGrant
		}
		return nil, fmt.Errorf("refresh token: status %d: %s", resp.StatusCode, tr.Error)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("refresh token: response has no access_token")
	}
	token := &oauth2.Token{
		AccessToken:  tr.AccessToken,
		TokenType:    tr.TokenType,
		RefreshToken: tr.RefreshToken,
	}
	if tr.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	if token.RefreshToken == "" {
		// The authorization server didn't rotate it, so it's still good.
		token.RefreshToken = refreshToken
	}
	return token, nil
}

package mcpgateway

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// oauthConfig is the OAuth client configuration discovered and registered
// against an MCP server's authorization server.
type oauthConfig struct {
	Issuer                string
	AuthorizationEndpoint string
	TokenEndpoint         string
	Scopes                []string
	ClientID              string
	ClientSecret          string
}

// detectAuth probes mcpServerURL per the MCP authorization spec
// (https://modelcontextprotocol.io/docs/tutorials/security/authorization):
// a server that requires authorization must reject an unauthenticated
// request with 401 and a "WWW-Authenticate: Bearer ..." challenge. Any other
// response means the server doesn't require per-user authorization.
func (s *store) detectAuth(ctx context.Context, mcpServerURL string) (AuthType, *oauthConfig, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, mcpServerURL, strings.NewReader("{}"),
	)
	if err != nil {
		return "", nil, fmt.Errorf("build probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("probing mcp server: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		return AuthTypeNone, nil, nil
	}

	challenges, err := oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))
	if err != nil || !hasBearerChallenge(challenges) {
		return AuthTypeNone, nil, nil
	}

	cfg, err := s.discoverOAuth(ctx, mcpServerURL, challenges)
	if err != nil {
		return "", nil, fmt.Errorf(
			"mcp server requires authorization but its oauth configuration could not be discovered: %w",
			err,
		)
	}
	return AuthTypeOAuth, cfg, nil
}

func hasBearerChallenge(challenges []oauthex.Challenge) bool {
	for _, c := range challenges {
		if c.Scheme == "bearer" {
			return true
		}
	}
	return false
}

func resourceMetadataURLFromChallenges(challenges []oauthex.Challenge) string {
	for _, c := range challenges {
		if u := c.Params["resource_metadata"]; u != "" {
			return u
		}
	}
	return ""
}

func scopesFromChallenges(challenges []oauthex.Challenge) []string {
	for _, c := range challenges {
		if c.Scheme == "bearer" && c.Params["scope"] != "" {
			return strings.Fields(c.Params["scope"])
		}
	}
	return nil
}

type prmCandidate struct {
	url      string
	resource string
}

// protectedResourceMetadataURLs returns the URLs to try when looking for
// protected resource metadata, in the order recommended by the MCP spec: the
// URL named in the WWW-Authenticate challenge (if any), then the well-known
// path scoped to the resource's own path (RFC 9728), then the well-known
// path at the resource's origin root.
func protectedResourceMetadataURLs(challengeURL, resourceURL string) []prmCandidate {
	var candidates []prmCandidate
	if challengeURL != "" {
		candidates = append(candidates, prmCandidate{url: challengeURL, resource: resourceURL})
	}
	ru, err := url.Parse(resourceURL)
	if err != nil {
		return candidates
	}

	scoped := *ru
	scoped.Path = "/.well-known/oauth-protected-resource" +
		strings.TrimSuffix("/"+strings.TrimPrefix(ru.Path, "/"), "/")
	scoped.RawQuery = ""
	candidates = append(candidates, prmCandidate{url: scoped.String(), resource: resourceURL})

	root := *ru
	root.Path = "/.well-known/oauth-protected-resource"
	root.RawQuery = ""
	rootResource := *ru
	rootResource.Path = ""
	rootResource.RawQuery = ""
	candidates = append(candidates, prmCandidate{url: root.String(), resource: rootResource.String()})

	return candidates
}

// authServerMetadataURL builds the RFC 8414 well-known metadata URL for an
// authorization server issuer.
func authServerMetadataURL(issuer string) (string, error) {
	u, err := url.Parse(issuer)
	if err != nil {
		return "", fmt.Errorf("parsing issuer %q: %w", issuer, err)
	}
	path := strings.TrimSuffix(u.Path, "/")
	u.Path = "/.well-known/oauth-authorization-server" + path
	return u.String(), nil
}

// discoverOAuth discovers an MCP server's authorization server (RFC 9728),
// fetches that server's metadata (RFC 8414), and dynamically registers a
// client with it (RFC 7591).
func (s *store) discoverOAuth(
	ctx context.Context,
	mcpServerURL string,
	challenges []oauthex.Challenge,
) (*oauthConfig, error) {
	var prm *oauthex.ProtectedResourceMetadata
	challengeURL := resourceMetadataURLFromChallenges(challenges)
	for _, candidate := range protectedResourceMetadataURLs(challengeURL, mcpServerURL) {
		found, err := oauthex.GetProtectedResourceMetadata(
			ctx, candidate.url, candidate.resource, s.httpClient,
		)
		if err == nil && found != nil {
			prm = found
			break
		}
	}
	if prm == nil || len(prm.AuthorizationServers) == 0 {
		return nil, fmt.Errorf(
			"could not discover protected resource metadata with an authorization server",
		)
	}
	issuer := prm.AuthorizationServers[0]

	asMetadataURL, err := authServerMetadataURL(issuer)
	if err != nil {
		return nil, err
	}
	asm, err := oauthex.GetAuthServerMeta(ctx, asMetadataURL, issuer, s.httpClient)
	if err != nil {
		return nil, fmt.Errorf("fetching authorization server metadata: %w", err)
	}
	if asm == nil {
		return nil, fmt.Errorf("authorization server %q did not return metadata", issuer)
	}
	if asm.RegistrationEndpoint == "" {
		return nil, fmt.Errorf(
			"authorization server %q does not support dynamic client registration", issuer,
		)
	}

	scopes := prm.ScopesSupported
	if len(scopes) == 0 {
		scopes = scopesFromChallenges(challenges)
	}

	reg, err := oauthex.RegisterClient(ctx, asm.RegistrationEndpoint, &oauthex.ClientRegistrationMetadata{
		RedirectURIs:            []string{s.redirectURL},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
		ClientName:              "Habitat",
		ApplicationType:         "web",
	}, s.httpClient)
	if err != nil {
		return nil, fmt.Errorf("registering oauth client: %w", err)
	}

	return &oauthConfig{
		Issuer:                asm.Issuer,
		AuthorizationEndpoint: asm.AuthorizationEndpoint,
		TokenEndpoint:         asm.TokenEndpoint,
		Scopes:                scopes,
		ClientID:              reg.ClientID,
		ClientSecret:          reg.ClientSecret,
	}, nil
}

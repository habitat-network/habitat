package oauthserver

import (
	"fmt"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
)

// Localhost development clients don't publish a metadata document; the
// document is synthesized from the client_id URL instead. See
// https://atproto.com/specs/oauth#localhost-client-development.
var (
	// defaultLocalhostRedirectUris are used when the client_id carries no
	// redirect_uri query parameter.
	defaultLocalhostRedirectUris = []string{"http://127.0.0.1/", "http://[::1]/"}
	defaultLocalhostScope        = "atproto"
	localhostClientName          = "Development client"
)

// isLocalhostClientId reports whether a client_id URL is a localhost
// development client_id, i.e. one whose metadata is synthesized rather than
// fetched. The hostname must be exactly `localhost`; loopback IP literals are
// not localhost client_ids.
func isLocalhostClientId(u *url.URL) bool {
	return u.Scheme == "http" && u.Hostname() == "localhost"
}

// localhostClientMetadata builds the virtual client metadata document for a
// localhost development client_id. The client_id origin must be exactly
// `http://localhost` with no port and an empty path; redirect_uri (repeatable)
// and scope (single) may be supplied as query parameters. Such a client is
// always public: it authenticates with no secret.
//
// See https://atproto.com/specs/oauth#localhost-client-development.
func localhostClientMetadata(id string, u *url.URL) (*oauth.ClientMetadata, error) {
	if u.Port() != "" {
		return nil, fmt.Errorf("localhost client id must not specify a port")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("localhost client id must have an empty path")
	}
	if u.User != nil {
		return nil, fmt.Errorf("localhost client id must not contain userinfo")
	}
	if u.Fragment != "" {
		return nil, fmt.Errorf("localhost client id must not contain a fragment")
	}

	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to parse localhost client id query: %w", err)
	}
	for param := range query {
		if param != "redirect_uri" && param != "scope" {
			return nil, fmt.Errorf("unsupported localhost client id parameter %q", param)
		}
	}

	scope := defaultLocalhostScope
	if scopes := query["scope"]; len(scopes) > 1 {
		return nil, fmt.Errorf("localhost client id must have at most one scope parameter")
	} else if len(scopes) == 1 {
		scope = scopes[0]
	}

	redirectUris := query["redirect_uri"]
	if len(redirectUris) == 0 {
		redirectUris = defaultLocalhostRedirectUris
	}
	for _, redirectUri := range redirectUris {
		if err := validateLoopbackRedirectUri(redirectUri); err != nil {
			return nil, err
		}
	}

	return &oauth.ClientMetadata{
		ClientID:                id,
		ClientName:              new(localhostClientName),
		ApplicationType:         new("native"),
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		RedirectURIs:            redirectUris,
		Scope:                   scope,
		TokenEndpointAuthMethod: "none",
		DPoPBoundAccessTokens:   true,
	}, nil
}

// validateLoopbackRedirectUri checks that a localhost client's declared
// redirect URI points at the loopback interface by IP literal. The port is
// deliberately unconstrained: the client is matched against these URIs ignoring
// the port (see fosite's RFC 8252 §7.3 loopback matching), so a dev server on
// any port works.
func validateLoopbackRedirectUri(redirectUri string) error {
	u, err := url.Parse(redirectUri)
	if err != nil {
		return fmt.Errorf("failed to parse localhost client redirect uri: %w", err)
	}
	if u.Scheme != "http" {
		return fmt.Errorf("localhost client redirect uri must use the http scheme")
	}
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return fmt.Errorf("localhost client redirect uri must use a loopback IP address")
	}
	return nil
}

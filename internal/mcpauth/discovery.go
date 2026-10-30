package mcpauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// ErrNoAuthServerMetadata is returned when a server's authorization server
// publishes no RFC 8414 or OpenID Connect metadata.
var ErrNoAuthServerMetadata = errors.New("mcpauth: authorization server publishes no metadata")

// ErrPKCEUnsupported is returned when an authorization server doesn't
// support PKCE with S256, which the MCP authorization spec requires.
var ErrPKCEUnsupported = errors.New("mcpauth: authorization server does not support PKCE S256")

// Discovery is what pear learns about an MCP server's authorization before
// sending a member to authorize.
type Discovery struct {
	// Resource is the server's RFC 8707 resource indicator.
	Resource string
	// Scopes are the scopes to request.
	Scopes     []string
	AuthServer *oauthex.AuthServerMeta
}

// Discoverer finds an MCP server's authorization server.
type Discoverer interface {
	Discover(ctx context.Context, serverURL string) (*Discovery, error)
}

type discoverer struct {
	httpClient *http.Client
}

// NewDiscoverer returns a Discoverer following the MCP authorization spec:
// the server's WWW-Authenticate challenge or its RFC 9728 protected resource
// metadata names its authorization server, whose RFC 8414 (or OpenID Connect)
// metadata says how to authorize. Metadata is fetched with the go-sdk's
// oauthex helpers, which refuse non-HTTPS URLs other than loopback ones and,
// unless httpClient has its own dialer or a proxy is configured, connections
// to non-public addresses.
//
// TODO: the initial unauthenticated request to the server itself goes
// through httpClient unguarded, as pear's MCP client does today.
func NewDiscoverer(httpClient *http.Client) Discoverer {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &discoverer{httpClient: httpClient}
}

func (d *discoverer) Discover(ctx context.Context, serverURL string) (*Discovery, error) {
	challenges, err := d.challenges(ctx, serverURL)
	if err != nil {
		return nil, err
	}
	prm, err := d.protectedResourceMetadata(ctx, challenges, serverURL)
	if err != nil {
		return nil, err
	}
	issuer := prm.AuthorizationServers[0]
	asm, err := auth.GetAuthServerMetadata(ctx, issuer, d.httpClient)
	if err != nil {
		return nil, fmt.Errorf("mcpauth: fetch authorization server metadata: %w", err)
	}
	if asm == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoAuthServerMetadata, issuer)
	}
	if !slices.Contains(asm.CodeChallengeMethodsSupported, "S256") {
		return nil, fmt.Errorf("%w: %s", ErrPKCEUnsupported, issuer)
	}

	scopes := scopeFromChallenges(challenges)
	if len(scopes) == 0 {
		scopes = prm.ScopesSupported
	}
	// Ask for a refresh token wherever the server offers them (SEP-2207), so
	// members don't have to sign in again whenever an access token expires.
	if slices.Contains(asm.ScopesSupported, "offline_access") &&
		!slices.Contains(scopes, "offline_access") {
		scopes = append(slices.Clone(scopes), "offline_access")
	}
	return &Discovery{Resource: prm.Resource, Scopes: scopes, AuthServer: asm}, nil
}

// maxProbeBodyBytes caps how much of the unauthenticated probe's response is
// drained before closing it.
const maxProbeBodyBytes = 64 << 10

// challenges makes an unauthenticated request to the server and returns the
// WWW-Authenticate challenges it answers with. A server that doesn't answer
// 401 has none.
func (d *discoverer) challenges(
	ctx context.Context,
	serverURL string,
) ([]oauthex.Challenge, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("mcpauth: build probe request: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcpauth: reach mcp server: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxProbeBodyBytes))
	if resp.StatusCode != http.StatusUnauthorized {
		return nil, nil
	}
	challenges, err := oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))
	if err != nil {
		return nil, fmt.Errorf("mcpauth: parse WWW-Authenticate: %w", err)
	}
	return challenges, nil
}

// protectedResourceMetadata fetches the server's RFC 9728 metadata from the
// first of the candidate URLs the MCP spec lists that serves it. A server
// with none is treated as its own authorization server, as in the 2025-03-26
// spec.
func (d *discoverer) protectedResourceMetadata(
	ctx context.Context,
	challenges []oauthex.Challenge,
	serverURL string,
) (*oauthex.ProtectedResourceMetadata, error) {
	candidates, err := protectedResourceMetadataURLs(
		challengeParam(challenges, "resource_metadata"),
		serverURL,
	)
	if err != nil {
		return nil, err
	}
	for _, c := range candidates {
		prm, err := oauthex.GetProtectedResourceMetadata(
			ctx,
			c.metadataURL,
			c.resource,
			d.httpClient,
		)
		if err != nil || prm == nil {
			continue
		}
		if len(prm.AuthorizationServers) == 0 {
			return nil, fmt.Errorf(
				"mcpauth: protected resource metadata at %s names no authorization server",
				c.metadataURL,
			)
		}
		return prm, nil
	}
	u, _ := url.Parse(serverURL) // parsed by protectedResourceMetadataURLs already
	return &oauthex.ProtectedResourceMetadata{
		Resource:             serverURL,
		AuthorizationServers: []string{(&url.URL{Scheme: u.Scheme, Host: u.Host}).String()},
	}, nil
}

// prmCandidate is a URL protected resource metadata may be served at, and
// the resource identifier that metadata must name.
type prmCandidate struct {
	metadataURL string
	resource    string
}

// protectedResourceMetadataURLs lists where to look for serverURL's
// protected resource metadata, in the order the MCP spec gives: the URL the
// server's challenge named, then the well-known URL at the server's path,
// then the one at its root. It mirrors the go-sdk's unexported helper of the
// same name in auth/authorization_code.go.
func protectedResourceMetadataURLs(challengeURL, serverURL string) ([]prmCandidate, error) {
	u, err := url.Parse(serverURL)
	if err != nil || !u.IsAbs() {
		return nil, fmt.Errorf("mcpauth: invalid server url %q", serverURL)
	}
	var out []prmCandidate
	if challengeURL != "" {
		out = append(out, prmCandidate{metadataURL: challengeURL, resource: serverURL})
	}
	if path := strings.TrimLeft(u.Path, "/"); path != "" {
		atPath := url.URL{
			Scheme: u.Scheme,
			Host:   u.Host,
			Path:   "/.well-known/oauth-protected-resource/" + path,
		}
		out = append(out, prmCandidate{metadataURL: atPath.String(), resource: serverURL})
	}
	root := url.URL{Scheme: u.Scheme, Host: u.Host}
	atRoot := url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/.well-known/oauth-protected-resource"}
	out = append(out, prmCandidate{metadataURL: atRoot.String(), resource: root.String()})
	return out, nil
}

// challengeParam returns param from the first Bearer challenge that has it.
func challengeParam(challenges []oauthex.Challenge, param string) string {
	for _, c := range challenges {
		if c.Scheme == "bearer" && c.Params[param] != "" {
			return c.Params[param]
		}
	}
	return ""
}

// scopeFromChallenges returns the scopes a Bearer challenge asked for.
func scopeFromChallenges(challenges []oauthex.Challenge) []string {
	return strings.Fields(challengeParam(challenges, "scope"))
}

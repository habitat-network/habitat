package mcpauth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/habitat-network/habitat/internal/keyval"
)

func (s *service) Begin(ctx context.Context, server Server, member syntax.DID) (string, error) {
	disc, err := s.discoverer.Discover(ctx, server.URL)
	if err != nil {
		return "", err
	}
	client, err := s.client(ctx, disc.AuthServer)
	if err != nil {
		return "", err
	}

	verifier := oauth2.GenerateVerifier()
	state := rand.Text()
	flow := pendingFlow{
		Server:       server,
		Member:       member,
		CodeVerifier: verifier,
		Resource:     disc.Resource,
		Endpoints: endpoints{
			Issuer:                disc.AuthServer.Issuer,
			AuthorizationURL:      disc.AuthServer.AuthorizationEndpoint,
			TokenURL:              disc.AuthServer.TokenEndpoint,
			IssParameterSupported: disc.AuthServer.AuthorizationResponseIssParameterSupported,
		},
		Client: client,
		Scopes: disc.Scopes,
	}
	if err := s.secrets.PutEncrypted(ctx, flowPrefix+state, flow, flowTTL); err != nil {
		return "", fmt.Errorf("store mcp authorization request: %w", err)
	}

	authURL, err := url.Parse(flow.Endpoints.AuthorizationURL)
	if err != nil {
		return "", fmt.Errorf("mcpauth: invalid authorization endpoint: %w", err)
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", client.ClientID)
	q.Set("redirect_uri", s.cfg.RedirectURL)
	q.Set("state", state)
	q.Set("code_challenge", oauth2.S256ChallengeFromVerifier(verifier))
	q.Set("code_challenge_method", "S256")
	q.Set("resource", flow.Resource)
	if len(flow.Scopes) > 0 {
		q.Set("scope", strings.Join(flow.Scopes, " "))
	}
	authURL.RawQuery = q.Encode()
	return authURL.String(), nil
}

func (s *service) Complete(ctx context.Context, callback Callback) (*Connection, error) {
	var flow pendingFlow
	err := s.secrets.ConsumeEncrypted(ctx, flowPrefix+callback.State, &flow)
	if errors.Is(err, keyval.ErrNotFound) {
		return nil, ErrFlowNotFound
	} else if err != nil {
		return nil, fmt.Errorf("load mcp authorization request: %w", err)
	}
	if callback.Error != "" {
		return nil, fmt.Errorf(
			"%w: %s %s",
			ErrAuthorizationDenied,
			callback.Error,
			callback.ErrorDescription,
		)
	}
	if err := checkIssuer(callback.Iss, flow.Endpoints); err != nil {
		return nil, err
	}

	resp, err := requestToken(ctx, s.httpClient, flow.Endpoints.TokenURL, flow.Client, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {callback.Code},
		"redirect_uri":  {s.cfg.RedirectURL},
		"code_verifier": {flow.CodeVerifier},
		"resource":      {flow.Resource},
	})
	if err != nil {
		return nil, err
	}
	scopes := flow.Scopes
	if resp.Scope != "" {
		scopes = strings.Fields(resp.Scope)
	}
	cred := &credential{
		ServerURL:    flow.Server.URL,
		Resource:     flow.Resource,
		Endpoints:    flow.Endpoints,
		Client:       flow.Client,
		RefreshToken: resp.RefreshToken,
		Scopes:       scopes,
	}
	if _, err := s.save(ctx, connectionKey(flow.Server, flow.Member), cred, resp); err != nil {
		return nil, err
	}
	return &Connection{Server: flow.Server, Member: flow.Member}, nil
}

// checkIssuer validates an authorization response's iss parameter (RFC
// 9207): required if the server promised it, and when present it must name
// the server the request went to.
func checkIssuer(iss string, ep endpoints) error {
	if iss == "" {
		if ep.IssParameterSupported {
			return fmt.Errorf("%w: missing iss", ErrIssuerMismatch)
		}
		return nil
	}
	if iss != ep.Issuer {
		return fmt.Errorf("%w: got %q, want %q", ErrIssuerMismatch, iss, ep.Issuer)
	}
	return nil
}

func (s *service) Status(ctx context.Context, server Server, member syntax.DID) (Status, error) {
	cred, err := s.loadCredential(ctx, server, member)
	if errors.Is(err, ErrNotConnected) {
		return StatusNotConnected, nil
	} else if err != nil {
		return "", err
	}
	if cred.NeedsReauth {
		return StatusNeedsReauth, nil
	}
	return StatusConnected, nil
}

// TODO: revoke the tokens at the authorization server (RFC 7009) when it
// advertises a revocation endpoint.
func (s *service) Disconnect(ctx context.Context, server Server, member syntax.DID) error {
	key := connectionKey(server, member)
	if err := s.plain.Delete(ctx, credentialPrefix+key); errors.Is(err, keyval.ErrNotFound) {
		return ErrNotConnected
	} else if err != nil {
		return fmt.Errorf("delete mcp credential: %w", err)
	}
	if err := s.deleteIfPresent(ctx, tokenPrefix+key); err != nil {
		return fmt.Errorf("delete mcp access token: %w", err)
	}
	return nil
}

// client returns pear's client registration with asm's authorization
// server: its client ID metadata document if both sides support one, else
// a dynamic registration (RFC 7591), made once per server and kept.
func (s *service) client(
	ctx context.Context,
	asm *oauthex.AuthServerMeta,
) (registeredClient, error) {
	if s.cfg.ClientIDMetadataURL != "" && asm.ClientIDMetadataDocumentSupported {
		return registeredClient{ClientID: s.cfg.ClientIDMetadataURL, AuthMethod: "none"}, nil
	}

	key := clientKey(asm.Issuer, s.cfg.RedirectURL)
	var client registeredClient
	err := s.secrets.GetEncrypted(ctx, key, &client)
	if err == nil {
		return client, nil
	} else if !errors.Is(err, keyval.ErrNotFound) {
		return registeredClient{}, fmt.Errorf("load mcp client registration: %w", err)
	}
	if asm.RegistrationEndpoint == "" {
		return registeredClient{}, fmt.Errorf(
			"%w: %s",
			ErrClientRegistrationUnsupported,
			asm.Issuer,
		)
	}

	// Pear keeps its secrets server-side, so it registers as a confidential
	// client wherever the server allows one.
	authMethod := "none"
	for _, m := range []string{"client_secret_basic", "client_secret_post"} {
		if slices.Contains(asm.TokenEndpointAuthMethodsSupported, m) {
			authMethod = m
			break
		}
	}
	resp, err := oauthex.RegisterClient(
		ctx,
		asm.RegistrationEndpoint,
		&oauthex.ClientRegistrationMetadata{
			RedirectURIs:            []string{s.cfg.RedirectURL},
			TokenEndpointAuthMethod: authMethod,
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			ClientName:              s.cfg.ClientName,
			ApplicationType:         "web",
		},
		s.httpClient,
	)
	if err != nil {
		return registeredClient{}, fmt.Errorf("mcpauth: register client: %w", err)
	}
	client = registeredClient{
		ClientID:     resp.ClientID,
		ClientSecret: resp.ClientSecret,
		AuthMethod:   authMethod,
	}
	if resp.TokenEndpointAuthMethod != "" {
		client.AuthMethod = resp.TokenEndpointAuthMethod
	}
	if client.ClientSecret == "" {
		client.AuthMethod = "none"
	}
	if err := s.secrets.PutEncrypted(ctx, key, client, keyval.NoExpiry); err != nil {
		return registeredClient{}, fmt.Errorf("store mcp client registration: %w", err)
	}
	return client, nil
}

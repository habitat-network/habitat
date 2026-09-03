// Package clientmeta resolves AT Proto OAuth client-id-metadata-documents
// and their published JWKS, for verifying JWTs a client signs with its own
// key: RFC 7523 JWT-bearer client authentication (internal/oauthserver) and
// AT Proto client attestation (internal/spaces/server). See
// https://atproto.com/specs/oauth#client-id-metadata-document.
package clientmeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/utils"
)

// ErrKeyNotFound is returned by ResolveKey when the client's published JWKS
// (inline or via jwks_uri) has no key matching the requested kid.
var ErrKeyNotFound = errors.New("no matching key in client jwks")

// Resolver fetches client metadata documents and JWKS over HTTP.
type Resolver struct {
	httpClient *http.Client
}

func WithClient(client *http.Client) utils.Opt[Resolver] {
	return func(r *Resolver) {
		r.httpClient = client
	}
}

// NewResolver constructs a Resolver whose fetches are bounded by
// defaultFetchTimeout.
func NewResolver(opts ...utils.Opt[Resolver]) *Resolver {
	return new(utils.ResolveOptions(Resolver{httpClient: httpx.NewClient()}, opts))
}

// FetchMetadata fetches and decodes the client metadata document published
// at clientID (the client's client_id URL). Localhost development client_ids
// are the exception: nothing is fetched, the metadata is derived from the
// client_id itself. See
// https://atproto.com/specs/oauth#localhost-client-development.
func (r *Resolver) FetchMetadata(
	ctx context.Context,
	clientID string,
) (*oauth.ClientMetadata, error) {
	parsed, err := url.Parse(clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client id: %w", err)
	}
	if isLocalhostClientId(parsed) {
		return localhostClientMetadata(clientID, parsed)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	// TODO: consider caching
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch client metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch client metadata: status %d", resp.StatusCode)
	}

	var metadata oauth.ClientMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to decode client metadata: %w", err)
	}
	return &metadata, nil
}

// fetchJWKS returns metadata's JWKS keys, from the inline jwks field if
// present, otherwise by fetching jwks_uri. Returns an empty, nil-error result
// if the client publishes neither.
func (r *Resolver) fetchJWKS(
	ctx context.Context,
	metadata *oauth.ClientMetadata,
) (*oauth.JWKS, error) {
	if metadata.JWKS != nil {
		return metadata.JWKS, nil
	}
	if metadata.JWKSURI == nil || *metadata.JWKSURI == "" {
		return nil, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *metadata.JWKSURI, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to make jwks_uri request: %w", err)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch jwks_uri: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch jwks_uri: status %d", resp.StatusCode)
	}

	var jwks oauth.JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode jwks_uri response: %w", err)
	}
	return &jwks, nil
}

// findJWK fetches clientID's metadata and returns the raw atproto JWK
// identified by kid from its published JWKS (inline jwks, or fetched from
// jwks_uri). Returns ErrKeyNotFound if the client has no matching key.
func (r *Resolver) findJWK(ctx context.Context, clientID, kid string) (atcrypto.JWK, error) {
	metadata, err := r.FetchMetadata(ctx, clientID)
	if err != nil {
		return atcrypto.JWK{}, err
	}
	jwks, err := r.fetchJWKS(ctx, metadata)
	if err != nil {
		return atcrypto.JWK{}, err
	}
	if jwks == nil {
		return atcrypto.JWK{}, ErrKeyNotFound
	}
	for _, key := range jwks.Keys {
		if key.KeyID == nil || *key.KeyID != kid {
			continue
		}
		return key, nil
	}
	return atcrypto.JWK{}, ErrKeyNotFound
}

// ResolveAtprotoKey fetches clientID's metadata and returns the key
// identified by kid from its published JWKS as an atcrypto.PublicKey, usable
// for signature verification with golang-jwt/v5's atproto-registered ES256
// signing method (see github.com/bluesky-social/indigo/atproto/auth).
// Returns ErrKeyNotFound if the client has no matching key.
func (r *Resolver) ResolveAtprotoKey(
	ctx context.Context,
	clientID, kid string,
) (atcrypto.PublicKey, error) {
	jwk, err := r.findJWK(ctx, clientID, kid)
	if err != nil {
		return nil, err
	}
	return atcrypto.ParsePublicJWK(jwk)
}

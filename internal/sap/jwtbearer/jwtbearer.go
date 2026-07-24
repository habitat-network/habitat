// Package jwtbearer authenticates sap to a habitat host as a confidential
// OAuth client using the RFC 7523 JWT-bearer grant: for a subject DID it mints
// an act-as-subject access token from the host's /oauth/token endpoint and
// returns an *http.Client that attaches it. Tokens are cached per DID.
package jwtbearer

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/golang-jwt/jwt/v5"
)

const keyID = "habitat"

const grantJWTBearer = "urn:ietf:params:oauth:grant-type:jwt-bearer"

// Directory resolves a DID's habitat host. Satisfied by identity.Directory.
type Directory interface {
	LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error)
}

// Builder mints JWT-bearer tokens and hands out authenticated clients.
type Builder struct {
	clientID string
	key      *ecdsa.PrivateKey
	dir      Directory
	http     *http.Client
	now      func() time.Time

	mu    sync.Mutex
	cache map[syntax.DID]cachedToken
}

type cachedToken struct {
	token string
	exp   time.Time
}

func New(clientID string, key *ecdsa.PrivateKey, dir Directory) *Builder {
	return &Builder{
		clientID: clientID,
		key:      key,
		dir:      dir,
		http:     &http.Client{Timeout: 30 * time.Second},
		now:      time.Now,
		cache:    map[syntax.DID]cachedToken{},
	}
}

// PublicJWK returns the public half of the signing key for the client-metadata
// JWKS.
func (b *Builder) PublicJWK() jose.JSONWebKey {
	return jose.JSONWebKey{
		Key:       b.key.Public(),
		KeyID:     keyID,
		Algorithm: string(jose.ES256),
		Use:       "sig",
	}
}

// ClientForDID returns an HTTP client that authenticates as did against its
// habitat host. Relative request URLs are resolved against that host.
func (b *Builder) ClientForDID(ctx context.Context, did syntax.DID) (*http.Client, error) {
	base, err := b.hostFor(ctx, did)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: &transport{b: b, did: did, base: base}}, nil
}

func (b *Builder) hostFor(ctx context.Context, did syntax.DID) (string, error) {
	ident, err := b.dir.LookupDID(ctx, did)
	if err != nil {
		return "", fmt.Errorf("lookup did %s: %w", did, err)
	}
	svc, ok := ident.Services["habitat"]
	if !ok || svc.URL == "" {
		return "", fmt.Errorf("did %s has no habitat service endpoint", did)
	}
	return strings.TrimSuffix(svc.URL, "/"), nil
}

// token returns a cached or freshly minted access token for did against base.
func (b *Builder) token(ctx context.Context, did syntax.DID, base string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if c, ok := b.cache[did]; ok && b.now().Add(30*time.Second).Before(c.exp) {
		return c.token, nil
	}

	assertion, err := b.sign(did, base)
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {grantJWTBearer}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, base+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := b.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("mint token for %s: %w", did, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mint token for %s: status %d", did, resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode token for %s: %w", did, err)
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	b.cache[did] = cachedToken{token: out.AccessToken, exp: b.now().Add(ttl)}
	return out.AccessToken, nil
}

func (b *Builder) sign(did syntax.DID, base string) (string, error) {
	now := b.now()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": b.clientID,
		"sub": did.String(),
		"aud": base + "/oauth/token",
		"iat": now.Unix(),
		"exp": now.Add(time.Minute).Unix(),
		"jti": randomJTI(),
	})
	tok.Header["kid"] = keyID
	return tok.SignedString(b.key)
}

func randomJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// transport authenticates each request as did and resolves relative URLs
// against the host base.
type transport struct {
	b    *Builder
	did  syntax.DID
	base string
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !req.URL.IsAbs() {
		u, err := url.Parse(t.base)
		if err != nil {
			return nil, fmt.Errorf("parse host base %q: %w", t.base, err)
		}
		req.URL.Scheme, req.URL.Host = u.Scheme, u.Host
	}
	tok, err := t.b.token(req.Context(), t.did, t.base)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Habitat-Auth-Method", "oauth")
	return http.DefaultTransport.RoundTrip(req)
}

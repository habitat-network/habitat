# pear ↔ sap Integration Test Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `integration/sap_test.go`, an end-to-end test that runs the real `cmd/pear` and `cmd/sap` as Docker containers and verifies sap eventually syncs every record written to pear — under concurrency and degraded `notifyWrite` network conditions — observed through sap's outbox websocket.

**Architecture:** sap gains a per-session **JWT-bearer** auth method (an ES256 confidential-client that mints act-as-subject tokens against pear's `/oauth/token`). pear's fosite client wrapper is taught to honor confidential-client metadata. The test orchestrates postgres + CoreDNS (wildcard DNS) + Caddy (wildcard TLS) + toxiproxy (notify chaos) + pear + sap with testcontainers-go, drives pear over its published HTTP port as a JWT-bearer confidential client, and asserts eventual delivery on the outbox websocket.

**Tech Stack:** Go 1.26, testcontainers-go, Caddy, CoreDNS, toxiproxy (`github.com/Shopify/toxiproxy/v2/client`), fosite (ory), go-jose, golang-jwt/jwt/v5, gorilla/websocket, PostgreSQL.

## Global Constraints

- Go 1.26; standard layout (entrypoints in `cmd/`, libraries in `internal/`).
- Follow `.claude/rules/go-conventions.md` and `go-tests.md`: constructors not globals; interface deps; typedefs over raw strings; `require` for assertions; interface fakes not mocks; no `time.Sleep` in unit tests (`synctest` where goroutines are involved); test files in-package for internal tests.
- Never edit generated files (`api/habitat/*`, `typescript/api/*`).
- The integration module (`integration/`) is a **separate Go module** with its own `go.mod`; it is run manually (not in `moon ci`). Add new deps there with `go get` inside `integration/`.
- Domains: pear = `pear.local.habitat.network`; sap = `sap.local.habitat.network`; test client metadata = `testclient.local.habitat.network`. `*.local.habitat.network` resolves to `127.0.0.1` publicly (host side).
- JWT-bearer grant type string: `urn:ietf:params:oauth:grant-type:jwt-bearer`. Confidential-client `token_endpoint_auth_method` = `private_key_jwt`, signing alg = `ES256`.
- Commit after each task with a focused message ending in the `Co-Authored-By` trailer.

## File Structure

**pear (Task 1):**
- Modify `internal/oauthserver/fosite_client.go` — derive `IsPublic()` and auth-method from metadata.
- Modify `internal/oauthserver/jwt_bearer_test.go` — add ES256 confidential-client test.

**sap library (Tasks 2–4):**
- Create `internal/sap/jwtbearer/jwtbearer.go` — ES256 act-as-subject token builder + HTTP client factory.
- Create `internal/sap/jwtbearer/jwtbearer_test.go`.
- Modify `internal/sap/session/session.go` — `AuthMethod` column, per-session dispatch, `JWTBearerClients` seam.
- Modify `internal/sap/session/session_test.go`.
- Modify `internal/sap/sap.go` — `Config.JWTBearer`, thread auth method through `AddSession`.
- Modify `internal/sap/sap_test.go` — update `AddSession` callers.

**sap binary (Tasks 4–5):**
- Modify `cmd/sap/main.go`, `cmd/sap/flags.go`, `cmd/sap/server.go` — build the builder, serve confidential-client metadata, add JWT-bearer session endpoint.
- Create `cmd/sap/Dockerfile`.

**integration test (Tasks 6–8):**
- Create `integration/sap_infra_test.go` — TLS/CA generation + container orchestration.
- Create `integration/sap_client_test.go` — JWT-bearer confidential test client + pear XRPC helpers.
- Create `integration/sap_test.go` — the test: bootstrap, outbox websocket, concurrent workload, chaos, assertions.

---

## Task 1: pear confidential-client support

**Files:**
- Modify: `internal/oauthserver/fosite_client.go`
- Test: `internal/oauthserver/jwt_bearer_test.go`

**Interfaces:**
- Consumes: `pdsclient.ClientMetadata` (fields `TokenEndpointAuthMethod`, `Jwks`), `ApprovedClientStore`.
- Produces: `client.IsPublic()` returns `false` iff `TokenEndpointAuthMethod == "private_key_jwt"`. No signature changes to exported functions.

**Background:** `fosite_client.go`'s `IsPublic()` currently hardcodes `true`. The jwt-bearer grant uses `GrantTypeJWTBearerCanSkipClientAuth: true`, so the grant handler authenticates the client from the assertion's `iss` + signature and skips separate client auth regardless of `IsPublic()`. We make `IsPublic()` reflect the metadata so a confidential client is represented correctly, and prove an **ES256** confidential client still obtains a token via the grant.

- [x] **Step 1: Write the failing test**

Add to `internal/oauthserver/jwt_bearer_test.go`:

```go
// newES256ConfidentialClient serves a spec-compliant atproto confidential-client
// metadata document (private_key_jwt, ES256 JWKS) and returns the client ID plus
// the P-256 signing key.
func newES256ConfidentialClient(t *testing.T) (clientID string, key *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	const keyID = "test-es256"
	var id string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client-metadata.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(&pdsclient.ClientMetadata{
			ClientId:                id,
			ApplicationType:         "web",
			GrantTypes:              []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:jwt-bearer"},
			Scope:                   "atproto",
			ResponseTypes:           []string{"code"},
			RedirectUris:            []string{id + "/callback"},
			TokenEndpointAuthMethod: "private_key_jwt",
			TokenEndpointAuthSigner: "ES256",
			DpopBoundAccessTokens:   true,
			Jwks: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
				Key:       key.Public(),
				KeyID:     keyID,
				Algorithm: string(jose.ES256),
				Use:       "sig",
			}}},
		}))
	}))
	t.Cleanup(server.Close)
	id = server.URL + "/client-metadata.json"
	return id, key
}

func TestHandleTokenJWTBearerES256Confidential(t *testing.T) {
	clientID, key := newES256ConfidentialClient(t)
	clientURL, _ := url.Parse(clientID)
	domain := clientURL.Host
	srv, tokenURL := setupJWTBearerTestServer(t, domain, clientID)

	const subject = "did:web:es256-subject.example"
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": clientID,
		"sub": subject,
		"aud": domain + "/oauth/token",
		"iat": now.Unix(),
		"exp": now.Add(time.Minute).Unix(),
		"jti": "es256-jti-1",
	})
	tok.Header["kid"] = "test-es256"
	assertion, err := tok.SignedString(key)
	require.NoError(t, err)

	resp, err := http.PostForm(tokenURL, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	})
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotEmpty(t, out.AccessToken)

	did, ok, err := srv.ValidateRaw(t.Context(), out.AccessToken)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, subject, did.Subject.String())
}
```

Add imports to the test file as needed: `crypto/ecdsa`, `crypto/elliptic`, `crypto/rand`, `encoding/json`, `net/http`, `net/http/httptest`, `net/url`, `time`, `github.com/golang-jwt/jwt/v5` (aliased `jwt`), `jose "github.com/go-jose/go-jose/v3"`.

- [x] **Step 2: Run the test to see how it behaves today**

Run: `go test ./internal/oauthserver/ -run TestHandleTokenJWTBearerES256Confidential -v`
Expected: either PASS (grant already tolerates ES256 + confidential metadata) or FAIL. If it already PASSES, `IsPublic()` still hardcodes `true` — Step 3 makes the representation correct and adds a guard test. If it FAILS, note the fosite error (`invalid_client` implies the confidential client now needs auth).

- [x] **Step 3: Make `IsPublic()` reflect the metadata**

In `internal/oauthserver/fosite_client.go` replace the `IsPublic` method:

```go
// IsPublic implements fosite.Client. A client that advertises the
// private_key_jwt token-endpoint auth method is confidential; all others
// (including the default empty value) are treated as public.
func (c *client) IsPublic() bool {
	return c.TokenEndpointAuthMethod != "private_key_jwt"
}
```

- [x] **Step 4: Run the ES256 test and the existing jwt-bearer tests**

Run: `go test ./internal/oauthserver/ -run TestHandleTokenJWTBearer -v`
Expected: PASS for both the RS256 public-client tests and the new ES256 confidential test. If the ES256 case fails with `invalid_client`, the grant is demanding separate client auth for confidential clients; in that case additionally implement `GetTokenEndpointAuthMethod()`/`GetTokenEndpointAuthSigningAlgorithm()` on `client` returning the metadata values and confirm `GrantTypeJWTBearerCanSkipClientAuth` still applies — re-run until green.

- [x] **Step 5: Run the full oauthserver package**

Run: `go test ./internal/oauthserver/...`
Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add internal/oauthserver/fosite_client.go internal/oauthserver/jwt_bearer_test.go
git commit -m "oauthserver: honor confidential-client metadata (ES256 jwt-bearer)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: sap session — per-session auth method + dispatch seam

**Files:**
- Modify: `internal/sap/session/session.go`
- Test: `internal/sap/session/session_test.go`

**Interfaces:**
- Produces:
  - `const AuthOAuth = "oauth"`, `const AuthJWTBearer = "jwt-bearer"` (string constants).
  - `type JWTBearerClients interface { ClientForDID(ctx context.Context, did syntax.DID) (*http.Client, error) }`
  - `func NewStore(db *gorm.DB, oauth *oauth.ClientApp, jwt JWTBearerClients) *Store` (jwt may be nil).
  - `func (s *Store) Add(ctx context.Context, did syntax.DID, sessionID, method string) error`
  - `ClientForSession` dispatches on the stored `AuthMethod`.
- Consumes: existing `getter`, `oauth.ClientApp`.

- [x] **Step 1: Write the failing test**

Add to `internal/sap/session/session_test.go`:

```go
type fakeJWTClients struct {
	client *http.Client
	calls  int
}

func (f *fakeJWTClients) ClientForDID(ctx context.Context, did syntax.DID) (*http.Client, error) {
	f.calls++
	return f.client, nil
}

func TestClientForSessionDispatchesJWTBearer(t *testing.T) {
	db := db_testutil.NewDB(t)
	sentinel := &http.Client{}
	jwt := &fakeJWTClients{client: sentinel}
	store := NewStore(db, nil, jwt)

	did := syntax.DID("did:web:member.example")
	require.NoError(t, store.Add(t.Context(), did, "", AuthJWTBearer))

	got, err := store.ClientForSession(t.Context(), did)
	require.NoError(t, err)
	require.Same(t, sentinel, got)
	require.Equal(t, 1, jwt.calls)
}
```

Ensure the test file imports `net/http`, `context`, and `db_testutil "github.com/habitat-network/habitat/internal/db/testutil"` (match existing imports).

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sap/session/ -run TestClientForSessionDispatchesJWTBearer -v`
Expected: FAIL to compile (`NewStore` arity, `Add` arity, `AuthJWTBearer`, `JWTBearerClients` undefined).

- [x] **Step 3: Add the auth-method column and constants**

In `internal/sap/session/session.go`, add the constants near the top of the type declarations and extend the `session` struct:

```go
// Auth methods a session can use to authenticate to its host.
const (
	AuthOAuth     = "oauth"
	AuthJWTBearer = "jwt-bearer"
)
```

Add the field to `session`:

```go
type session struct {
	DID        syntax.DID `gorm:"column:did;primaryKey"`
	SessionID  string     // keys the oauth client's session store; empty for jwt-bearer
	AuthMethod string     // AuthOAuth (default) or AuthJWTBearer
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
```

- [x] **Step 4: Add the JWT seam to the Store and update constructors**

Replace the `Store`, `NewStore`, `WithTx`, and `Add` definitions:

```go
// JWTBearerClients builds an HTTP client that authenticates as a DID via the
// JWT-bearer grant. Satisfied by jwtbearer.Builder; nil when unconfigured.
type JWTBearerClients interface {
	ClientForDID(ctx context.Context, did syntax.DID) (*http.Client, error)
}

type Store struct {
	db     *gorm.DB
	getter *getter
	jwt    JWTBearerClients // may be nil
}

func NewStore(db *gorm.DB, oauth *oauth.ClientApp, jwt JWTBearerClients) *Store {
	return &Store{db: db, getter: newGetter(oauth), jwt: jwt}
}

// WithTx returns a Store scoped to the given transaction.
func (s *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{db: tx, getter: s.getter, jwt: s.jwt}
}

// Add upserts a session for the account with the given auth method.
func (s *Store) Add(ctx context.Context, did syntax.DID, sessionID, method string) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "did"}},
		DoUpdates: clause.AssignmentColumns([]string{"session_id", "auth_method", "updated_at"}),
	}).Create(&session{DID: did, SessionID: sessionID, AuthMethod: method}).Error
}
```

- [x] **Step 5: Dispatch in `ClientForSession`**

Replace `ClientForSession`:

```go
// ClientForSession returns an HTTP client authenticated as the session's
// account against its host, using the session's recorded auth method.
func (s *Store) ClientForSession(ctx context.Context, did syntax.DID) (*http.Client, error) {
	var sess session
	if err := s.db.WithContext(ctx).First(&sess, "did = ?", did).Error; err != nil {
		return nil, fmt.Errorf("load session %s: %w", did, err)
	}
	if sess.AuthMethod == AuthJWTBearer {
		if s.jwt == nil {
			return nil, fmt.Errorf("jwt-bearer client not configured for %s", did)
		}
		return s.jwt.ClientForDID(ctx, did)
	}
	resumed, err := s.getter.resume(ctx, sess.DID, sess.SessionID)
	if err != nil {
		return nil, err
	}
	return resumed.authClient(), nil
}
```

- [x] **Step 6: Run the new and existing session tests**

Run: `go test ./internal/sap/session/ -v`
Expected: PASS. (Existing tests that call `NewStore`/`Add` must be updated to the new signatures — pass `nil` for `jwt` and `AuthOAuth` for method. Update them in this step.)

- [x] **Step 7: Commit**

```bash
git add internal/sap/session/
git commit -m "sap/session: per-session auth method with jwt-bearer dispatch

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: sap jwtbearer builder

**Files:**
- Create: `internal/sap/jwtbearer/jwtbearer.go`
- Test: `internal/sap/jwtbearer/jwtbearer_test.go`

**Interfaces:**
- Produces:
  - `type Directory interface { LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) }` (satisfied by `identity.Directory`).
  - `func New(clientID string, key *ecdsa.PrivateKey, dir Directory) *Builder`
  - `func (b *Builder) ClientForDID(ctx context.Context, did syntax.DID) (*http.Client, error)` — satisfies `session.JWTBearerClients`.
  - `func (b *Builder) PublicJWK() jose.JSONWebKey` — for the served client-metadata JWKS. KeyID is `"habitat"`.
- Consumes: `Directory`, an ES256 P-256 private key.

- [x] **Step 1: Write the failing test**

Create `internal/sap/jwtbearer/jwtbearer_test.go`:

```go
package jwtbearer

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

type fakeDir struct{ hostURL string }

func (f fakeDir) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	return &identity.Identity{
		DID:      did,
		Services: map[string]identity.Service{"habitat": {Type: "HabitatServer", URL: f.hostURL}},
	}, nil
}

func TestClientForDIDMintsAndAttachesToken(t *testing.T) {
	var tokenRequests int64
	var seenAuth, seenMethod string

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&tokenRequests, 1)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", r.Form.Get("grant_type"))
		require.NotEmpty(t, r.Form.Get("assertion"))
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok-123", "expires_in": 3600})
	})
	mux.HandleFunc("/xrpc/network.habitat.space.listSpaces", func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenMethod = r.Header.Get("Habitat-Auth-Method")
		_ = json.NewEncoder(w).Encode(map[string]any{"spaces": []any{}})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	b := New("https://sap.example/client-metadata.json", key, fakeDir{hostURL: server.URL})

	did := syntax.DID("did:web:member.example")
	client, err := b.ClientForDID(t.Context(), did)
	require.NoError(t, err)

	for range 3 {
		resp, err := client.Get("/xrpc/network.habitat.space.listSpaces")
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		_ = resp.Body.Close()
	}

	require.Equal(t, "Bearer tok-123", seenAuth)
	require.Equal(t, "oauth", seenMethod)
	require.Equal(t, int64(1), atomic.LoadInt64(&tokenRequests), "token should be cached across requests")
	require.True(t, strings.HasPrefix(b.PublicJWK().Algorithm, "ES256"))
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sap/jwtbearer/ -run TestClientForDIDMintsAndAttachesToken -v`
Expected: FAIL (package/`New` undefined).

- [x] **Step 3: Implement the builder**

Create `internal/sap/jwtbearer/jwtbearer.go`:

```go
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
```

- [x] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/sap/jwtbearer/ -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/sap/jwtbearer/
git commit -m "sap/jwtbearer: ES256 act-as-subject JWT-bearer client factory

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: Wire JWT-bearer into sap + cmd/sap

**Files:**
- Modify: `internal/sap/sap.go`
- Modify: `internal/sap/sap_test.go`
- Modify: `cmd/sap/flags.go`
- Modify: `cmd/sap/main.go`
- Modify: `cmd/sap/server.go`

**Interfaces:**
- Consumes: `session.NewStore(db, oauth, jwt)`, `session.JWTBearerClients`, `session.AuthOAuth`, `session.AuthJWTBearer`, `jwtbearer.New`, `jwtbearer.Builder.PublicJWK`.
- Produces:
  - `Config.JWTBearer session.JWTBearerClients` (nil-able).
  - `func (s *Sap) AddSession(ctx context.Context, did syntax.DID, sessionID, method string) error`
  - cmd/sap serves a confidential-client `client-metadata.json`; new internal endpoint `POST /session/jwt` adds a `jwt-bearer` session from `{"did": "..."}`.

- [x] **Step 1: Thread the auth method and JWT config through the library**

In `internal/sap/sap.go`:

Add to `Config`:

```go
	// JWTBearer, when set, lets sessions authenticate to their host via the
	// JWT-bearer grant instead of OAuth. Chosen per session (see AddSession).
	JWTBearer session.JWTBearerClients
```

Change the `session.NewStore` call in `New`:

```go
	sessions := session.NewStore(config.DB, config.OAuthClient, config.JWTBearer)
```

Change `AddSession`:

```go
// AddSession registers an authenticated session and kicks off its backfill
// crawl. method is session.AuthOAuth or session.AuthJWTBearer.
func (s *Sap) AddSession(ctx context.Context, did syntax.DID, sessionID, method string) error {
	if err := s.sessions.Add(ctx, did, sessionID, method); err != nil {
		return fmt.Errorf("add session: %w", err)
	}
	go s.crawler.Run(detachSpan(ctx), did)
	return nil
}
```

Add the import `"github.com/habitat-network/habitat/internal/sap/session"` if not present.

- [x] **Step 2: Update the in-process sap test callers**

In `internal/sap/sap_test.go`, change the `AddSession` call to pass the OAuth method:

```go
	require.NoError(t, s.AddSession(t.Context(), author, "sess1", session.AuthOAuth))
```

Add the `session` import. Run: `go test ./internal/sap/ -run TestSap -v` — Expected: PASS (unchanged behavior; jwt config nil).

- [x] **Step 3: Add the signing-key flag**

In `cmd/sap/flags.go`, add a flag var and entry:

```go
	fJwtSigningKey = "jwt-signing-key"
```

```go
		&cli.StringFlag{
			Name:    fJwtSigningKey,
			Usage:   "Base64-encoded raw P-256 (ES256) private key for the JWT-bearer confidential client. Generated ephemerally if empty.",
			Sources: cli.EnvVars("SAP_JWT_SIGNING_KEY"),
		},
```

- [x] **Step 4: Build the builder and confidential-client metadata in main**

In `cmd/sap/main.go`, after `domain` is set and before `sap.New`, construct the signing key and builder:

```go
	signingKey, err := loadOrGenerateES256(cmd.String(fJwtSigningKey))
	if err != nil {
		return fmt.Errorf("load jwt signing key: %w", err)
	}
	clientID := "https://" + domain + "/client-metadata.json"
	jwtBuilder := jwtbearer.New(clientID, signingKey, dir)
```

Pass it into the config:

```go
	s, err := sap.New(sap.Config{
		DB:          db,
		OAuthClient: oauthApp,
		Directory:   dir,
		Endpoint:    endpoint,
		JWTBearer:   jwtBuilder,
		Meter:       otel.Meter("sap"),
		Tracer:      otel.Tracer("sap"),
	})
```

Add the helper at the bottom of `main.go`:

```go
func loadOrGenerateES256(encoded string) (*ecdsa.PrivateKey, error) {
	if encoded == "" {
		return ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}
	return ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
}
```

Pass the builder into the server constructor so it can serve metadata (see Step 5). Add imports: `crypto/ecdsa`, `crypto/elliptic`, `crand "crypto/rand"`, `encoding/base64`, and `"github.com/habitat-network/habitat/internal/sap/jwtbearer"`.

- [x] **Step 5: Serve confidential-client metadata and add the JWT session endpoint**

In `cmd/sap/server.go`, add the builder to `server` and constructor:

```go
type server struct {
	sap         *sap.Sap
	oauthClient *oauth.ClientApp
	jwt         *jwtbearer.Builder
	serviceAuth *auth.ServiceAuthValidator
}

func NewSapServer(
	sapInstance *sap.Sap,
	oauthClient *oauth.ClientApp,
	jwt *jwtbearer.Builder,
	serviceAuth *auth.ServiceAuthValidator,
) *server {
	return &server{sap: sapInstance, oauthClient: oauthClient, jwt: jwt, serviceAuth: serviceAuth}
}
```

Replace `handleClientMetadata` to serve the confidential-client document:

```go
// handleClientMetadata serves sap's atproto confidential-client metadata
// document, used by hosts to fetch sap's JWKS when validating its JWT-bearer
// assertions. See https://atproto.com/specs/oauth.
func (s *server) handleClientMetadata(w http.ResponseWriter, r *http.Request) {
	clientID := "https://" + s.domain + "/client-metadata.json"
	httpx.WriteJSON(r.Context(), w, &pdsclient.ClientMetadata{
		ClientId:                clientID,
		ClientName:              "Habitat Sap",
		ClientUri:               "https://" + s.domain,
		ApplicationType:         "web",
		GrantTypes:              []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:jwt-bearer"},
		Scope:                   "atproto",
		ResponseTypes:           []string{"code"},
		RedirectUris:            []string{"https://" + s.domain + "/oauth-callback"},
		TokenEndpointAuthMethod: "private_key_jwt",
		TokenEndpointAuthSigner: "ES256",
		DpopBoundAccessTokens:   true,
		Jwks:                    &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{s.jwt.PublicJWK()}},
	})
}
```

Add a `domain string` field to `server` (set it in `NewSapServer` — add a `domain` parameter) OR pass the full clientID; simplest is to add a `domain` field and parameter. Add the JWT session handler:

```go
// handleAddJWTSession registers a jwt-bearer session for a DID directly (no
// OAuth redirect); sap authenticates to the DID's host via the JWT-bearer grant.
func (s *server) handleAddJWTSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DID string `json:"did"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	did, ok := httpx.ParseDIDInput(r.Context(), w, req.DID, "did")
	if !ok {
		return
	}
	if err := s.sap.AddSession(r.Context(), did, "", session.AuthJWTBearer); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
```

Update `handleOAuthCallback` to pass the method:

```go
	if err := s.sap.AddSession(
		r.Context(), sessionData.AccountDID, sessionData.SessionID, session.AuthOAuth,
	); err != nil {
```

Add imports to `server.go`: `jose "github.com/go-jose/go-jose/v3"`, `"github.com/habitat-network/habitat/internal/pdsclient"`, `"github.com/habitat-network/habitat/internal/sap/jwtbearer"`, `"github.com/habitat-network/habitat/internal/sap/session"`.

- [x] **Step 6: Wire the new server params and route in main**

In `cmd/sap/main.go`, update the `NewSapServer` call to pass the builder and domain, and register the route on the internal mux:

```go
	server := NewSapServer(s, oauthApp, jwtBuilder, domain, &auth.ServiceAuthValidator{
		Dir:      dir,
		Audience: endpoint,
	})
```

```go
	internalMux.HandleFunc("/session/jwt", server.handleAddJWTSession)
```

(Match `NewSapServer`'s parameter order to whatever you defined in Step 5; include `domain`.)

- [x] **Step 7: Build and run sap unit tests**

Run: `go build ./cmd/sap/... && go test ./internal/sap/...`
Expected: build succeeds; tests PASS.

- [x] **Step 8: Commit**

```bash
git add cmd/sap/ internal/sap/sap.go internal/sap/sap_test.go
git commit -m "cmd/sap: serve confidential-client metadata; add jwt-bearer sessions

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: sap Dockerfile

**Files:**
- Create: `cmd/sap/Dockerfile`

- [x] **Step 1: Write the Dockerfile**

Create `cmd/sap/Dockerfile` (modeled on `cmd/pear/Dockerfile`, without the UI stage):

```dockerfile
#### BUILD STAGE
FROM golang:1.26-alpine AS build
WORKDIR /app

# Copy go module files first for better layer caching
COPY go.mod go.sum* ./
COPY cmd/sap/go.mod cmd/sap/go.sum ./cmd/sap/

WORKDIR /app/cmd/sap
RUN go mod download

# Copy the entire repository (needed for the replace directive)
COPY . /app/

RUN go build -o /app/bin/sap .

FROM alpine:latest
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY --from=build /app/bin/sap /app/sap
CMD ["/app/sap"]
```

- [x] **Step 2: Verify it builds**

Run: `docker build -f cmd/sap/Dockerfile -t sap:test .`
Expected: image builds successfully.

- [x] **Step 3: Commit**

```bash
git add cmd/sap/Dockerfile
git commit -m "cmd/sap: add Dockerfile

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: Integration infra — TLS, DNS, and container orchestration

**Files:**
- Create: `integration/sap_infra_test.go`

**Interfaces:**
- Produces (all in package `integration`):
  - `type sapStack struct { PearHTTP string; SapOutboxWS string; Toxiproxy *toxiclient.Client; NotifyProxy *toxiclient.Proxy; SapClientID string; TestClientID string; TestClientKey *ecdsa.PrivateKey; PearBaseURL string }`
  - `func startSapStack(ctx context.Context, t *testing.T) *sapStack`
  - `PearBaseURL` = `https://pear.local.habitat.network`; `PearHTTP` = `http://127.0.0.1:<published 8000>` (test → pear); `SapOutboxWS` = `ws://127.0.0.1:<published 2581>/channel`.

**Notes on approach:**
- The test process talks to pear over pear's **published HTTP port** (caddy is only for in-container TLS traffic), so no host `:443` is needed and there is no conflict with a running dev Caddy.
- One self-signed CA signs a leaf cert whose SANs cover `local.habitat.network`, `*.local.habitat.network`, `*.pear.local.habitat.network`, `*.*.pear.local.habitat.network`. Caddy serves it; pear and sap trust the CA via `SSL_CERT_FILE`.
- CoreDNS answers `*.local.habitat.network → caddy IP` and forwards everything else to the container's Docker resolver (`127.0.0.11`) so service aliases still resolve. pear/sap use CoreDNS as their only resolver.

- [x] **Step 1: Add integration dependencies**

Run inside `integration/`:
```bash
cd integration
go get github.com/Shopify/toxiproxy/v2/client@latest
go get github.com/testcontainers/testcontainers-go@latest
go get github.com/gorilla/websocket@latest
```
Expected: `go.mod`/`go.sum` updated.

- [x] **Step 2: TLS CA + leaf generation helper**

Create `integration/sap_infra_test.go` starting with the package and a cert helper:

```go
package integration

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	toxiclient "github.com/Shopify/toxiproxy/v2/client"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/docker/docker/api/types/container"
)

// writeTLS generates a CA and a leaf cert covering the habitat test domains,
// writes ca.pem, leaf.crt, leaf.key into dir, and returns the CA PEM bytes.
func writeTLS(t *testing.T, dir string) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "habitat-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "*.local.habitat.network"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{
			"local.habitat.network",
			"*.local.habitat.network",
			"*.pear.local.habitat.network",
			"*.*.pear.local.habitat.network",
		},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caTmpl, &leafKey.PublicKey, caKey)
	require.NoError(t, err)
	leafKeyDER, err := x509.MarshalECPrivateKey(leafKey)
	require.NoError(t, err)

	write := func(name string, blocks ...*pem.Block) {
		f, err := os.Create(filepath.Join(dir, name))
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		for _, b := range blocks {
			require.NoError(t, pem.Encode(f, b))
		}
		require.NoError(t, os.Chmod(filepath.Join(dir, name), 0o644))
	}
	write("ca.pem", &pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	write("leaf.crt", &pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	write("leaf.key", &pem.Block{Type: "EC PRIVATE KEY", Bytes: leafKeyDER})
}
```

- [x] **Step 3: Caddyfile + Corefile writers**

Append helpers that write the proxy configs into the shared dir. `testClientMetadata` is the JSON the test client serves (built in Task 7; here we accept it as a string).

```go
func writeCaddyfile(t *testing.T, dir string) {
	t.Helper()
	content := `{
	auto_https off
}

pear.local.habitat.network, *.pear.local.habitat.network, *.*.pear.local.habitat.network {
	tls /certs/leaf.crt /certs/leaf.key
	reverse_proxy pear:8000
}

sap.local.habitat.network {
	tls /certs/leaf.crt /certs/leaf.key
	@notify path /xrpc/network.habitat.space.notifyWrite
	reverse_proxy @notify toxiproxy:8666
	reverse_proxy sap:2580
}

testclient.local.habitat.network {
	tls /certs/leaf.crt /certs/leaf.key
	handle /client-metadata.json {
		root * /certs
		rewrite * /testclient-metadata.json
		file_server
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Caddyfile"), []byte(content), 0o644))
}

func writeCorefile(t *testing.T, dir, caddyIP string) {
	t.Helper()
	content := fmt.Sprintf(`local.habitat.network:53 {
	template IN A {
		match ".*\\.?local\\.habitat\\.network\\.$"
		answer "{{ .Name }} 60 IN A %s"
	}
}
.:53 {
	forward . 127.0.0.11
}
`, caddyIP)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Corefile"), []byte(content), 0o644))
}
```

- [x] **Step 4: Orchestration — `startSapStack`**

Append the orchestrator. It creates the network, generates TLS, starts caddy, derives its IP, starts CoreDNS, postgres, toxiproxy, pear, sap, then configures the notify proxy. `pearEnv`/`sapEnv` are built with the required flags. `certsDir` is bind-mounted read-only into caddy at `/certs` and used for `SSL_CERT_FILE` into pear/sap.

```go
func startSapStack(ctx context.Context, t *testing.T) *sapStack {
	t.Helper()
	certsDir := t.TempDir()
	writeTLS(t, certsDir)
	writeCaddyfile(t, certsDir)

	net, err := network.New(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = net.Remove(ctx) })
	netName := net.Name

	// Test client + sap client IDs (metadata served by caddy / sap respectively).
	testClientID := "https://testclient.local.habitat.network/client-metadata.json"
	sapClientID := "https://sap.local.habitat.network/client-metadata.json"

	// The test client's ES256 key + served metadata document.
	testKey, testMetaJSON := buildTestClientMetadata(t, testClientID)
	require.NoError(t, os.WriteFile(filepath.Join(certsDir, "testclient-metadata.json"), testMetaJSON, 0o644))

	bindCerts := func(hc *container.HostConfig) {
		hc.Binds = append(hc.Binds, certsDir+":/certs:ro")
	}

	// 1. Caddy.
	caddy := mustStart(ctx, t, testcontainers.ContainerRequest{
		Image:          "caddy:2-alpine",
		Networks:       []string{netName},
		NetworkAliases: map[string][]string{netName: {"caddy"}},
		Cmd:            []string{"caddy", "run", "--config", "/certs/Caddyfile", "--adapter", "caddyfile"},
		HostConfigModifier: bindCerts,
		WaitingFor:     wait.ForListeningPort("443/tcp"),
	})
	caddyIP, err := caddy.ContainerIP(ctx)
	require.NoError(t, err)

	// 2. CoreDNS (needs caddy IP).
	writeCorefile(t, certsDir, caddyIP)
	coredns := mustStart(ctx, t, testcontainers.ContainerRequest{
		Image:          "coredns/coredns:1.11.1",
		Networks:       []string{netName},
		NetworkAliases: map[string][]string{netName: {"coredns"}},
		Cmd:            []string{"-conf", "/certs/Corefile"},
		HostConfigModifier: bindCerts,
		WaitingFor:     wait.ForListeningPort("53/tcp"),
	})
	corednsIP, err := coredns.ContainerIP(ctx)
	require.NoError(t, err)
	useDNS := func(hc *container.HostConfig) { hc.DNS = []string{corednsIP} }

	// 3. Postgres (+ create the sap database).
	pgURL, pgHostURL := startPostgres(ctx, t, netName)

	// 4. Toxiproxy.
	toxi := mustStart(ctx, t, testcontainers.ContainerRequest{
		Image:          "ghcr.io/shopify/toxiproxy:2.9.0",
		Networks:       []string{netName},
		NetworkAliases: map[string][]string{netName: {"toxiproxy"}},
		ExposedPorts:   []string{"8474/tcp"},
		WaitingFor:     wait.ForListeningPort("8474/tcp"),
	})
	toxiHost, err := toxi.PortEndpoint(ctx, "8474/tcp", "http")
	require.NoError(t, err)
	toxiClient := toxiclient.NewClient(toxiHost)

	// 5. pear.
	pearEnv := pearEnv(t, pgURL, sapClientID, testClientID)
	pear := mustStart(ctx, t, testcontainers.ContainerRequest{
		Image:          buildImage(ctx, t, "cmd/pear/Dockerfile", "pear:itest"),
		Networks:       []string{netName},
		NetworkAliases: map[string][]string{netName: {"pear"}},
		Env:            pearEnv,
		ExposedPorts:   []string{"8000/tcp"},
		HostConfigModifier: func(hc *container.HostConfig) { useDNS(hc); bindCerts(hc) },
		WaitingFor: wait.ForHTTP("/health").WithPort("8000/tcp").WithStartupTimeout(90 * time.Second),
	})
	pearHTTP, err := pear.PortEndpoint(ctx, "8000/tcp", "http")
	require.NoError(t, err)

	// 6. sap.
	sap := mustStart(ctx, t, testcontainers.ContainerRequest{
		Image:          buildImage(ctx, t, "cmd/sap/Dockerfile", "sap:itest"),
		Networks:       []string{netName},
		NetworkAliases: map[string][]string{netName: {"sap"}},
		Env:            sapEnv(pgHostURL),
		ExposedPorts:   []string{"2581/tcp"},
		HostConfigModifier: func(hc *container.HostConfig) { useDNS(hc); bindCerts(hc) },
		WaitingFor: wait.ForHTTP("/health").WithPort("2581/tcp").WithStartupTimeout(90 * time.Second),
	})
	sapWSHost, err := sap.PortEndpoint(ctx, "2581/tcp", "")
	require.NoError(t, err)

	// 7. Configure the notify proxy: caddy → toxiproxy:8666 → sap:2580.
	notifyProxy, err := toxiClient.CreateProxy("sap_notify", "0.0.0.0:8666", "sap:2580")
	require.NoError(t, err)

	return &sapStack{
		PearBaseURL:   "https://pear.local.habitat.network",
		PearHTTP:      pearHTTP,
		SapOutboxWS:   "ws://" + sapWSHost + "/channel",
		Toxiproxy:     toxiClient,
		NotifyProxy:   notifyProxy,
		SapClientID:   sapClientID,
		TestClientID:  testClientID,
		TestClientKey: testKey,
	}
}
```

- [x] **Step 5: Small helpers (`mustStart`, `buildImage`, `startPostgres`, env builders)**

Append:

```go
type sapStack struct {
	PearBaseURL   string
	PearHTTP      string
	SapOutboxWS   string
	Toxiproxy     *toxiclient.Client
	NotifyProxy   *toxiclient.Proxy
	SapClientID   string
	TestClientID  string
	TestClientKey *ecdsa.PrivateKey
}

func mustStart(ctx context.Context, t *testing.T, req testcontainers.ContainerRequest) testcontainers.Container {
	t.Helper()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req, Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	t.Cleanup(func() { dumpLogs(t, c) })
	return c
}

func dumpLogs(t *testing.T, c testcontainers.Container) {
	rc, err := c.Logs(context.Background())
	if err != nil {
		return
	}
	defer func() { _ = rc.Close() }()
	buf := make([]byte, 1<<20)
	n, _ := rc.Read(buf)
	if n > 0 {
		name, _ := c.Name(context.Background())
		t.Logf("=== logs %s ===\n%s", name, string(buf[:n]))
	}
}

// buildImage builds an image from a repo Dockerfile with the repo root as the
// context, once per tag.
func buildImage(ctx context.Context, t *testing.T, dockerfile, tag string) string {
	t.Helper()
	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			FromDockerfile: testcontainers.FromDockerfile{
				Context:    "..",
				Dockerfile: dockerfile,
				Repo:       tag, // built and kept; not started here
				KeepImage:  true,
			},
		},
	}
	// Build only.
	c, err := testcontainers.GenericContainer(ctx, req)
	require.NoError(t, err)
	_ = c.Terminate(ctx)
	return tag
}
```

Note: if `FromDockerfile` build-then-return-tag proves awkward in the installed testcontainers version, build the two images once in `TestMain` via `docker build` (exec) and just reference the tags in the requests. Choose whichever the installed version supports; the tags consumed downstream are `pear:itest` and `sap:itest`.

Append postgres + env builders:

```go
func startPostgres(ctx context.Context, t *testing.T, netName string) (inNet, hostForSap string) {
	t.Helper()
	pg := mustStart(ctx, t, testcontainers.ContainerRequest{
		Image:          "postgres:16-alpine",
		Networks:       []string{netName},
		NetworkAliases: map[string][]string{netName: {"postgres"}},
		Env: map[string]string{
			"POSTGRES_USER":     "habitat",
			"POSTGRES_PASSWORD": "habitat",
			"POSTGRES_DB":       "habitat",
		},
		WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(60 * time.Second),
	})
	// Create the sap database.
	_, _, err := pg.Exec(ctx, []string{"psql", "-U", "habitat", "-d", "habitat", "-c", "CREATE DATABASE sap"})
	require.NoError(t, err)
	return "postgres://habitat:habitat@postgres:5432/habitat?sslmode=disable",
		"postgres://habitat:habitat@postgres:5432/sap?sslmode=disable"
}

func pearEnv(t *testing.T, pgURL, sapClientID, testClientID string) map[string]string {
	t.Helper()
	return map[string]string{
		"HABITAT_DOMAIN":               "pear.local.habitat.network",
		"HABITAT_PORT":                 "8000",
		"HABITAT_DB":                   pgURL,
		"HABITAT_PGURL":                pgURL,
		"HABITAT_PDS_CRED_ENCRYPT_KEY": genKey32(t),
		"HABITAT_OAUTH_SERVER_SECRET":  genKey32(t),
		"HABITAT_OAUTH_CLIENT_SECRET":  genKey32(t),
		"HABITAT_SPACE_SIGNING_KEY":    genP256Multibase(t),
		"HABITAT_BUILTIN_APP":          sapClientID + "," + testClientID,
		"SSL_CERT_FILE":                "/certs/ca.pem",
	}
}

func sapEnv(pgURL string) map[string]string {
	return map[string]string{
		"SAP_DOMAIN":        "sap.local.habitat.network",
		"SAP_PORT":          "2580",
		"SAP_INTERNAL_PORT": "2581",
		"SAP_DB":            pgURL,
		"SAP_LOG_LEVEL":     "debug",
		"SSL_CERT_FILE":     "/certs/ca.pem",
	}
}
```

`genKey32` and `genP256Multibase` produce a base64 32-byte key and an atcrypto multibase P-256 key respectively (implement with `encrypt.GenerateKey` and `atcrypto.GeneratePrivateKeyP256().Multibase()` — add those imports).

- [x] **Step 6: Compile the infra file**

Run: `cd integration && go vet ./...`
Expected: compiles (the file references `buildTestClientMetadata`, added in Task 7; temporarily stub it returning `(nil, nil)` to compile this task, then implement in Task 7). Prefer to implement Task 7's `buildTestClientMetadata` before running the full test.

- [x] **Step 7: Commit**

```bash
git add integration/sap_infra_test.go integration/go.mod integration/go.sum
git commit -m "integration: sap stack orchestration (caddy, coredns, toxiproxy, pg)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7: Integration test client + bootstrap helpers

**Files:**
- Create: `integration/sap_client_test.go`

**Interfaces:**
- Produces (package `integration`):
  - `func buildTestClientMetadata(t *testing.T, clientID string) (*ecdsa.PrivateKey, []byte)` — ES256 key + confidential-client metadata JSON served by caddy.
  - `type pearClient struct { ... }` with `func newPearClient(base, clientID string, key *ecdsa.PrivateKey) *pearClient` and method `func (c *pearClient) xrpc(ctx, method, subjectDID string, in any, out any) error` that mints a JWT-bearer token for `subjectDID` and calls `POST/GET base/xrpc/method`.
  - Bootstrap helpers: `createOrg`, `issueInvite`, `mintMember`, `createSpace`, `putRecord` (thin wrappers over `xrpc`).
- Consumes: `sapStack.PearHTTP`, `PearBaseURL`, `TestClientKey`, `TestClientID`.

**Notes:** the client mints its own ES256 assertions (aud = `PearBaseURL + "/oauth/token"`), posts them to `PearHTTP/oauth/token` (published port), caches tokens per subject, and sets `Habitat-Auth-Method: oauth` on every XRPC call. The `Host` header on requests is set to `pear.local.habitat.network` so any host-aware logic sees the canonical host.

- [x] **Step 1: Test-client metadata builder**

Create `integration/sap_client_test.go`:

```go
package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
)

func buildTestClientMetadata(t *testing.T, clientID string) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	meta := &pdsclient.ClientMetadata{
		ClientId:                clientID,
		ClientName:              "Habitat Integration Test Client",
		ApplicationType:         "web",
		GrantTypes:              []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:jwt-bearer"},
		Scope:                   "atproto",
		ResponseTypes:           []string{"code"},
		RedirectUris:            []string{"https://testclient.local.habitat.network/callback"},
		TokenEndpointAuthMethod: "private_key_jwt",
		TokenEndpointAuthSigner: "ES256",
		DpopBoundAccessTokens:   true,
		Jwks: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key:       key.Public(),
			KeyID:     "test-client",
			Algorithm: string(jose.ES256),
			Use:       "sig",
		}}},
	}
	b, err := json.Marshal(meta)
	require.NoError(t, err)
	return key, b
}
```

- [x] **Step 2: JWT-bearer pear client**

Append:

```go
type pearClient struct {
	httpBase string // published http endpoint (e.g. http://127.0.0.1:PORT)
	issuer   string // canonical https base for aud (https://pear.local.habitat.network)
	clientID string
	key      *ecdsa.PrivateKey
	http     *http.Client

	mu     sync.Mutex
	tokens map[string]string
}

func newPearClient(httpBase, issuer, clientID string, key *ecdsa.PrivateKey) *pearClient {
	return &pearClient{
		httpBase: strings.TrimSuffix(httpBase, "/"),
		issuer:   strings.TrimSuffix(issuer, "/"),
		clientID: clientID,
		key:      key,
		http:     &http.Client{Timeout: 30 * time.Second},
		tokens:   map[string]string{},
	}
}

func (c *pearClient) token(ctx context.Context, subject string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tok, ok := c.tokens[subject]; ok {
		return tok, nil
	}
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": c.clientID,
		"sub": subject,
		"aud": c.issuer + "/oauth/token",
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
		"jti": randJTI(),
	})
	tok.Header["kid"] = "test-client"
	assertion, err := tok.SignedString(c.key)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.httpBase+"/oauth/token", strings.NewReader(form.Encode()))
	req.Host = "pear.local.habitat.network"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token status %d: %s", resp.StatusCode, body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	c.tokens[subject] = out.AccessToken
	return out.AccessToken, nil
}

// xrpc calls an XRPC method as subject. GET when in is nil, else POST JSON.
func (c *pearClient) xrpc(ctx context.Context, method, subject string, in, out any) error {
	var body io.Reader
	httpMethod := http.MethodGet
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
		httpMethod = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, httpMethod, c.httpBase+"/xrpc/"+method, body)
	if err != nil {
		return err
	}
	req.Host = "pear.local.habitat.network"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Habitat-Auth-Method", "oauth")
	if subject != "" {
		tok, err := c.token(ctx, subject)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s status %d: %s", method, resp.StatusCode, b)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func randJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
```

- [x] **Step 3: Bootstrap wrappers**

Append thin helpers over `xrpc` for the flows used by the test. `createOrg` is unauthenticated (`subject == ""`):

```go
func (c *pearClient) createOrg(ctx context.Context, name, adminHandle string) (habitat.NetworkHabitatOrgCreateOutput, error) {
	var out habitat.NetworkHabitatOrgCreateOutput
	err := c.xrpc(ctx, "network.habitat.org.create", "", habitat.NetworkHabitatOrgCreateInput{
		Name:         name,
		AdminHandle:  adminHandle,
		LoginMethod:  "password",
		AdminPassword: "password1234",
		ContactEmail: "test@example.com",
	}, &out)
	return out, err
}

func (c *pearClient) issueInvite(ctx context.Context, adminDID string) (string, error) {
	var out habitat.NetworkHabitatOrgIssueInviteTokenOutput
	err := c.xrpc(ctx, "network.habitat.org.issueInviteToken", adminDID,
		habitat.NetworkHabitatOrgIssueInviteTokenInput{Reusable: true}, &out)
	return out.Token, err
}

func (c *pearClient) mintMember(ctx context.Context, orgID, token, handle string) (string, error) {
	var out habitat.NetworkHabitatOrgMintMemberIdentityOutput
	err := c.xrpc(ctx, "network.habitat.org.mintMemberIdentity", "",
		habitat.NetworkHabitatOrgMintMemberIdentityInput{OrgId: orgID, Token: token, Handle: handle}, &out)
	return out.Did, err
}

func (c *pearClient) createSpace(ctx context.Context, member, spaceType, skey string) (string, error) {
	var out habitat.NetworkHabitatSpaceCreateSpaceOutput
	err := c.xrpc(ctx, "network.habitat.space.createSpace", member,
		habitat.NetworkHabitatSpaceCreateSpaceInput{Type: spaceType, Skey: skey}, &out)
	return out.Uri, err
}

func (c *pearClient) putRecord(ctx context.Context, member, space, collection, rkey string, record map[string]any) (string, error) {
	var out habitat.NetworkHabitatSpacePutRecordOutput
	err := c.xrpc(ctx, "network.habitat.space.putRecord", member,
		habitat.NetworkHabitatSpacePutRecordInput{
			Space: space, Repo: member, Collection: collection, Rkey: rkey, Record: record,
		}, &out)
	return out.Uri, err
}
```

- [x] **Step 4: Compile**

Run: `cd integration && go vet ./...`
Expected: compiles (remove any temporary stub of `buildTestClientMetadata`).

- [x] **Step 5: Commit**

```bash
git add integration/sap_client_test.go
git commit -m "integration: JWT-bearer confidential test client + bootstrap helpers

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 8: The integration test — sync under concurrency and notify chaos

**Files:**
- Create: `integration/sap_test.go`

**Interfaces:**
- Consumes: `startSapStack`, `newPearClient`, bootstrap helpers, `sapStack` fields, `github.com/gorilla/websocket`, toxiproxy client.

**Behavior:** bootstrap org + 3 members + spaces; register a jwt-bearer sap session per member; open the outbox websocket and drain+ack; run concurrent writers (creates + updates) while injecting notify toxics; assert every created URI is delivered exactly once.

- [x] **Step 1: Outbox websocket drainer**

Create `integration/sap_test.go`:

```go
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	toxiclient "github.com/Shopify/toxiproxy/v2/client"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type outboxMsg struct {
	ID    uint            `json:"id"`
	URI   string          `json:"uri"`
	Value json.RawMessage `json:"value"`
}

// drainOutbox connects to sap's outbox websocket, acks every message, and
// records delivered URIs (deduplicated) until ctx is cancelled.
func drainOutbox(ctx context.Context, t *testing.T, wsURL string, seen *sync.Map, count *int64) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	require.NoError(t, err)
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	for {
		var msg outboxMsg
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		if _, loaded := seen.LoadOrStore(msg.URI, true); !loaded {
			addInt64(count, 1)
		}
		_ = conn.WriteJSON(map[string]uint{"id": msg.ID})
	}
}
```

(Provide `addInt64` via `sync/atomic` or use `atomic.Int64` directly; adjust types accordingly.)

- [x] **Step 2: The test body — bootstrap + register sessions**

Append:

```go
func TestSapSyncsPearRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	stack := startSapStack(ctx, t)

	pc := newPearClient(stack.PearHTTP, stack.PearBaseURL, stack.TestClientID, stack.TestClientKey)

	// Bootstrap: org + admin, then two more members (3 repo owners total).
	org, err := pc.createOrg(ctx, "itest-org", "admin")
	require.NoError(t, err)
	members := []string{org.AdminDid}
	for _, h := range []string{"alice", "bob"} {
		token, err := pc.issueInvite(ctx, org.AdminDid)
		require.NoError(t, err)
		did, err := pc.mintMember(ctx, org.OrgId, token, h)
		require.NoError(t, err)
		members = append(members, did)
	}

	// Each member creates two spaces (owned by the org, authored by the member).
	type repoSpace struct{ member, space string }
	var spaces []repoSpace
	for _, m := range members {
		for i := range 2 {
			uri, err := pc.createSpace(ctx, m, "network.habitat.group", fmt.Sprintf("space-%s-%d", m[len(m)-6:], i))
			require.NoError(t, err)
			spaces = append(spaces, repoSpace{member: m, space: uri})
		}
	}

	// Register a jwt-bearer sap session per member.
	for _, m := range members {
		registerSapSession(ctx, t, stack, m)
	}
```

- [x] **Step 3: Outbox drain + concurrent workload with notify chaos**

Continue the test body:

```go
	created := &sync.Map{}
	var expected int64
	seen := &sync.Map{}
	var delivered int64

	drainCtx, stopDrain := context.WithCancel(ctx)
	defer stopDrain()
	go drainOutbox(drainCtx, t, stack.SapOutboxWS, seen, &delivered)

	const collection = "network.habitat.test"
	// Degrade the notify path partway through, then heal it.
	go func() {
		time.Sleep(2 * time.Second)
		_, _ = stack.NotifyProxy.AddToxic("latency", "latency", "downstream", 1.0, toxiclient.Attributes{"latency": 800, "jitter": 400})
		time.Sleep(3 * time.Second)
		_ = stack.NotifyProxy.RemoveToxic("latency")
		_, _ = stack.NotifyProxy.AddToxic("timeout", "timeout", "downstream", 1.0, toxiclient.Attributes{"timeout": 500})
		time.Sleep(3 * time.Second)
		_ = stack.NotifyProxy.RemoveToxic("timeout")
	}()

	// Concurrent writers: creates and updates across all repos.
	var wg sync.WaitGroup
	for _, rs := range spaces {
		for w := range 10 {
			wg.Add(1)
			go func(rs repoSpace, w int) {
				defer wg.Done()
				rkey := fmt.Sprintf("rec-%d", w)
				uri, err := pc.putRecord(ctx, rs.member, rs.space, collection, rkey, map[string]any{"n": w})
				if err != nil {
					t.Errorf("putRecord: %v", err)
					return
				}
				if _, loaded := created.LoadOrStore(uri, true); !loaded {
					addInt64(&expected, 1)
				}
				// Update the same record once.
				if _, err := pc.putRecord(ctx, rs.member, rs.space, collection, rkey, map[string]any{"n": w, "v": 2}); err != nil {
					t.Errorf("update putRecord: %v", err)
				}
			}(rs, w)
		}
	}
	wg.Wait()
```

- [x] **Step 4: Assert eventual delivery**

Continue and close the test:

```go
	require.Eventually(t, func() bool {
		got := loadInt64(&delivered)
		t.Logf("outbox delivered %d / expected %d", got, loadInt64(&expected))
		return got == loadInt64(&expected)
	}, 60*time.Second, 500*time.Millisecond, "sap did not sync all records")

	// Every delivered URI was one we created.
	created.Range(func(k, _ any) bool {
		_, ok := seen.Load(k)
		require.True(t, ok, "created URI not delivered: %v", k)
		return true
	})
}
```

Add `registerSapSession` (posts `{"did": member}` to sap's internal `/session/jwt`; `stack` needs the sap internal HTTP host — derive it from `SapOutboxWS` by swapping scheme/path, or add a `SapInternal string` field to `sapStack` in Task 6 pointing at `http://127.0.0.1:PORT`):

```go
func registerSapSession(ctx context.Context, t *testing.T, stack *sapStack, did string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"did": did})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, stack.SapInternal+"/session/jwt", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
```

Add the `SapInternal` field to `sapStack` and set it in `startSapStack` (`"http://" + sapWSHost`). Add the needed imports (`bytes`, `net/http`, `encoding/json`) to `sap_test.go`. Implement `addInt64`/`loadInt64` with `atomic` or switch the counters to `atomic.Int64` and adjust call sites.

- [x] **Step 5: Run the integration test**

Run: `cd integration && go test -run TestSapSyncsPearRecords -v -timeout 600s`
Expected: PASS. Realistically this first run surfaces bugs — proceed to Step 6.

- [x] **Step 6: Systematic-debugging pass for discovered bugs**

For each failure, use the `superpowers:systematic-debugging` skill. Likely findings and where to fix:
- **notifyWrite hash empty** (`internal/notify/notifier.go` sends `Hash: ""`): confirm sap's incremental sync still converges (it re-derives via `listRepoOps`/`getRepo` + verifier). If convergence breaks, fix the notifier to send the signed commit hash or fix sap's handling of an empty hash. Add/extend a unit test in the owning package.
- **DID resolution / TLS trust**: if sap logs `lookup did ... x509`/DNS errors, the CoreDNS template or `SSL_CERT_FILE` is wrong — fix the Corefile regex or CA mount.
- **jwt-bearer aud/host**: if pear returns `invalid_grant` on sap's token requests, check sap resolved the correct habitat host and `aud`.
- **concurrency races**: run sap package tests with `-race`; fix any races in crawl/sync/outbox.

Each production fix lands as its own commit with a message describing the bug.

- [x] **Step 7: Commit the test**

```bash
git add integration/sap_test.go
git commit -m "integration: end-to-end pear-sap sync under concurrency and notify chaos

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

- [x] **Step 8: Add a moon task (optional wiring)**

If `integration/moon.yml` defines a test task, add a `sap` variant or ensure the module's `test` task runs `go test ./...`. Mirror the existing task; do not add it to `moon ci`.

Run: `cat integration/moon.yml` and follow the existing pattern.

---

## Self-Review Notes

- **Spec coverage:** JWT-bearer per-session auth (Tasks 2–4) ✓; confidential-client metadata + pear support (Tasks 1, 4) ✓; dnsmasq/DNS wildcard (Task 6, via CoreDNS) ✓; caddy wildcard TLS + notify-only toxiproxy path (Task 6) ✓; postgres (Task 6) ✓; sap Dockerfile (Task 5) ✓; outbox-websocket assertions (Task 8) ✓; high concurrency + chaos (Task 8) ✓; bug-fixing pass (Task 8 Step 6) ✓.
- **Deviations from spec (intentional, minimal):** (a) DNS via **CoreDNS** rather than dnsmasq — same role (wildcard `*.local.habitat.network → caddy`), chosen for a reliable official image and Docker-alias forwarding. (b) Test → pear over pear's **published HTTP port** instead of through caddy — avoids a host `:443` conflict with a running dev Caddy; caddy still fronts all in-container TLS. (c) A dedicated `/session/jwt` endpoint instead of an `auth_method` field on `/session/add`, because the existing `/session/add` is an OAuth redirect flow. (d) sap's ES256 signing key defaults to ephemeral generation when `SAP_JWT_SIGNING_KEY` is unset; `--jwt_client_id` from the spec is dropped (derived from `SAP_DOMAIN`).
- **Type consistency:** `AddSession(ctx, did, sessionID, method)` used consistently across `internal/sap/sap.go`, `cmd/sap/server.go`, and `internal/sap/sap_test.go`; `NewStore(db, oauth, jwt)` and `Add(ctx, did, sessionID, method)` consistent across session code and callers; grant string `urn:ietf:params:oauth:grant-type:jwt-bearer` identical in sap builder, pear config, and both test clients; JWKS `kid` = `"habitat"` (sap) / `"test-client"` (test client) matches the assertion header in each signer.

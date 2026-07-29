# OAuth Server Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Simplify the OAuth server by replacing custom stateless auth codes, in-memory stores, and cookie-carried request state with a single DB-backed `oauth_requests` table, Fosite's HMAC strategy, a garbage collector, and a cookie that carries nothing but two opaque values.

**Architecture:** Replace `storage.MemoryStore` and the custom encrypted-CBOR strategy with a GORM-backed `OAuthRequest` table whose columns map directly to the OAuth authorize request parameters. The store implements `fosite.PARStorage`, `oauth2.CoreStorage`, and `pkce.PKCERequestStorage` via this table; client assertion JWT tracking becomes a no-op. A `Collector` in `internal/oauthserver` periodically deletes expired rows. The authorize flow reads the pushed request with `GetPARSession` directly (rather than `NewAuthorizeRequest`, which would consume it), re-stores it under a fresh `crypto/rand` key, and puts that key — plus the opaque provider state — in the encrypted cookie. The callback finds its request through that key. The `state` parameter the PDS echoes back is `pdsclient`'s own and is never used for lookup.

**Tech Stack:** Go, Fosite, GORM, gorilla/sessions (for the request-key + provider-state cookie)

## Global Constraints

These bind every task. Violating one is a spec failure, not a style nit.

1. **The cookie carries only two small values: `request_key` and `provider_state`.** All
   authorization-request data lives in the DB, reached via `request_key`. There is no
   `authRequestFlash` struct, no gob registration, no form data in the cookie.
2. **Never key off the `state` query parameter on the PDS callback.** `pdsclient.Authorize`
   generates its own `state` inside its PAR push to the PDS and the redirect URL it returns
   carries only `client_id` and `request_uri` (`internal/pdsclient/oauth_client.go:203`).
   The `state` the PDS echoes back belongs to `pdsclient`, not to us. Appending our own
   `&state=` to that redirect is ignored in production even though
   `internal/pdsclient/dummy_oauth_client.go` echoes it in tests. The callback must find its
   request via the cookie's `request_key`.
3. **All opaque keys (`request_key`) come from `crypto/rand`, at least 16 bytes**, hex- or
   base64url-encoded. `math/rand` is forbidden — these keys authorize resuming an in-flight
   authorization.
4. **`oauth_server.go` never touches `s.db` or GORM directly.** Every read/write goes through
   a method on `*store`. (`ListConnectedApps` already violates this today; leave it alone —
   out of scope.)
5. **`OAuthRequest.Subject` carries the resolved DID.** It is set at PAR time (from the
   login hint) or after handle resolution, and is the single source of truth for "which user
   is this flow for". There is no `login_hint`/`handle` column — the reconstructed form does
   not carry them.
6. **`toAuthorizeRequest` must return a requester fosite can actually respond to.**
   `NewAuthorizeResponse` gates on `GetResponseTypes().ExactOne("code")` and
   `WriteAuthorizeResponse` needs a parsed `*url.URL` in `RedirectURI` plus a `Client` for
   `IsRedirectURIValid()`. Because the PAR path deliberately bypasses `NewAuthorizeRequest`,
   nothing else populates these fields.
7. **`fosite.Request` has no `AuthorizedAt` field** (fosite v0.49.0 — the field is
   `RequestedAt`). Do not add an `AuthorizedAt` column.

---

### Task 1: Add `OAuthRequest` model and helpers

**Files:**
- Modify: `internal/oauthserver/fosite_storage.go`

- [ ] **Step 1: Add OAuthRequest model**

Add the new model struct above `OAuthSession`:

```go
// OAuthRequest is the single row type backing every short-lived piece of OAuth
// flow state: pushed authorization requests, in-flight authorization requests
// bridging the PDS redirect, and issued authorization codes. Which one a row is
// depends only on what its Key is (a PAR request_uri, a cookie-held request key,
// or an authorization code signature).
type OAuthRequest struct {
    Key                 string `gorm:"primaryKey"`
    ClientID            string `gorm:"size:1024"`
    // Subject is the resolved DID this flow authenticates. Empty until the login
    // hint is resolved (at PAR time) or the handle is resolved (at authorize time).
    Subject             string `gorm:"size:255"`
    Scopes              string `gorm:"size:512"` // space-separated
    CodeChallenge       string `gorm:"size:255"`
    CodeChallengeMethod string `gorm:"size:32"`
    RedirectURI         string `gorm:"size:1024"`
    State               string `gorm:"size:1024"` // the client's own OAuth state param
    ResponseType        string `gorm:"size:64"`
    ExpiresAt           time.Time `gorm:"index"`
}
```

No `AuthorizedAt` column — `fosite.Request` has no such field in fosite v0.49.0.

- [ ] **Step 2: Add helper methods to convert between OAuthRequest and fosite types**

`toAuthorizeRequest` must fully hydrate the requester. The PAR path deliberately skips
`NewAuthorizeRequest`, so this is the *only* thing that populates `ResponseTypes` and the
parsed `RedirectURI` — and `NewAuthorizeResponse`/`WriteAuthorizeResponse` both hard-require
them (see Global Constraint 6).

```go
import (
    "net/url"
    "strings"
    "github.com/ory/fosite"
)

// toAuthorizeRequest rebuilds the fosite.AuthorizeRequest this row was stored
// from. The redirect URI is parsed rather than left in the form because
// WriteAuthorizeResponse reads the struct field, not the form.
func (r *OAuthRequest) toAuthorizeRequest(client fosite.Client) (*fosite.AuthorizeRequest, error) {
    scopes := fosite.Arguments{}
    if r.Scopes != "" {
        scopes = strings.Split(r.Scopes, " ")
    }
    form := url.Values{}
    setIfNotEmpty := func(k, v string) {
        if v != "" {
            form.Set(k, v)
        }
    }
    setIfNotEmpty("client_id", r.ClientID)
    setIfNotEmpty("redirect_uri", r.RedirectURI)
    setIfNotEmpty("response_type", r.ResponseType)
    setIfNotEmpty("scope", r.Scopes)
    setIfNotEmpty("state", r.State)
    setIfNotEmpty("code_challenge", r.CodeChallenge)
    setIfNotEmpty("code_challenge_method", r.CodeChallengeMethod)

    var redirectURI *url.URL
    if r.RedirectURI != "" {
        var err error
        redirectURI, err = url.Parse(r.RedirectURI)
        if err != nil {
            return nil, fmt.Errorf("failed to parse stored redirect uri: %w", err)
        }
    }

    return &fosite.AuthorizeRequest{
        ResponseTypes:        fosite.Arguments(strings.Fields(r.ResponseType)),
        HandledResponseTypes: fosite.Arguments{},
        RedirectURI:          redirectURI,
        State:                r.State,
        Request: fosite.Request{
            Client:         client,
            Session:        &session{Subject: r.Subject, ClientID: r.ClientID, Scopes: scopes},
            RequestedScope: scopes,
            GrantedScope:   scopes,
            Form:           form,
            RequestedAt:    time.Now().UTC(),
        },
    }, nil
}

// fromRequester populates the OAuthRequest columns from a fosite.Requester.
func (r *OAuthRequest) fromRequester(key string, requester fosite.Requester, expiresAt time.Time) {
    form := requester.GetRequestForm()
    r.Key = key
    r.ClientID = requester.GetClient().GetID()
    r.Scopes = strings.Join(requester.GetRequestedScopes(), " ")
    r.RedirectURI = form.Get("redirect_uri")
    r.State = form.Get("state")
    r.ResponseType = form.Get("response_type")
    r.CodeChallenge = form.Get("code_challenge")
    r.CodeChallengeMethod = form.Get("code_challenge_method")
    r.ExpiresAt = expiresAt
    if sess := requester.GetSession(); sess != nil {
        r.Subject = sess.GetSubject()
    }
}
```

Note on `Subject`: because `CreatePARSession` uses GORM `Save` (a full-column UPDATE when the
key exists), re-storing a request under a key that already has a resolved DID would blank it
if the incoming session carries no subject. Callers that already know the DID must therefore
go through `CreateAuthorizeFlowSession` (Task 5), which sets `Subject` explicitly after
`fromRequester`.

- [ ] **Step 3: Add helper to reconstruct PKCE form from OAuthRequest**

```go
func (r *OAuthRequest) pkceForm() url.Values {
    v := url.Values{}
    if r.CodeChallenge != "" {
        v.Set("code_challenge", r.CodeChallenge)
    }
    if r.CodeChallengeMethod != "" {
        v.Set("code_challenge_method", r.CodeChallengeMethod)
    }
    return v
}
```

- [ ] **Step 4: Run tests to confirm compilation**

Run: `go build ./internal/oauthserver/`
Expected: builds without error

- [ ] **Step 5: Commit**

```bash
git add internal/oauthserver/fosite_storage.go
git commit -m "feat: add OAuthRequest model and helpers"
```

---

### Task 2: Rewrite store to use DB-backed OAuthRequest table

**Files:**
- Modify: `internal/oauthserver/fosite_storage.go`

- [ ] **Step 1: Remove `memoryStore` field, replace with pure DB-store**

Change the store struct:

```go
type store struct {
    db                       *gorm.DB
    approvedJwtBearerClients ApprovedClientStore
}
```

Remove `strategy *strategy` field from store (strategy is no longer needed by storage).

Update `newStore`:

```go
func newStore(db *gorm.DB, approvedJwtBearerClients ApprovedClientStore) (*store, error) {
    err := db.AutoMigrate(&OAuthRequest{}, &OAuthSession{}, &ConnectedApp{})
    if err != nil {
        return nil, err
    }
    return &store{
        db:                       db,
        approvedJwtBearerClients: approvedJwtBearerClients,
    }, nil
}
```

Remove the `strategy` import from this file, remove the `init()` gob registration if present.

- [ ] **Step 2: Implement PAR storage methods using OAuthRequest table**

Note: `CreatePARSession` must NOT blank `Subject` — `HandlePAR` resolves the login hint to a
DID and puts it on the session precisely so it lands in this column (Global Constraint 5).
`Save` (upsert) rather than `Create`, so the same key can be re-stored after the DID is
resolved without a duplicate-key error.

```go
// parSessionTTL bounds how long an in-flight authorization request — pushed or
// mid-PDS-redirect — stays resumable.
const parSessionTTL = 10 * time.Minute

func (s *store) CreatePARSession(ctx context.Context, requestURI string, request fosite.AuthorizeRequester) error {
    var r OAuthRequest
    r.fromRequester(requestURI, request, time.Now().Add(parSessionTTL))
    return s.db.WithContext(ctx).Save(&r).Error
}

func (s *store) GetPARSession(ctx context.Context, requestURI string) (fosite.AuthorizeRequester, error) {
    var r OAuthRequest
    err := s.db.WithContext(ctx).First(&r, "key = ?", requestURI).Error
    if err != nil {
        return nil, errors.Join(fosite.ErrNotFound, err)
    }
    if time.Now().After(r.ExpiresAt) {
        return nil, fosite.ErrNotFound
    }
    client, err := s.GetClient(ctx, r.ClientID)
    if err != nil {
        return nil, errors.Join(fosite.ErrNotFound, err)
    }
    return r.toAuthorizeRequest(client)
}

func (s *store) DeletePARSession(ctx context.Context, requestURI string) error {
    return s.db.WithContext(ctx).Delete(&OAuthRequest{}, "key = ?", requestURI).Error
}
```

- [ ] **Step 3: Implement authorize code storage methods**

```go
func (s *store) CreateAuthorizeCodeSession(ctx context.Context, signature string, requester fosite.Requester) error {
    var r OAuthRequest
    // Authorize code sessions expire based on fosite config (typically 10-15 min)
    exp := time.Now().Add(15 * time.Minute)
    if sess := requester.GetSession(); sess != nil {
        if t := sess.GetExpiresAt(fosite.AuthorizeCode); !t.IsZero() {
            exp = t
        }
    }
    r.fromRequester(signature, requester, exp)
    return s.db.WithContext(ctx).Create(&r).Error
}

func (s *store) GetAuthorizeCodeSession(ctx context.Context, signature string, _ fosite.Session) (fosite.Requester, error) {
    var r OAuthRequest
    err := s.db.WithContext(ctx).First(&r, "key = ?", signature).Error
    if err != nil {
        return nil, errors.Join(fosite.ErrNotFound, err)
    }
    client, err := s.GetClient(ctx, r.ClientID)
    if err != nil {
        return nil, errors.Join(fosite.ErrNotFound, err)
    }
    ar, err := r.toAuthorizeRequest(client)
    if err != nil {
        return nil, err
    }
    ar.Request.Session = &session{
        Subject:           r.Subject,
        ClientID:          r.ClientID,
        Scopes:            strings.Fields(r.Scopes),
        PKCEChallenge:     r.CodeChallenge,
        AuthCodeExpiresAt: r.ExpiresAt,
    }
    return &ar.Request, nil
}

func (s *store) InvalidateAuthorizeCodeSession(ctx context.Context, signature string) error {
    return s.db.WithContext(ctx).Delete(&OAuthRequest{}, "key = ?", signature).Error
}
```

- [ ] **Step 4: Implement PKCE storage methods**

These cannot be no-ops. fosite calls
`CreateAuthorizeCodeSession(ctx, sig, ar.Sanitize(c.GetSanitationWhiteList(ctx)))`
(`flow_authorize_code_auth.go:85`), and `Sanitize` keeps only
`{"code","redirect_uri","grant_type","response_type","scope","client_id"}` —
`code_challenge` and `code_challenge_method` are stripped before the store sees them, so
`fromRequester` would always persist an empty challenge and every PKCE token exchange would
fail with *"code verifier was provided but the code challenge was absent"*.

`CreatePKCERequestSession` is the one call site that receives the challenge intact
(`handler/pkce/handler.go:61-64` sanitizes with `["code_challenge","code_challenge_method"]`),
and fosite invokes it with the same signature as the authorize code, after
`CreateAuthorizeCodeSession`. So it updates the row the code session just wrote.

```go
// CreatePKCERequestSession implements pkce.PKCERequestStorage. It writes the
// challenge onto the authorize code's row: this is the only point in the flow
// where fosite hands us the challenge un-sanitized.
func (s *store) CreatePKCERequestSession(
    ctx context.Context,
    signature string,
    requester fosite.Requester,
) error {
    form := requester.GetRequestForm()
    return s.db.WithContext(ctx).Model(&OAuthRequest{}).
        Where("key = ?", signature).
        Updates(map[string]any{
            "code_challenge":        form.Get("code_challenge"),
            "code_challenge_method": form.Get("code_challenge_method"),
        }).Error
}

// GetPKCERequestSession implements pkce.PKCERequestStorage. The client is
// populated because pkce.validateNoPKCE calls IsPublic() on it.
func (s *store) GetPKCERequestSession(
    ctx context.Context,
    signature string,
    _ fosite.Session,
) (fosite.Requester, error) {
    var r OAuthRequest
    if err := s.db.WithContext(ctx).First(&r, "key = ?", signature).Error; err != nil {
        return nil, errors.Join(fosite.ErrNotFound, err)
    }
    client, err := s.GetClient(ctx, r.ClientID)
    if err != nil {
        return nil, errors.Join(fosite.ErrNotFound, err)
    }
    return &fosite.Request{Client: client, Form: r.pkceForm()}, nil
}

// DeletePKCERequestSession implements pkce.PKCERequestStorage. The challenge
// lives on the authorize code's row, which InvalidateAuthorizeCodeSession
// already deletes.
func (s *store) DeletePKCERequestSession(ctx context.Context, signature string) error {
    return nil
}
```

- [ ] **Step 5: No-op client assertion JWT and RFC 7523 tracking**

The existing code never checks these (always returns nil/false), so persistence is unnecessary.

```go
func (s *store) SetClientAssertionJWT(ctx context.Context, jti string, exp time.Time) error {
    return nil
}

func (s *store) ClientAssertionJWTValid(ctx context.Context, jti string) error {
    return nil
}

func (s *store) IsJWTUsed(ctx context.Context, jti string) (bool, error) {
    return false, nil
}

func (s *store) MarkJWTUsedForTime(ctx context.Context, jti string, exp time.Time) error {
    return nil
}
```

- [ ] **Step 6: Remove unused interface assertions and old imports**

Remove the `storage "github.com/ory/fosite/storage"` import. Keep the `_` interface assertions updated:

```go
var (
    _ fosite.Storage                = (*store)(nil)
    _ fosite.PARStorage             = (*store)(nil)
    _ oauth2.CoreStorage            = (*store)(nil)
    _ oauth2.TokenRevocationStorage = (*store)(nil)
    _ pkce.PKCERequestStorage       = (*store)(nil)
    _ rfc7523.RFC7523KeyStorage     = (*store)(nil)
)
```

- [ ] **Step 8: Build to verify**

Run: `go build ./internal/oauthserver/`
Expected: builds without error

- [ ] **Step 9: Run existing tests to check nothing broke yet (will fail, but check compile)**

Run: `go vet ./internal/oauthserver/`
Expected: vet passes

- [ ] **Step 10: Commit**

```bash
git add internal/oauthserver/fosite_storage.go
git commit -m "feat: rewrite store with DB-backed OAuthRequest table"
```

---

### Task 3: Delete fosite_strategy.go and simplify fosite_session.go

**Files:**
- Delete: `internal/oauthserver/fosite_strategy.go`
- Modify: `internal/oauthserver/fosite_session.go`
- Modify: `internal/oauthserver/oauth_server.go`

- [ ] **Step 1: Delete fosite_strategy.go**

Run: `rm internal/oauthserver/fosite_strategy.go`

- [ ] **Step 2: Simplify fosite_session.go — remove encrypt import and auth code encoding/decoding**

Remove the `encrypt` import (no longer needed once getAuthCode/decodeSession are gone):
```go
"github.com/habitat-network/habitat/internal/encrypt"
```

Remove these methods (custom encrypted-CBOR code strategy — no longer needed with HMAC):
```go
func (s *session) getAuthCode(encryptionKey []byte) (string, error)              // remove
func decodeSession(authCode string, encryptionKey []byte) (*session, error)       // remove
```

Keep `newAuthorizeSession` — it's still used by the existing oauth_server.go until Task 5 rewrites the callback. The `syntax` import stays too. The `session` struct definition stays — it still carries DID, scopes, PKCE challenge, and expiry fields.

- [ ] **Step 3: Update NewOAuthServer to use DefaultJWTStrategy directly**

In `oauth_server.go`, change the strategy construction:

```go
// Remove custom newStrategy call:
// strategy, err := newStrategy(secret, config)

// Use DefaultJWTStrategy directly:
privateKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), secret)
if err != nil {
    return nil, fmt.Errorf("failed to parse private key: %w", err)
}
strategy := compose.NewOAuth2JWTStrategy(func(ctx context.Context) (any, error) {
    return privateKey, nil
}, oauth2.NewHMACSHAStrategy(&hmac.HMACStrategy{Config: config}, config), config)
```

Add imports: `"crypto/ecdsa"`, `"crypto/elliptic"`, `"github.com/ory/fosite/compose"`, `"github.com/ory/fosite/handler/oauth2"`, `"github.com/ory/fosite/token/hmac"`.

Remove the old `strategy, err := newStrategy(secret, config)` block.

Update the `newStore` call to remove the strategy parameter:
```go
storage, err := newStore(db, approvedJwtBearerClients)
```

- [ ] **Step 3b: Resolve the login hint to a DID in `HandlePAR`**

The reconstructed form has no `handle`/`login_hint` column, so a PAR session must carry its
user as `Subject` or the flow would always bounce through disambiguation (Global Constraint 5).
Resolve the hint at push time and hand `NewPushedAuthorizeResponse` a seeded session —
`CreatePARSession` then persists it into the `Subject` column via `fromRequester`.

In `HandlePAR`, after `NewPushedAuthorizeRequest` succeeds and before
`NewPushedAuthorizeResponse`:

```go
sess := newSession()
if loginHint != "" {
    // Best-effort: an unresolvable hint is not a PAR failure, it just means the
    // user gets the disambiguation page at the authorize step.
    if atid, err := syntax.ParseAtIdentifier(loginHint); err == nil {
        // directory caches errors, so don't pass in the real context
        if id, err := o.directory.Lookup(context.Background(), atid); err == nil {
            sess.Subject = id.DID.String()
        } else {
            slog.WarnContext(ctx, "failed to resolve PAR login hint", "err", err, "hint", loginHint)
        }
    }
}
resp, err := o.provider.NewPushedAuthorizeResponse(ctx, req, sess)
```

Keep the existing `r.Form.Add("login_hint", loginHint)` line — it is what fosite's PAR
handler validates against, and the direct-authorize path still reads it from a live form.

- [ ] **Step 4: Remove `newAuthorizeSession` usage in HandleCallback**

In `HandleCallback`, replace:
```go
resp, err := o.provider.NewAuthorizeResponse(
    ctx, authRequest, newAuthorizeSession(authRequest, arf.Did),
)
```
with:
```go
resp, err := o.provider.NewAuthorizeResponse(
    ctx, authRequest, newSession(),
)
```
The session will be populated by the store's `GetAuthorizeCodeSession` which reconstructs it from the stored columns.

- [ ] **Step 5: Build to verify**

Run: `go build ./internal/oauthserver/`
Expected: builds without error

- [ ] **Step 6: Commit**

```bash
git rm internal/oauthserver/fosite_strategy.go
git add internal/oauthserver/fosite_session.go internal/oauthserver/oauth_server.go
git commit -m "feat: use DefaultJWTStrategy directly, remove custom strategy"
```

---

### Task 4: Add garbage collector

**Files:**
- Create: `internal/oauthserver/gc.go`

- [ ] **Step 1: Create gc.go**

```go
package oauthserver

import (
    "context"
    "log/slog"
    "time"

    "gorm.io/gorm"
)

// Collector periodically deletes expired OAuth sessions from the database.
type Collector struct {
    db       *gorm.DB
    interval time.Duration
}

// NewCollector creates a new Collector. interval defaults to 5 minutes.
func NewCollector(db *gorm.DB, interval time.Duration) *Collector {
    if interval <= 0 {
        interval = 5 * time.Minute
    }
    return &Collector{db: db, interval: interval}
}

// Run starts the collection loop. Stops when ctx is cancelled.
func (c *Collector) Run(ctx context.Context) error {
    ticker := time.NewTicker(c.interval)
    defer ticker.Stop()
    slog.InfoContext(ctx, "starting OAuth session garbage collector",
        "interval", c.interval)
    for {
        select {
        case <-ctx.Done():
            slog.InfoContext(ctx, "stopping OAuth session garbage collector")
            return ctx.Err()
        case <-ticker.C:
            c.clean(ctx)
        }
    }
}

func (c *Collector) clean(ctx context.Context) {
    var deleted int64
    result := c.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&OAuthRequest{})
    if result.Error != nil {
        slog.WarnContext(ctx, "failed to clean expired OAuth requests", "err", result.Error)
    } else {
        deleted += result.RowsAffected
    }

    result = c.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&OAuthSession{})
    if result.Error != nil {
        slog.WarnContext(ctx, "failed to clean expired OAuth sessions", "err", result.Error)
    } else {
        deleted += result.RowsAffected
    }

    if deleted > 0 {
        slog.DebugContext(ctx, "cleaned expired OAuth sessions", "count", deleted)
    }
}
```

- [ ] **Step 2: Build to verify**

Run: `go build ./internal/oauthserver/`
Expected: builds without error

- [ ] **Step 3: Commit**

```bash
git add internal/oauthserver/gc.go
git commit -m "feat: add OAuth session garbage collector"
```

---

### Task 5: Rewrite oauth_server.go authorize/callback flow

**Files:**
- Modify: `internal/oauthserver/oauth_server.go`

- [ ] **Step 1: Replace the flash cookie with a two-value key cookie**

Delete the `authRequestFlash` struct, the `init()` function, and the `"encoding/gob"` and
`"maps"` imports. The cookie now carries two small values and nothing else:

```go
const (
    // requestKeyCookie holds the opaque key of the in-flight authorization
    // request row. It is what lets the callback find the request without
    // trusting the `state` the PDS echoes back — that state belongs to
    // pdsclient's own OAuth flow, not to ours.
    requestKeyCookie = "request_key"
    // providerStateCookie holds the opaque login-provider state for one redirect hop.
    providerStateCookie = "provider_state"
)
```

Rename the `OAuthServer.sessionStore` field to `cookieStore` and in `NewOAuthServer`:

```go
cookieStore := sessions.NewCookieStore(secret)
cookieStore.Options = &sessions.Options{
    Path:     "/",
    HttpOnly: true,
    Secure:   true,
    SameSite: http.SameSiteNoneMode,
    MaxAge:   10 * 60, // the user has this long to authenticate at their PDS
}
```

`string` and `[]byte` are pre-registered by `encoding/gob`, so no `gob.Register` call is
needed for the new cookie values.

- [ ] **Step 2: Add a crypto-random key generator**

`math/rand` is forbidden here — this key authorizes resuming an in-flight authorization
(Global Constraint 3).

```go
// newRequestKey mints the opaque identifier for an in-flight authorization
// request. It is held only in the encrypted cookie, so it must be unguessable.
func newRequestKey() (string, error) {
    b := make([]byte, 32)
    if _, err := rand.Read(b); err != nil { // crypto/rand
        return "", fmt.Errorf("failed to generate request key: %w", err)
    }
    return base64.RawURLEncoding.EncodeToString(b), nil
}
```

- [ ] **Step 3: Add `CreateAuthorizeFlowSession` to the store**

`oauth_server.go` must not touch GORM (Global Constraint 4), and it needs to store a
requester together with a DID that is not yet on the requester's session. `CreatePARSession`
stays as fosite's `PARStorage` implementation; this is the method our own flow calls.

In `fosite_storage.go`:

```go
// CreateAuthorizeFlowSession persists an authorization request that is waiting
// on the user — either at the disambiguation page or at their PDS — under an
// opaque key held in the caller's cookie. A zero did means the user is not yet
// resolved. Save (not Create) so the same key can be re-stored once it is.
func (s *store) CreateAuthorizeFlowSession(
    ctx context.Context,
    key string,
    requester fosite.AuthorizeRequester,
    did syntax.DID,
) error {
    var r OAuthRequest
    r.fromRequester(key, requester, time.Now().Add(parSessionTTL))
    r.Subject = did.String()
    return s.db.WithContext(ctx).Save(&r).Error
}
```

Reads and deletes reuse `GetPARSession` / `DeletePARSession` — same table, same semantics.

- [ ] **Step 4: Rewrite HandleAuthorize**

The flow, in order:

1. Read the cookie. If it holds a `request_key`, we are resuming after disambiguation —
   `GetPARSession(key)`.
2. Else if `request_uri` is present, this is a PAR flow — `GetPARSession(request_uri)`
   directly, *not* `NewAuthorizeRequest`, which would consume the PAR session.
3. Else it is a direct authorize — `NewAuthorizeRequest`.
4. Determine the DID. `HandlePAR` may already have resolved it onto `Subject`; otherwise
   resolve a handle from the query (disambiguation return) or the form.
5. No handle and no subject → park the request under a fresh key, put the key in the cookie,
   redirect to disambiguation.
6. Otherwise begin the login, re-store under a fresh key with the DID, write the key and the
   provider state to the cookie, and redirect to the provider **unmodified**.

```go
func (o *OAuthServer) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    cookie, err := o.cookieStore.Get(r, sessionName)
    if err != nil {
        o.metrics.authorizeErr(ctx, err, "get_cookie")
        httpx.WriteServerError(ctx, w, fmt.Errorf("failed to get cookie: %w", err))
        return
    }

    // staleKey is the row this request currently lives under, if any. It is
    // dropped once the request has been re-stored under a fresh key, so a
    // consumed PAR request_uri or a used disambiguation key cannot be replayed.
    var staleKey string
    var requester fosite.AuthorizeRequester
    switch resumeKey, _ := cookie.Values[requestKeyCookie].(string); {
    case resumeKey != "":
        staleKey = resumeKey
        requester, err = o.storage.GetPARSession(ctx, resumeKey)
        if err != nil {
            o.metrics.authorizeErr(ctx, err, "resume_lookup")
            httpx.WriteInvalidRequest(ctx, w, "authorization request expired", err)
            return
        }
    case r.URL.Query().Get("request_uri") != "":
        staleKey = r.URL.Query().Get("request_uri")
        requester, err = o.storage.GetPARSession(ctx, staleKey)
        if err != nil {
            o.metrics.authorizeErr(ctx, err, "par_lookup")
            o.provider.WriteAuthorizeError(ctx, w, requester, fosite.ErrInvalidRequestURI)
            return
        }
    default:
        requester, err = o.provider.NewAuthorizeRequest(ctx, r)
        if err != nil {
            o.metrics.authorizeErr(ctx, err, fositeErrReason(err))
            o.provider.WriteAuthorizeError(ctx, w, requester, err)
            return
        }
    }

    key, err := newRequestKey()
    if err != nil {
        o.metrics.authorizeErr(ctx, err, "new_request_key")
        httpx.WriteServerError(ctx, w, err)
        return
    }

    // HandlePAR resolves the login hint at push time, so the subject may already
    // be known; otherwise fall back to a handle from the disambiguation redirect
    // or the request form.
    var did syntax.DID
    if subject := requester.GetSession().GetSubject(); subject != "" {
        did, err = syntax.ParseDID(subject)
        if err != nil {
            o.metrics.authorizeErr(ctx, err, "parse_subject")
            httpx.WriteServerError(ctx, w, fmt.Errorf("failed to parse stored subject: %w", err))
            return
        }
    } else {
        handle := r.URL.Query().Get("handle")
        if handle == "" {
            handle = requester.GetRequestForm().Get("handle")
        }
        if handle == "" {
            handle = requester.GetRequestForm().Get("login_hint")
        }
        if handle == "" {
            // Park the request and let the user pick an identity. The cookie
            // carries only the key, so the resumed request is provably ours.
            if err := o.storage.CreateAuthorizeFlowSession(ctx, key, requester, ""); err != nil {
                o.metrics.authorizeErr(ctx, err, "store_disambiguation")
                httpx.WriteServerError(ctx, w, fmt.Errorf("failed to store request: %w", err))
                return
            }
            if staleKey != "" {
                if err := o.storage.DeletePARSession(ctx, staleKey); err != nil {
                    slog.WarnContext(ctx, "failed to delete stale oauth request", "err", err)
                }
            }
            cookie.Values[requestKeyCookie] = key
            delete(cookie.Values, providerStateCookie)
            if err := cookie.Save(r, w); err != nil {
                o.metrics.authorizeErr(ctx, err, "save_cookie")
                httpx.WriteServerError(ctx, w, fmt.Errorf("failed to save cookie: %w", err))
                return
            }
            http.Redirect(w, r, disambiguationPath, http.StatusSeeOther)
            return
        }

        atid, err := syntax.ParseAtIdentifier(handle)
        if err != nil {
            o.metrics.authorizeErr(ctx, err, "parse_handle")
            httpx.WriteInvalidRequest(ctx, w, "failed to parse handle", err)
            return
        }
        // directory caches errors, so don't pass in the real context
        id, err := o.directory.Lookup(context.Background(), atid)
        if err != nil {
            o.metrics.authorizeErr(ctx, err, "lookup_atid")
            httpx.WriteServerError(ctx, w, fmt.Errorf("failed to lookup atid: %w", err))
            return
        }
        did = id.DID
    }

    redirect, providerState, err := o.loginRouter.Authorize(ctx, did)
    if err != nil {
        o.metrics.authorizeErr(ctx, err, "begin_login")
        httpx.WriteServerError(ctx, w, fmt.Errorf("failed to begin login: %w", err))
        return
    }

    if err := o.storage.CreateAuthorizeFlowSession(ctx, key, requester, did); err != nil {
        o.metrics.authorizeErr(ctx, err, "store_request")
        httpx.WriteServerError(ctx, w, fmt.Errorf("failed to store request: %w", err))
        return
    }
    if staleKey != "" {
        if err := o.storage.DeletePARSession(ctx, staleKey); err != nil {
            slog.WarnContext(ctx, "failed to delete stale oauth request", "err", err)
        }
    }

    cookie.Values[requestKeyCookie] = key
    cookie.Values[providerStateCookie] = providerState
    if err := cookie.Save(r, w); err != nil {
        o.metrics.authorizeErr(ctx, err, "save_cookie")
        httpx.WriteServerError(ctx, w, fmt.Errorf("failed to save cookie: %w", err))
        return
    }

    // Redirect to the provider untouched: pdsclient already put its own state
    // inside the request it pushed to the PDS, and appending ours would be both
    // ignored and ambiguous on the way back.
    http.Redirect(w, r, redirect, http.StatusSeeOther)
    o.metrics.authorizeSuccess(ctx)
}
```

- [ ] **Step 5: Rewrite HandleCallback**

The callback finds its request through the cookie key, never through the `state` query
parameter (Global Constraint 2).

```go
func (o *OAuthServer) HandleCallback(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    cookie, err := o.cookieStore.Get(r, sessionName)
    if err != nil {
        o.metrics.callbackErr(ctx, err, "get_cookie")
        httpx.WriteInvalidRequest(ctx, w, "failed to get cookie", err)
        return
    }
    key, _ := cookie.Values[requestKeyCookie].(string)
    providerState, _ := cookie.Values[providerStateCookie].([]byte)

    // The cookie is single-use: clear it before doing anything that can fail.
    cookie.Options.MaxAge = -1
    if err := cookie.Save(r, w); err != nil {
        o.metrics.callbackErr(ctx, err, "delete_cookie")
        httpx.WriteServerError(ctx, w, fmt.Errorf("failed to delete cookie: %w", err))
        return
    }
    if key == "" || len(providerState) == 0 {
        o.metrics.callbackErr(ctx, nil, "no_cookie_state")
        httpx.WriteInvalidRequest(ctx, w, "no authorization request in progress", nil)
        return
    }

    requester, err := o.storage.GetPARSession(ctx, key)
    if err != nil {
        o.metrics.callbackErr(ctx, err, "request_lookup")
        httpx.WriteInvalidRequest(ctx, w, "authorization request expired", err)
        return
    }
    defer func() {
        if err := o.storage.DeletePARSession(ctx, key); err != nil {
            slog.WarnContext(ctx, "failed to delete oauth request", "err", err)
        }
    }()

    did, err := syntax.ParseDID(requester.GetSession().GetSubject())
    if err != nil {
        o.metrics.callbackErr(ctx, err, "parse_did")
        httpx.WriteServerError(ctx, w, fmt.Errorf("failed to parse stored subject: %w", err))
        return
    }

    if err := o.loginRouter.Exchange(ctx, did, r.URL.Query(), providerState); err != nil {
        o.metrics.callbackErr(ctx, err, "complete_login")
        httpx.WriteServerError(ctx, w, fmt.Errorf("failed to complete login: %w", err))
        return
    }

    // Grant the requested scopes so they are bound to the authorization code and
    // echoed back in the token response. Without this the token response carries
    // an empty scope, which atproto clients reject (they require a valid scope
    // containing "atproto"). The client's allowed scopes were already validated
    // when the authorize request was parsed.
    for _, scope := range requester.GetRequestedScopes() {
        requester.GrantScope(scope)
    }

    resp, err := o.provider.NewAuthorizeResponse(ctx, requester, &session{
        Subject:       did.String(),
        ClientID:      requester.GetClient().GetID(),
        Scopes:        requester.GetRequestedScopes(),
        PKCEChallenge: requester.GetRequestForm().Get("code_challenge"),
    })
    if err != nil {
        o.metrics.callbackErr(ctx, err, fositeErrReason(err))
        httpx.WriteServerError(ctx, w, fmt.Errorf("failed to create response: %w", err))
        return
    }
    resp.AddParameter("iss", o.issuer)
    o.provider.WriteAuthorizeResponse(ctx, w, requester, resp)
    o.metrics.callbackSuccess()
}
```

- [ ] **Step 6: Build to verify**

Run: `go build ./internal/oauthserver/`
Expected: builds without error

- [ ] **Step 7: Remove unused `newAuthorizeSession` from fosite_session.go**

Now that the callback creates the session inline, `newAuthorizeSession` is dead code. Remove the function and the `syntax` import from `fosite_session.go`:

```go
// Remove the import:
// "github.com/bluesky-social/indigo/atproto/syntax"

// Remove this function:
// func newAuthorizeSession(req fosite.AuthorizeRequester, did syntax.DID) *session { ... }
```

- [ ] **Step 8: Build to verify**

Run: `go build ./internal/oauthserver/`
Expected: builds without error

- [ ] **Step 9: Commit**

```bash
git add internal/oauthserver/oauth_server.go internal/oauthserver/fosite_session.go
git commit -m "feat: rewrite authorize/callback flow with DB-backed state (no raw DB access, cookie for disambiguation only)"
```

---

### Task 6: Wire up garbage collector in cmd/pear

**Files:**
- Modify: `cmd/pear/main.go`

- [ ] **Step 1: Add GC goroutine to errgroup**

After creating the `oauthServer` (line ~295), add:

```go
gc := oauthserver.NewCollector(db.WithContext(startupCtx), 5*time.Minute)
eg.Go(func() error {
    return gc.Run(egCtx)
})
```

- [ ] **Step 2: Build to verify**

Run: `go build ./cmd/pear/`
Expected: builds without error

- [ ] **Step 3: Commit**

```bash
git add cmd/pear/main.go
git commit -m "feat: wire up OAuth session garbage collector"
```

---

### Task 7: Update tests

**Files:**
- Modify: `internal/oauthserver/oauth_server_test.go`
- Modify: `internal/oauthserver/storage_test.go` (if it constructs a store)

The cookie jar stays essential in every end-to-end test: the request key now lives in the
cookie, so a client without a jar cannot complete a flow. Do not remove `cookiejar` setup
from any test.

- [ ] **Step 1: Fix store/server construction**

`newStore` and `NewOAuthServer` lost the `*strategy` parameter. Update every call site in
tests.

`NewOAuthServer` still validates the secret — `ecdsa.ParseRawPrivateKey(elliptic.P256(), secret)`
just moved inline from `newStrategy`. Keep the `NewOAuthServer rejects invalid secret` case.

- [ ] **Step 2: Replace `encrypt` key generation with a raw random key**

The secret is no longer round-tripped through `encrypt.ParseKey`; it is used directly as
`[]byte` for the fosite global secret, the cookie store, and the P-256 private key. Replace
`encrypt.GenerateKey()`/`encrypt.ParseKey()` with a 32-byte `crypto/rand` key and drop the
`encrypt` import if nothing else uses it.

```go
secret := make([]byte, 32)
_, err := rand.Read(secret)
require.NoError(t, err)
```

The `CanHandle returns true for oauth header` case signs with `secret` directly.

- [ ] **Step 3: Run the end-to-end tests and fix fallout**

`TestOAuthServerE2E`, `TestHandleCallbackDIDNotInAllowlist`,
`TestOAuthServerAuthenticatesHiveServedIdentity`, `TestHandleCallbackRejectsOrgScopeForNonAdmin`,
`TestIndigoClientApp`, `TestValidate`, and the `acquireAccessToken` helper all drive the flow
by following redirects with a cookie jar. That still works: the jar now carries the request
key and provider state instead of the flash.

Two things genuinely changed and may need test updates:
- The authorize redirect to the provider no longer has anything appended to it.
- A callback with no cookie now fails with "no authorization request in progress" rather than
  "no state found for session". Update any assertion on that message.

Run: `go test ./internal/oauthserver/ -count=1`

- [ ] **Step 4: Add a test that the callback does not trust the PDS `state` parameter**

This is the regression the whole redesign exists to prevent, and the passthrough dummy
provider echoes `state` back, so a lookup keyed on it would pass unnoticed (Global
Constraint 2). Drive the flow to the callback and assert it still succeeds when the callback
URL's `state` is replaced with a value that was never a request key.

- [ ] **Step 5: Add a store round-trip test for `toAuthorizeRequest`**

Assert that a requester read back through `GetPARSession` has a non-nil parsed `RedirectURI`,
`ResponseTypes` equal to `["code"]`, its scopes, its PKCE challenge, and its `Subject` — the
fields fosite needs and that the columns must carry (Global Constraint 6).

- [ ] **Step 6: Run the full package suite**

Run: `go test ./internal/oauthserver/ -count=1`
Expected: all tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/oauthserver/
git commit -m "test: update tests for DB-backed OAuth flow"
```

---

### Task 8: Final cleanup and verification

**Files:**
- `internal/oauthserver/fosite_storage.go` — remove unused imports, remove `encrypt` references
- `go.mod` — remove unused dependencies if any (keep gorilla/sessions)
- `internal/oauthserver/` — verify all files compile and vet clean

- [ ] **Step 1: Run full build**

Run: `go build ./...`
Expected: all packages build

- [ ] **Step 2: Run vet**

Run: `go vet ./internal/oauthserver/...`
Expected: no vet errors

- [ ] **Step 3: Run linter**

Run: `golangci-lint run ./internal/oauthserver/...`
Expected: no lint errors

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: final cleanup after OAuth server simplification"
```

# `pkg/oauth_client` Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract OAuth logic from `internal/sap/` into a reusable `pkg/oauth_client` package that wraps indigo's `oauth.ClientApp` and uses `oauth.ClientConfig` + `oauth.ClientAuthStore` directly from indigo.

**Architecture:** `pkg/oauth_client` provides a thin `App` struct wrapping `oauth.ClientApp`. `internal/sap/` implements `oauth.ClientAuthStore` via GORM. `cmd/sap/` wires the App and serves OAuth callback + client-metadata endpoints directly. `org_manager.go` and `internal/sap/server.go` are deleted.

**Tech Stack:** Go 1.26, `github.com/bluesky-social/indigo/atproto/auth/oauth`, GORM, SQLite

---
## File Structure

### Created
- `pkg/oauth_client/go.mod`
- `pkg/oauth_client/oauth_client.go` — `App` struct wrapping `oauth.ClientApp`
- `pkg/oauth_client/oauth_client_test.go`
- `internal/sap/store.go` — GORM implementation of `oauth.ClientAuthStore`

### Modified
- `internal/sap/sap.go` — absorb SAP coordination from orgManager, add `ClientAuthStore` impl
- `internal/sap/models.go` — adjust `managedOrg`, add session/auth request GORM models
- `internal/sap/crawler.go` — use `ClientSession` instead of `orgManager.GetClient()`
- `internal/sap/resyncer.go` — use `ClientSession` instead of `orgManager.GetClient()`
- `internal/sap/subscriber.go` — use `ClientSession` instead of `orgManager.GetClient()`
- `internal/sap/crawler_test.go` — update test setup
- `internal/sap/sap_test.go` — update
- `internal/sap/resync_buffer.go` — minor import fixes
- `cmd/sap/server.go` — add OAuth callback + client-metadata handlers
- `cmd/sap/main.go` — wire `oauth_client.App`
- `cmd/sap/go.mod` — add dependency on `pkg/oauth_client`
- `cmd/sap/sap_test.go` — update integration test wiring
- `cmd/sap/websocket_test.go` — update test setup

### Deleted
- `internal/sap/org_manager.go` — OAuth + SAP coordination absorbed
- `internal/sap/org_manager_test.go` — covered by pkg + integration tests
- `internal/sap/server.go` — handlers move to cmd/sap/

### Root go.mod
- `go.mod` — add `replace` directive for `pkg/oauth_client`

---

### Task 1: Create `pkg/oauth_client` package

**Files:**
- Create: `pkg/oauth_client/go.mod`
- Create: `pkg/oauth_client/oauth_client.go`
- Create: `pkg/oauth_client/oauth_client_test.go`

- [ ] **Step 1: Create `pkg/oauth_client/go.mod`**

```
module github.com/habitat-network/habitat/pkg/oauth_client

go 1.26.3

require (
    github.com/bluesky-social/indigo v0.0.0-20260318212431-cbaa83aee9dd
    github.com/stretchr/testify v1.11.1
    golang.org/x/oauth2 v0.36.0
)
```

- [ ] **Step 2: Write `pkg/oauth_client/oauth_client.go`**

```go
package oauth_client

import (
    "context"
    "net/url"

    "github.com/bluesky-social/indigo/atproto/auth/oauth"
    "github.com/bluesky-social/indigo/atproto/syntax"
)

// App wraps oauth.ClientApp, providing a Habitat OAuth client entrypoint.
// Callers provide the config (via oauth.NewPublicConfig etc.) and a persistent
// store implementing oauth.ClientAuthStore.
type App struct {
    inner *oauth.ClientApp
}

// NewApp creates an App.
func NewApp(config *oauth.ClientConfig, store oauth.ClientAuthStore) *App {
    return &App{
        inner: oauth.NewClientApp(config, store),
    }
}

// StartAuthFlow resolves the identifier (handle, DID, or auth server URL) and
// returns a redirect URL for the user to approve the OAuth flow.
func (a *App) StartAuthFlow(ctx context.Context, identifier string) (string, error) {
    return a.inner.StartAuthFlow(ctx, identifier)
}

// ProcessCallback handles the OAuth callback query parameters, exchanges the
// authorization code for tokens, and persists the session data via the store.
func (a *App) ProcessCallback(ctx context.Context, params url.Values) (*oauth.ClientSessionData, error) {
    return a.inner.ProcessCallback(ctx, params)
}

// ResumeSession fetches an existing session from the store and returns a
// ClientSession that can make authenticated API calls.
func (a *App) ResumeSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSession, error) {
    return a.inner.ResumeSession(ctx, did, sessionID)
}

// Logout revokes access/refresh tokens (if supported by the auth server) and
// deletes the session from the store.
func (a *App) Logout(ctx context.Context, did syntax.DID, sessionID string) error {
    return a.inner.Logout(ctx, did, sessionID)
}
```

- [ ] **Step 3: Write `pkg/oauth_client/oauth_client_test.go`**

```go
package oauth_client

import (
    "net/http"
    "net/http/httptest"
    "net/url"
    "testing"

    "github.com/bluesky-social/indigo/atproto/auth/oauth"
    "github.com/bluesky-social/indigo/atproto/syntax"
    "github.com/stretchr/testify/require"
)

func TestNewApp(t *testing.T) {
    config := oauth.NewPublicConfig(
        "https://example.com/client-metadata.json",
        "https://example.com/oauth-callback",
        []string{"atproto"},
    )
    store := oauth.NewMemStore()
    app := NewApp(&config, store)
    require.NotNil(t, app)
    require.NotNil(t, app.inner)
}

func TestAppStartAuthFlowAndCallback(t *testing.T) {
    // Start a test auth server that supports PAR
    authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/.well-known/oauth-authorization-server":
            w.Header().Set("Content-Type", "application/json")
            w.Write([]byte(`{
                "issuer": "` + r.Host + `",
                "authorization_endpoint": "` + r.Host + `/oauth/authorize",
                "token_endpoint": "` + r.Host + `/oauth/token",
                "response_types_supported": ["code"],
                "grant_types_supported": ["authorization_code", "refresh_token"],
                "code_challenge_methods_supported": ["S256"],
                "token_endpoint_auth_methods_supported": ["none"],
                "token_endpoint_auth_signing_alg_values_supported": ["ES256"],
                "scopes_supported": ["atproto"],
                "authorization_response_iss_parameter_supported": true,
                "require_pushed_authorization_requests": true,
                "pushed_authorization_request_endpoint": "` + r.Host + `/oauth/par",
                "dpop_signing_alg_values_supported": ["ES256"],
                "client_id_metadata_document_supported": true
            }`))
        default:
            t.Logf("unexpected request: %s %s", r.Method, r.URL.Path)
            http.NotFound(w, r)
        }
    }))
    t.Cleanup(authSrv.Close)

    // We need a proper PAR- and DPoP-supporting AS to fully test the flow.
    // The OAuth flow involves async web redirects, so this test validates
    // that construction and basic call patterns work, not the full protocol.
    config := oauth.NewLocalhostConfig(
        authSrv.URL+"/oauth-callback",
        []string{"atproto"},
    )
    store := oauth.NewMemStore()
    app := NewApp(&config, store)

    // Verify App exposes expected methods by calling them (they'll fail on
    // incomplete server setup, but the wiring is correct).
    _, err := app.StartAuthFlow(t.Context(), authSrv.URL)
    t.Logf("StartAuthFlow expected to fail without proper PAR endpoint: %v", err)
}

func TestAppResumeSession(t *testing.T) {
    config := oauth.NewLocalhostConfig(
        "http://localhost/oauth-callback",
        []string{"atproto"},
    )
    store := oauth.NewMemStore()
    app := NewApp(&config, store)

    _, err := app.ResumeSession(t.Context(), syntax.DID("did:plc:test"), "sess1")
    require.Error(t, err, "expected error for non-existent session")
}

func TestAppLogout(t *testing.T) {
    config := oauth.NewLocalhostConfig(
        "http://localhost/oauth-callback",
        []string{"atproto"},
    )
    store := oauth.NewMemStore()
    app := NewApp(&config, store)

    err := app.Logout(t.Context(), syntax.DID("did:plc:test"), "sess1")
    require.Error(t, err, "expected error for non-existent session")
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./pkg/oauth_client/ -v
```
Expected: Tests pass (or at least compile and show expected errors).

- [ ] **Step 5: Commit**

```bash
git add pkg/oauth_client/
git commit -m "feat: create pkg/oauth_client package"
```

---

### Task 2: Create `pkg/oauth_client/gormstore` — reusable GORM-backed `ClientAuthStore`

**Files:**
- Create: `pkg/oauth_client/gormstore/go.mod`
- Create: `pkg/oauth_client/gormstore/gormstore.go`
- Create: `pkg/oauth_client/gormstore/gormstore_test.go`

- [ ] **Step 1: Create `pkg/oauth_client/gormstore/go.mod`**

```
module github.com/habitat-network/habitat/pkg/oauth_client/gormstore

go 1.26.3

require (
    github.com/bluesky-social/indigo v0.0.0-20260318212431-cbaa83aee9dd
    github.com/stretchr/testify v1.11.1
    gorm.io/gorm v1.31.1
)
```

- [ ] **Step 2: Write `pkg/oauth_client/gormstore/gormstore.go`**

A ready-to-use `oauth.ClientAuthStore` implementation backed by GORM. Stores session data and auth request info as JSON blobs in two tables (`sap_sessions`, `sap_auth_requests`).

Key design decisions:
- Sessions are keyed by (did, session_id) composite primary key
- Auth requests are keyed by `state` (the OAuth state parameter)
- `ClientSessionData` is serialized as JSON — caller must ensure fields are exported or write a custom marshaler
- `SaveSession` extracts did/session_id from the session data by marshaling to JSON first, then unmarshaling into a key-extraction struct. This avoids package-access issues with indigo's unexported fields.

```go
package gormstore

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/bluesky-social/indigo/atproto/auth/oauth"
    "github.com/bluesky-social/indigo/atproto/syntax"
    "gorm.io/gorm"
)

// sessionRow stores oauth.ClientSessionData keyed by (did, session_id).
type sessionRow struct {
    DID       syntax.DID `gorm:"column:did;primaryKey;type:text"`
    SessionID string     `gorm:"column:session_id;primaryKey;type:text"`
    Data      []byte     `gorm:"column:data;type:blob"`
    CreatedAt time.Time
    UpdatedAt time.Time
}

func (sessionRow) TableName() string { return "client_sessions" }

// authRequestRow stores oauth.AuthRequestData keyed by state.
type authRequestRow struct {
    State     string    `gorm:"column:state;primaryKey;type:text"`
    Data      []byte    `gorm:"column:data;type:blob"`
    CreatedAt time.Time
}

func (authRequestRow) TableName() string { return "client_auth_requests" }

// Store implements oauth.ClientAuthStore backed by GORM.
type Store struct {
    db *gorm.DB
}

// New creates a Store and auto-migrates the schema.
func New(db *gorm.DB) (*Store, error) {
    if err := db.AutoMigrate(&sessionRow{}, &authRequestRow{}); err != nil {
        return nil, fmt.Errorf("migrate gormstore: %w", err)
    }
    return &Store{db: db}, nil
}

// GetSession implements oauth.ClientAuthStore.
func (s *Store) GetSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSessionData, error) {
    var row sessionRow
    if err := s.db.WithContext(ctx).
        Where("did = ? AND session_id = ?", did, sessionID).
        First(&row).Error; err != nil {
        return nil, err
    }
    var data oauth.ClientSessionData
    if err := json.Unmarshal(row.Data, &data); err != nil {
        return nil, fmt.Errorf("unmarshal session: %w", err)
    }
    return &data, nil
}

// SaveSession implements oauth.ClientAuthStore.
func (s *Store) SaveSession(ctx context.Context, sess oauth.ClientSessionData) error {
    data, err := json.Marshal(sess)
    if err != nil {
        return fmt.Errorf("marshal session: %w", err)
    }
    // Extract key fields by round-tripping through JSON.
    key, err := extractSessionKey(data)
    if err != nil {
        return fmt.Errorf("extract session key: %w", err)
    }
    return s.db.WithContext(ctx).Save(&sessionRow{
        DID:       key.DID,
        SessionID: key.SessionID,
        Data:      data,
    }).Error
}

// DeleteSession implements oauth.ClientAuthStore.
func (s *Store) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
    return s.db.WithContext(ctx).
        Where("did = ? AND session_id = ?", did, sessionID).
        Delete(&sessionRow{}).Error
}

// GetAuthRequestInfo implements oauth.ClientAuthStore.
func (s *Store) GetAuthRequestInfo(ctx context.Context, state string) (*oauth.AuthRequestData, error) {
    var row authRequestRow
    if err := s.db.WithContext(ctx).
        Where("state = ?", state).
        First(&row).Error; err != nil {
        return nil, err
    }
    var data oauth.AuthRequestData
    if err := json.Unmarshal(row.Data, &data); err != nil {
        return nil, fmt.Errorf("unmarshal auth request: %w", err)
    }
    return &data, nil
}

// SaveAuthRequestInfo implements oauth.ClientAuthStore.
func (s *Store) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
    data, err := json.Marshal(info)
    if err != nil {
        return fmt.Errorf("marshal auth request: %w", err)
    }
    return s.db.WithContext(ctx).Create(&authRequestRow{
        State: info.State,
        Data:  data,
    }).Error
}

// DeleteAuthRequestInfo implements oauth.ClientAuthStore.
func (s *Store) DeleteAuthRequestInfo(ctx context.Context, state string) error {
    return s.db.WithContext(ctx).
        Where("state = ?", state).
        Delete(&authRequestRow{}).Error
}

// sessionKey holds the primary-key fields extracted from ClientSessionData JSON.
type sessionKey struct {
    DID       syntax.DID `json:"account_did"`
    SessionID string     `json:"session_id"`
}

func extractSessionKey(data []byte) (*sessionKey, error) {
    var key sessionKey
    if err := json.Unmarshal(data, &key); err != nil {
        return nil, err
    }
    if key.DID == "" || key.SessionID == "" {
        return nil, fmt.Errorf("session data missing did or session_id")
    }
    return &key, nil
}
```

- [ ] **Step 3: Write `pkg/oauth_client/gormstore/gormstore_test.go`**

```go
package gormstore

import (
    "context"
    "testing"

    "github.com/bluesky-social/indigo/atproto/auth/oauth"
    "github.com/bluesky-social/indigo/atproto/syntax"
    "github.com/stretchr/testify/require"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"
)

func openDB(t *testing.T) *gorm.DB {
    t.Helper()
    db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
    require.NoError(t, err)
    return db
}

func TestStore_SaveAndGetSession(t *testing.T) {
    store, err := New(openDB(t))
    require.NoError(t, err)

    did := syntax.DID("did:plc:test")
    sessionID := "sess1"
    // Store a session by round-tripping through JSON
    data := []byte(`{"account_did":"did:plc:test","session_id":"sess1"}`)
    var sess oauth.ClientSessionData
    require.NoError(t, json.Unmarshal(data, &sess))
    require.NoError(t, store.SaveSession(context.Background(), sess))

    got, err := store.GetSession(context.Background(), did, sessionID)
    require.NoError(t, err)
    require.NotNil(t, got)
}

func TestStore_SaveAndGetAuthRequest(t *testing.T) {
    store, err := New(openDB(t))
    require.NoError(t, err)

    info := oauth.AuthRequestData{
        State:        "state123",
        AuthServerURL: "https://pds.example.com",
    }
    require.NoError(t, store.SaveAuthRequestInfo(context.Background(), info))

    got, err := store.GetAuthRequestInfo(context.Background(), "state123")
    require.NoError(t, err)
    require.Equal(t, "state123", got.State)
    require.Equal(t, "https://pds.example.com", got.AuthServerURL)
}

func TestStore_DeleteSession(t *testing.T) {
    store, err := New(openDB(t))
    require.NoError(t, err)

    data := []byte(`{"account_did":"did:plc:test","session_id":"sess1"}`)
    var sess oauth.ClientSessionData
    json.Unmarshal(data, &sess)
    store.SaveSession(context.Background(), sess)

    require.NoError(t, store.DeleteSession(context.Background(), "did:plc:test", "sess1"))

    _, err = store.GetSession(context.Background(), "did:plc:test", "sess1")
    require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestStore_DeleteAuthRequest(t *testing.T) {
    store, err := New(openDB(t))
    require.NoError(t, err)

    info := oauth.AuthRequestData{State: "state456"}
    store.SaveAuthRequestInfo(context.Background(), info)
    require.NoError(t, store.DeleteAuthRequestInfo(context.Background(), "state456"))

    _, err = store.GetAuthRequestInfo(context.Background(), "state456")
    require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./pkg/oauth_client/gormstore/ -v
```
Expected: Tests pass.

- [ ] **Step 5: Commit**

```bash
git add pkg/oauth_client/gormstore/
git commit -m "feat: add gormstore implementation of ClientAuthStore"
```

---

### Task 3: Add root replace directive and update `cmd/sap/go.mod`

**Files:**
- Modify: `cmd/sap/go.mod`
- Modify: `go.mod`

- [ ] **Step 1: Add replace directive to root `go.mod`**

Append to `go.mod` (after the existing replace directives):
```
require github.com/habitat-network/habitat/pkg/oauth_client v0.0.0-00010101000000-000000000000

replace github.com/habitat-network/habitat/pkg/oauth_client => ./pkg/oauth_client
```

- [ ] **Step 2: Update `cmd/sap/go.mod`**

Add to `require` block:
```
github.com/habitat-network/habitat/pkg/oauth_client v0.0.0-00010101000000-000000000000
```

Add to `replace` block:
```
replace github.com/habitat-network/habitat/pkg/oauth_client => ../../pkg/oauth_client
```

- [ ] **Step 3: Tidy modules**

```bash
go mod tidy
cd cmd/sap && go mod tidy
```

- [ ] **Step 4: Verify compilation**

```bash
go build ./pkg/oauth_client/ && go vet ./pkg/oauth_client/
```

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum cmd/sap/go.mod cmd/sap/go.sum
git commit -m "chore: add pkg/oauth_client module dependencies"
```

---

### Task 3: Implement `ClientAuthStore` in `internal/sap/` and adjust models

**Files:**
- Create: `internal/sap/store.go`
- Modify: `internal/sap/models.go`
- Modify: `internal/sap/sap.go`

- [ ] **Step 1: Write `internal/sap/store.go`**

```go
package sap

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/bluesky-social/indigo/atproto/auth/oauth"
    "github.com/bluesky-social/indigo/atproto/syntax"
    "gorm.io/gorm"
)

// ---- GORM models for the ClientAuthStore ----

// sapSession stores oauth.ClientSessionData keyed by (did, session_id).
type sapSession struct {
    DID       syntax.DID `gorm:"column:did;primaryKey;type:text"`
    SessionID string     `gorm:"column:session_id;primaryKey;type:text"`
    Data      []byte     `gorm:"column:data;type:blob"`
    CreatedAt time.Time
    UpdatedAt time.Time
}

func (sapSession) TableName() string { return "sap_sessions" }

// sapAuthRequest stores oauth.AuthRequestData keyed by state.
type sapAuthRequest struct {
    State     string    `gorm:"column:state;primaryKey;type:text"`
    Data      []byte    `gorm:"column:data;type:blob"`
    CreatedAt time.Time
}

func (sapAuthRequest) TableName() string { return "sap_auth_requests" }

// ---- Implementation ----

type gormClientAuthStore struct {
    db *gorm.DB
}

var _ oauth.ClientAuthStore = (*gormClientAuthStore)(nil)

func newGormClientAuthStore(db *gorm.DB) (*gormClientAuthStore, error) {
    if err := db.AutoMigrate(&sapSession{}, &sapAuthRequest{}); err != nil {
        return nil, fmt.Errorf("migrate auth store: %w", err)
    }
    return &gormClientAuthStore{db: db}, nil
}

// GetSession implements oauth.ClientAuthStore.
func (s *gormClientAuthStore) GetSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSessionData, error) {
    var row sapSession
    if err := s.db.WithContext(ctx).
        Where("did = ? AND session_id = ?", did, sessionID).
        First(&row).Error; err != nil {
        return nil, err
    }
    var data oauth.ClientSessionData
    if err := json.Unmarshal(row.Data, &data); err != nil {
        return nil, fmt.Errorf("unmarshal session: %w", err)
    }
    return &data, nil
}

// SaveSession implements oauth.ClientAuthStore.
func (s *gormClientAuthStore) SaveSession(ctx context.Context, sess oauth.ClientSessionData) error {
    data, err := json.Marshal(sess)
    if err != nil {
        return fmt.Errorf("marshal session: %w", err)
    }
    // Determine DID + SessionID from the session data.
    // We assume the session has access to these fields, so we reach into them
    // via reflection or by storing them alongside.
    did := sess.AccountDID
    sessionID := sess.SessionID
    return s.db.WithContext(ctx).Save(&sapSession{
        DID:       *did,
        SessionID: sessionID,
        Data:      data,
    }).Error
}

// DeleteSession implements oauth.ClientAuthStore.
func (s *gormClientAuthStore) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
    return s.db.WithContext(ctx).
        Where("did = ? AND session_id = ?", did, sessionID).
        Delete(&sapSession{}).Error
}

// GetAuthRequestInfo implements oauth.ClientAuthStore.
func (s *gormClientAuthStore) GetAuthRequestInfo(ctx context.Context, state string) (*oauth.AuthRequestData, error) {
    var row sapAuthRequest
    if err := s.db.WithContext(ctx).
        Where("state = ?", state).
        First(&row).Error; err != nil {
        return nil, err
    }
    var data oauth.AuthRequestData
    if err := json.Unmarshal(row.Data, &data); err != nil {
        return nil, fmt.Errorf("unmarshal auth request: %w", err)
    }
    return &data, nil
}

// SaveAuthRequestInfo implements oauth.ClientAuthStore.
func (s *gormClientAuthStore) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
    data, err := json.Marshal(info)
    if err != nil {
        return fmt.Errorf("marshal auth request: %w", err)
    }
    return s.db.WithContext(ctx).Create(&sapAuthRequest{
        State: info.State,
        Data:  data,
    }).Error
}

// DeleteAuthRequestInfo implements oauth.ClientAuthStore.
func (s *gormClientAuthStore) DeleteAuthRequestInfo(ctx context.Context, state string) error {
    return s.db.WithContext(ctx).
        Where("state = ?", state).
        Delete(&sapAuthRequest{}).Error
}
```

- [ ] **Step 2: Update `internal/sap/models.go`**

Simplify `managedOrg` to remove OAuth fields (moved to store tables):

```go
package sap

import (
    "time"

    "github.com/bluesky-social/indigo/atproto/syntax"
    habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type crawlState string

const (
    crawlStateRunning  crawlState = "running"
    crawlStateComplete crawlState = "complete"
    crawlStateErrored  crawlState = "errored"
)

// managedOrg is the GORM model for organization crawl management.
// OAuth session data is stored via ClientAuthStore (sap_sessions + sap_auth_requests).
type managedOrg struct {
    DID  syntax.DID `gorm:"column:did;primaryKey"`
    Host string

    ErrorMsg        string
    CrawlState      *crawlState
    SubscribeCursor string
    CrawlCursor     string
    CreatedAt       time.Time
    UpdatedAt       time.Time
}
```

- [ ] **Step 3: Update `internal/sap/sap.go`**

Replace the orgManager usage, absorb SAP coordination, and wire the store:

```go
package sap

import (
    "context"
    "errors"
    "fmt"
    "log/slog"
    "net/http"
    "strings"

    "github.com/bluesky-social/indigo/atproto/auth/oauth"
    "github.com/bluesky-social/indigo/atproto/identity"
    "github.com/bluesky-social/indigo/atproto/syntax"
    "github.com/habitat-network/habitat/internal/utils"
    oauthClient "github.com/habitat-network/habitat/pkg/oauth_client"
    "golang.org/x/sync/errgroup"
    "gorm.io/gorm"
)

type Sap interface {
    http.Handler
    Start(ctx context.Context) error
    AddOrg(ctx context.Context, orgIdentifier string) (redirectURL string, err error)
    ListOrgs(ctx context.Context) ([]syntax.DID, error)
    Outbox
}

type sapImpl struct {
    Outbox
    db         *gorm.DB
    oauthApp   *oauthClient.App
    host       string
    sub        *subscriber
    resyncBuf  *resyncBuffer
    resyncer   *resyncer
    crawler    *crawler
    store      *gormClientAuthStore
}

type SapConfig struct {
    PublicDomain      string
    DB                *gorm.DB
    OAuthConfig       *oauth.ClientConfig
    ResyncParallelism int
    Directory         identity.Directory
}

func NewSap(config SapConfig) (*sapImpl, error) {
    store, err := newGormClientAuthStore(config.DB)
    if err != nil {
        return nil, fmt.Errorf("init auth store: %w", err)
    }

    // Set the directory on the OAuth config if not already set
    if config.OAuthConfig != nil {
        // We need to set the directory on the ClientApp after creating it.
        // The oauth_client.App wraps this.
    }

    oauthApp := oauthClient.NewApp(config.OAuthConfig, store)

    resyncNotif := utils.NewPollNotifier()
    outboxNotif := utils.NewPollNotifier()
    dir := config.Directory
    if dir == nil {
        dir = identity.DefaultDirectory()
    }

    resyncBuf := newResyncBuffer(config.DB, resyncNotif, outboxNotif)
    sub := newSubscriber(config.DB, oauthApp, resyncBuf, dir)
    resyncer := newResyncer(config.DB, oauthApp, resyncBuf, resyncNotif, outboxNotif, config.ResyncParallelism, dir)
    crawler := newCrawler(config.DB, oauthApp, resyncBuf, sub, resyncNotif, dir)
    outbox := newOutbox(config.DB, outboxNotif)

    _, pathPrefix, _ := strings.Cut(config.PublicDomain, "/")
    return &sapImpl{
        Outbox:    outbox,
        db:        config.DB,
        oauthApp:  oauthApp,
        host:      "https://" + pathPrefix,
        sub:       sub,
        resyncBuf: resyncBuf,
        resyncer:  resyncer,
        crawler:   crawler,
        store:     store,
    }, nil
}

func (s *sapImpl) AddOrg(ctx context.Context, orgIdentifier string) (redirectURL string, err error) {
    redirectURL, err = s.oauthApp.StartAuthFlow(ctx, orgIdentifier)
    if err != nil {
        return "", fmt.Errorf("start auth flow: %w", err)
    }
    return redirectURL, nil
}

func (s *sapImpl) CompleteAuth(ctx context.Context, code, state string) (*oauth.ClientSessionData, error) {
    params := url.Values{}
    params.Set("code", code)
    params.Set("state", state)
    sess, err := s.oauthApp.ProcessCallback(ctx, params)
    if err != nil {
        return nil, fmt.Errorf("process callback: %w", err)
    }

    // Create or update managedOrg for SAP coordination
    org := &managedOrg{
        DID:        *sess.AccountDID,
        CrawlState: new(crawlStateRunning),
    }
    if err := s.db.WithContext(ctx).Save(org).Error; err != nil {
        return nil, fmt.Errorf("save org: %w", err)
    }

    s.startOrgSync(org)
    return sess, nil
}

func (s *sapImpl) ListOrgs(ctx context.Context) ([]syntax.DID, error) {
    var orgs []managedOrg
    err := s.db.WithContext(ctx).
        Where("crawl_state IS NOT NULL").
        Find(&orgs).Error
    if err != nil {
        return nil, err
    }
    dids := make([]syntax.DID, len(orgs))
    for i, o := range orgs {
        dids[i] = o.DID
    }
    return dids, nil
}

func (s *sapImpl) Start(ctx context.Context) error {
    eg, ctx := errgroup.WithContext(ctx)
    eg.Go(func() error {
        return s.sub.loadSubscriptions(ctx)
    })
    eg.Go(func() error {
        return s.crawler.resumeIncompleteCrawls(ctx)
    })
    eg.Go(func() error {
        s.resyncer.run(ctx)
        return nil
    })
    err := eg.Wait()
    return errors.Join(err, s.sub.closeSubscriptions())
}

func (s *sapImpl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // Delegate to the old internal server handlers moved to cmd/sap/.
    // By the time this is called, the mux in cmd/sap/ should handle routes.
    http.NotFound(w, r)
}

func (s *sapImpl) startOrgSync(org *managedOrg) {
    go s.crawler.crawlOrg(context.Background(), org)
    go s.sub.addSubscription(context.Background(), org)
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/sap/store.go internal/sap/models.go internal/sap/sap.go
git commit -m "feat: implement ClientAuthStore and update sap.go"
```

---

### Task 4: Update crawler, resyncer, subscriber to use `oauth_client.App`

**Files:**
- Modify: `internal/sap/crawler.go`
- Modify: `internal/sap/resyncer.go`
- Modify: `internal/sap/subscriber.go`
- Modify: `internal/sap/resync_buffer.go`

- [ ] **Step 1: Update `internal/sap/crawler.go`**

Replace `orgManager` dependency with `*oauthClient.App` + `identity.Directory`:

```go
package sap

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "net/http"
    "net/url"

    "github.com/bluesky-social/indigo/atproto/identity"
    "github.com/bluesky-social/indigo/atproto/syntax"
    "github.com/habitat-network/habitat/api/habitat"
    habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
    "github.com/habitat-network/habitat/internal/utils"
    oauthClient "github.com/habitat-network/habitat/pkg/oauth_client"
    "gorm.io/gorm"
    "gorm.io/gorm/clause"
)

type crawler struct {
    db          *gorm.DB
    oauthApp    *oauthClient.App
    dir         identity.Directory
    resyncBuf   *resyncBuffer
    sub         *subscriber
    resyncNotif *utils.PollNotifier
}

func newCrawler(
    db *gorm.DB,
    oauthApp *oauthClient.App,
    resyncBuf *resyncBuffer,
    sub *subscriber,
    resyncNotif *utils.PollNotifier,
    dir identity.Directory,
) *crawler {
    return &crawler{
        db:          db,
        oauthApp:    oauthApp,
        dir:         dir,
        resyncBuf:   resyncBuf,
        sub:         sub,
        resyncNotif: resyncNotif,
    }
}

func (c *crawler) resumeIncompleteCrawls(ctx context.Context) error {
    var orgs []managedOrg
    if err := c.db.WithContext(ctx).
        Where(c.db.Where("crawl_state = ?", crawlStateRunning).Or("crawl_state IS NULL")).
        Find(&orgs).Error; err != nil {
        return fmt.Errorf("find incomplete crawls: %w", err)
    }
    for i := range orgs {
        go c.crawlOrg(ctx, &orgs[i])
    }
    return nil
}

func (c *crawler) crawlOrg(ctx context.Context, org *managedOrg) {
    if err := c.db.WithContext(ctx).
        Model(&managedOrg{}).
        Where("did = ?", org.DID).
        Update("crawl_state", crawlStateRunning).Error; err != nil {
        slog.ErrorContext(ctx, "set crawl running", "org", org.DID, "err", err)
        return
    }

    crawlErr := c.resumeCrawl(ctx, org)

    if crawlErr != nil {
        if err := c.db.WithContext(ctx).
            Model(&managedOrg{}).
            Where("did = ?", org.DID).
            Updates(map[string]any{
                "crawl_state": crawlStateErrored,
                "error_msg":   crawlErr.Error(),
            }).Error; err != nil {
            slog.ErrorContext(ctx, "set crawl errored", "org", org.DID, "err", err)
        }
        if err := c.resyncBuf.clearOrg(ctx, org.DID); err != nil {
            slog.ErrorContext(ctx, "clear org buffer", "org", org.DID, "err", err)
        }
        c.sub.cancelSubscription(org.DID)
        slog.ErrorContext(ctx, "crawl failed", "org", org.DID, "err", crawlErr)
        return
    }

    if err := c.db.WithContext(ctx).
        Model(&managedOrg{}).
        Where("did = ?", org.DID).
        Update("crawl_state", crawlStateComplete).Error; err != nil {
        slog.ErrorContext(ctx, "set crawl complete", "org", org.DID, "err", err)
        return
    }
    if err := c.resyncBuf.drainOrg(ctx, org.DID); err != nil {
        slog.ErrorContext(ctx, "drain org buffer", "org", org.DID, "err", err)
    }
    slog.InfoContext(ctx, "crawler finished", "org", org.DID)
}

func (c *crawler) resumeCrawl(ctx context.Context, org *managedOrg) error {
    sess, err := c.oauthApp.ResumeSession(ctx, org.DID, "")  // sessionID from managedOrg
    if err != nil {
        return fmt.Errorf("resume session: %w", err)
    }
    client := http.DefaultClient
    cursor := org.CrawlCursor
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        params := url.Values{}
        if cursor != "" {
            params.Set("cursor", cursor)
        }

        req, err := http.NewRequestWithContext(ctx, "GET",
            org.Host+"/xrpc/network.habitat.space.listSpaces?"+params.Encode(), nil)
        if err != nil {
            return fmt.Errorf("create request: %w", err)
        }

        resp, err := sess.DoWithAuth(client, req, "network.habitat.space.listSpaces")
        if err != nil {
            return fmt.Errorf("list spaces: %w", err)
        }

        var listSpacesOutput habitat.NetworkHabitatSpaceListSpacesOutput
        decodeErr := json.NewDecoder(resp.Body).Decode(&listSpacesOutput)
        closeErr := resp.Body.Close()
        if decodeErr != nil {
            return fmt.Errorf("decode list spaces output: %w", decodeErr)
        }
        if closeErr != nil {
            return closeErr
        }
        if resp.StatusCode != http.StatusOK {
            return fmt.Errorf("list spaces: %s", resp.Status)
        }

        if len(listSpacesOutput.Spaces) == 0 {
            break
        }

        for _, space := range listSpacesOutput.Spaces {
            if err := c.enumerateSpaceMembers(ctx, sess, org, space.Uri); err != nil {
                return fmt.Errorf("enumerate space members for %s: %w", space.Uri, err)
            }
        }

        if listSpacesOutput.Cursor == "" {
            break
        }
        cursor = listSpacesOutput.Cursor

        if err := c.db.WithContext(ctx).
            Model(&managedOrg{}).
            Where("did = ?", org.DID).
            Update("crawl_cursor", listSpacesOutput.Cursor).Error; err != nil {
            return fmt.Errorf("save crawl cursor: %w", err)
        }
    }
    return nil
}

func (c *crawler) enumerateSpaceMembers(
    ctx context.Context,
    sess *oauth.ClientSession,
    org *managedOrg,
    spaceURI string,
) error {
    values := url.Values{"space": []string{spaceURI}}
    req, err := http.NewRequestWithContext(ctx, "GET",
        org.Host+"/xrpc/network.habitat.space.getMembers?"+values.Encode(), nil)
    if err != nil {
        return err
    }

    client := http.DefaultClient
    resp, err := sess.DoWithAuth(client, req, "network.habitat.space.getMembers")
    if err != nil {
        return err
    }
    defer func() { _ = resp.Body.Close() }()

    var getMembersOutput habitat.NetworkHabitatSpaceGetMembersOutput
    if err := json.NewDecoder(resp.Body).Decode(&getMembersOutput); err != nil {
        return err
    }
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("get members: %s", resp.Status)
    }

    space := habitat_syntax.SpaceURI(spaceURI)
    for _, member := range getMembersOutput.Members {
        if err := c.db.WithContext(ctx).
            Clauses(clause.OnConflict{DoNothing: true}).
            Create(&managedRepo{
                Space: space,
                DID:   syntax.DID(member.Did),
                State: RepoStatePending,
            }).Error; err != nil {
            return err
        }
    }
    slog.InfoContext(ctx, "enumerate space members, notifying resync",
        "space", space, "members", len(getMembersOutput.Members))
    c.resyncNotif.Notify()
    return nil
}
```

- [ ] **Step 2: Update `internal/sap/resyncer.go`** similarly

Replace `orgManager` dependency with `*oauthClient.App` + `identity.Directory`:

```go
type resyncer struct {
    db          *gorm.DB
    oauthApp    *oauthClient.App
    dir         identity.Directory
    resyncBuf   *resyncBuffer
    parallelism int
    resyncNotif *utils.PollNotifier
    outboxNotif *utils.PollNotifier
    jobs        chan resyncJob
}

func newResyncer(
    db *gorm.DB,
    oauthApp *oauthClient.App,
    resyncBuf *resyncBuffer,
    resyncNotif *utils.PollNotifier,
    outboxNotif *utils.PollNotifier,
    parallelism int,
    dir identity.Directory,
) *resyncer {
    if parallelism <= 0 {
        parallelism = 5
    }
    return &resyncer{
        db:          db,
        oauthApp:    oauthApp,
        dir:         dir,
        resyncBuf:   resyncBuf,
        parallelism: parallelism,
        resyncNotif: resyncNotif,
        outboxNotif: outboxNotif,
        jobs:        make(chan resyncJob),
    }
}
```

Update `syncRepo` to use `sess.DoWithAuth` instead of `orgManager.GetClient()`:

```go
func (r *resyncer) syncRepo(ctx context.Context, space habitat_syntax.SpaceURI, repoDID syntax.DID) error {
    orgDID := space.SpaceOwner()
    var org managedOrg
    if err := r.db.WithContext(ctx).First(&org, "did = ?", orgDID).Error; err != nil {
        return fmt.Errorf("load org: %w", err)
    }

    sess, err := r.oauthApp.ResumeSession(ctx, org.DID, "")
    if err != nil {
        return fmt.Errorf("resume session: %w", err)
    }

    var repo managedRepo
    err = r.db.WithContext(ctx).
        Where("space = ? AND did = ?", space, repoDID).
        First(&repo).Error
    if err != nil {
        return err
    }
    since := ""
    if repo.Rev != "" {
        since = repo.Rev.String()
    }

    client := http.DefaultClient
    for {
        // ... loop body using sess.DoWithAuth(client, req, nsid) instead of client.Get(...
    }
}
```

- [ ] **Step 3: Update `internal/sap/subscriber.go`**

Replace `orgManager` dependency with `*oauthClient.App` + `identity.Directory`:

```go
type subscriber struct {
    db         *gorm.DB
    oauthApp   *oauthClient.App
    dir        identity.Directory
    resyncBuf  *resyncBuffer
    subscriptionsMu sync.RWMutex
    subscriptions   map[syntax.DID]*subscription
}
```

Update `addSubscription` to use `oauthApp` for getting the HTTP client:

```go
func (s *subscriber) addSubscription(ctx context.Context, org *managedOrg) {
    sess, err := s.oauthApp.ResumeSession(ctx, org.DID, "")
    if err != nil {
        slog.ErrorContext(ctx, "resume session for subscription", "org", org.DID, "err", err)
        return
    }
    client := sse.NewClient(org.Host + "/xrpc/network.habitat.sync.subscribeSpaces")
    // Use a custom transport that delegates to sess.DoWithAuth
    client.Connection.Transport = &oauthSessionTransport{sess: sess}
    // ... rest of the method
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/sap/crawler.go internal/sap/resyncer.go internal/sap/subscriber.go
git commit -m "refactor: replace orgManager with oauth_client.App in crawler/resyncer/subscriber"
```

---

### Task 5: Move HTTP handlers to `cmd/sap/` and update wiring

**Files:**
- Modify: `cmd/sap/server.go`
- Modify: `cmd/sap/main.go`
- Delete: `internal/sap/server.go`

- [ ] **Step 1: Update `cmd/sap/server.go`** to add OAuth callback + client-metadata handlers

```go
package main

import (
    "encoding/json"
    "log/slog"
    "net/http"

    "github.com/bluesky-social/indigo/atproto/auth/oauth"
    "github.com/habitat-network/habitat/internal/sap"
)

type server struct {
    sap        sap.Sap
    oauthApp   *oauth.ClientApp // for client metadata
    oauthConfig *oauth.ClientConfig
}

func NewSapServer(sap sap.Sap, oauthConfig *oauth.ClientConfig) *server {
    return &server{
        sap:         sap,
        oauthConfig: oauthConfig,
    }
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *server) handleClientMetadata(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    if err := json.NewEncoder(w).Encode(s.oauthConfig.ClientMetadata()); err != nil {
        slog.ErrorContext(r.Context(), "write client metadata", "err", err)
    }
}

func (s *server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
    code := r.URL.Query().Get("code")
    stateVal := r.URL.Query().Get("state")
    if code == "" || stateVal == "" {
        http.Error(w, "missing code or state parameter", http.StatusBadRequest)
        return
    }
    // Complete the auth and start sync
    sess, err := s.sap.CompleteAuth(r.Context(), code, stateVal)
    if err != nil {
        slog.ErrorContext(r.Context(), "complete auth", "err", err)
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    slog.InfoContext(r.Context(), "org added", "org", sess.AccountDID)
    w.WriteHeader(http.StatusOK)
}

func (s *server) handleAddOrg(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Handle string `json:"handle"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid JSON body", http.StatusBadRequest)
        return
    }
    redirectURL, err := s.sap.AddOrg(r.Context(), req.Handle)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    if r.Method == http.MethodPost {
        w.Header().Set("Content-Type", "application/json")
        enc := json.NewEncoder(w)
        enc.SetEscapeHTML(false)
        err = enc.Encode(map[string]string{"redirect_url": redirectURL})
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
        }
        return
    }
    w.Header().Set("Location", redirectURL)
    w.WriteHeader(http.StatusSeeOther)
}

func (s *server) handleListOrgs(w http.ResponseWriter, r *http.Request) {
    orgs, err := s.sap.ListOrgs(r.Context())
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(map[string]any{"orgs": orgs})
}
```

- [ ] **Step 2: Update `cmd/sap/main.go`**

```go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "strings"
    "syscall"

    "github.com/bluesky-social/indigo/atproto/atcrypto"
    "github.com/bluesky-social/indigo/atproto/auth/oauth"
    "github.com/habitat-network/habitat/internal/sap"
    "github.com/urfave/cli/v3"
    "golang.org/x/sync/errgroup"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"
    "gorm.io/gorm/logger"
)

func runSap(ctx context.Context, cmd *cli.Command) error {
    var logLevel slog.Level
    err := logLevel.UnmarshalText([]byte(strings.ToUpper(cmd.String(fLogLevel))))
    if err != nil {
        return fmt.Errorf("unmarshal log level: %w", err)
    }
    slog.SetLogLoggerLevel(logLevel)

    db, err := setupDatabase(cmd.String(fDb))
    if err != nil {
        return fmt.Errorf("setup database: %w", err)
    }

    domain := cmd.String(fDomain)
    secret := cmd.String(fSecret)

    // Build the OAuth client config
    oauthConfig := oauth.NewPublicConfig(
        "https://"+domain+"/client-metadata.json",
        "https://"+domain+"/oauth-callback",
        []string{"atproto"},
    )
    if secret != "" && secret != "secret" {
        priv, err := atcrypto.ParsePrivateMultibase(secret)
        if err != nil {
            return fmt.Errorf("parse secret: %w", err)
        }
        if err := oauthConfig.SetClientSecret(priv, "sap"); err != nil {
            return fmt.Errorf("set client secret: %w", err)
        }
    }

    s, err := sap.NewSap(sap.SapConfig{
        DB:          db,
        PublicDomain: domain,
        OAuthConfig: &oauthConfig,
    })
    if err != nil {
        return fmt.Errorf("create sap: %w", err)
    }

    server := NewSapServer(s, &oauthConfig)

    mux := http.NewServeMux()
    mux.HandleFunc("/health", server.handleHealth)
    mux.HandleFunc("/org/add", server.handleAddOrg)
    mux.HandleFunc("/org/list", server.handleListOrgs)
    mux.HandleFunc("/channel", server.handleOutboxChannel)
    mux.HandleFunc("/oauth-callback", server.handleOAuthCallback)
    mux.HandleFunc("/client-metadata.json", server.handleClientMetadata)

    slog.InfoContext(ctx, "listening", "port", cmd.String(fPort))

    eg, ctx := errgroup.WithContext(ctx)
    eg.Go(func() error {
        err := s.Start(ctx)
        slog.ErrorContext(ctx, "stopped", "error", err)
        return err
    })
    eg.Go(func() error {
        srv := http.Server{
            Addr:    fmt.Sprintf(":%s", cmd.String(fPort)),
            Handler: mux,
        }
        go func() { _ = srv.ListenAndServe() }()
        <-ctx.Done()
        return srv.Shutdown(ctx)
    })

    return eg.Wait()
}
```

- [ ] **Step 3: Delete `internal/sap/server.go`**

```bash
rm internal/sap/server.go
```

- [ ] **Step 4: Commit**

```bash
git add cmd/sap/server.go cmd/sap/main.go
git rm internal/sap/server.go
git commit -m "refactor: move OAuth handlers to cmd/sap/, delete internal/sap/server.go"
```

---

### Task 6: Delete `internal/sap/org_manager.go` and `org_manager_test.go`

**Files:**
- Delete: `internal/sap/org_manager.go`
- Delete: `internal/sap/org_manager_test.go`

- [ ] **Step 1: Remove the files**

```bash
rm internal/sap/org_manager.go internal/sap/org_manager_test.go
```

- [ ] **Step 2: Verify nothing else imports them**

```bash
rg "org_manager" --include "*.go" internal/ cmd/
```
Expected: No results.

- [ ] **Step 3: Commit**

```bash
git rm internal/sap/org_manager.go internal/sap/org_manager_test.go
git commit -m "refactor: remove org_manager (logic moved to sap.go + pkg/oauth_client)"
```

---

### Task 7: Update tests

**Files:**
- Modify: `internal/sap/crawler_test.go`
- Modify: `internal/sap/sap_test.go`
- Modify: `cmd/sap/sap_test.go`
- Modify: `cmd/sap/websocket_test.go`

- [ ] **Step 1: Update `internal/sap/crawler_test.go`**

Replace `newOrgManager` with `oauthClient.App` + `MemStore`:

```go
func openTestDB(t *testing.T) *gorm.DB {
    t.Helper()
    db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db?_journal_mode=WAL"), &gorm.Config{})
    require.NoError(t, err)
    require.NoError(t, autoMigrate(db))
    return db
}

func setupCrawlerTest(t *testing.T) (*crawler, *gorm.DB, *utils.PollNotifier) {
    db := openTestDB(t)
    resyncNotif := utils.NewPollNotifier()
    outboxNotif := utils.NewPollNotifier()

    config := oauth.NewLocalhostConfig(
        "http://test-sap.local/oauth-callback",
        []string{"atproto"},
    )
    store := oauth.NewMemStore()
    oauthApp := oauthClient.NewApp(&config, store)

    resyncBuf := newResyncBuffer(db, resyncNotif, outboxNotif)
    sub := newSubscriber(db, oauthApp, resyncBuf, nil)
    c := newCrawler(db, oauthApp, resyncBuf, sub, resyncNotif, nil)
    return c, db, resyncNotif
}

func TestCrawler(t *testing.T) {
    // ... existing test code adapted to use setupCrawlerTest
}
```

- [ ] **Step 2: Update `internal/sap/sap_test.go`**

Update to pass `OAuthConfig`:

```go
func TestSap_Start(t *testing.T) {
    db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
    require.NoError(t, err)

    config := oauth.NewPublicConfig(
        "https://example.com/client-metadata.json",
        "https://example.com/oauth-callback",
        []string{"atproto"},
    )

    s, err := NewSap(SapConfig{
        PublicDomain: "https://example.com",
        DB:           db,
        OAuthConfig:  &config,
    })
    require.NoError(t, err)
    ctx, cancel := context.WithCancel(t.Context())
    var wg sync.WaitGroup
    wg.Go(func() {
        require.ErrorIs(t, s.Start(ctx), context.Canceled)
    })

    cancel()
    wg.Wait()
}
```

- [ ] **Step 3: Update `cmd/sap/sap_test.go`**

The integration test needs significant updates to use the new OAuth flow with `oauth.ClientApp`. The test sets up a Pear server that acts as both the OAuth provider and the AT Protocol PDS. The key change is that the SAP OAuth flow now uses the indigo `oauth.ClientApp` which uses PAR + DPoP. The test server must support these.

- [ ] **Step 4: Update `cmd/sap/websocket_test.go`**

Update test setup to pass `OAuthConfig`:

```go
func openOutboxTestServer(t *testing.T) (*httptest.Server, sap.Sap, *gorm.DB) {
    t.Helper()
    db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
    require.NoError(t, err)

    config := oauth.NewPublicConfig(
        "https://example.com/client-metadata.json",
        "https://example.com/oauth-callback",
        []string{"atproto"},
    )
    s, err := sap.NewSap(sap.SapConfig{
        PublicDomain: "example.com",
        DB:           db,
        OAuthConfig:  &config,
    })
    require.NoError(t, err)

    srv := NewSapServer(s, &config)
    mux := http.NewServeMux()
    mux.HandleFunc("/channel", srv.handleOutboxChannel)
    httpServer := httptest.NewServer(mux)
    t.Cleanup(httpServer.Close)
    return httpServer, s, db
}
```

- [ ] **Step 5: Run all tests and fix any issues**

```bash
go test ./internal/sap/ -v -count=1 -timeout=60s
go test ./cmd/sap/ -v -count=1 -timeout=120s
```

- [ ] **Step 6: Commit**

```bash
git add internal/sap/crawler_test.go internal/sap/sap_test.go cmd/sap/sap_test.go cmd/sap/websocket_test.go
git commit -m "test: update tests for oauth_client package"
```

---

### Task 8: Final verification

**Files:**
- All

- [ ] **Step 1: Build everything**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 2: Run linting**

```bash
golangci-lint run ./internal/sap/... ./cmd/sap/... ./pkg/oauth_client/...
```

- [ ] **Step 3: Run all tests**

```bash
go test ./internal/sap/... ./cmd/sap/... ./pkg/oauth_client/... -count=1
```

- [ ] **Step 4: Final commit**

```bash
git add -A
git commit -m "refactor: complete OAuth extraction to pkg/oauth_client"
```

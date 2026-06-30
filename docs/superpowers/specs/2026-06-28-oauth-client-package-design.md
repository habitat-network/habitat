# `pkg/oauth_client` — Extracted OAuth Client SDK

## Motivation

Extract OAuth logic from `internal/sap/org_manager.go` into a standalone, reusable Go
package at `pkg/oauth_client/` with its own `go.mod`. The package wraps
`github.com/bluesky-social/indigo/atproto/auth/oauth` and is persistence-agnostic —
callers implement `oauth.ClientAuthStore`.

## API

```go
package oauth_client

import "github.com/bluesky-social/indigo/atproto/auth/oauth"

// App wraps oauth.ClientApp, providing Habitat's OAuth client entrypoint.
type App struct {
    inner *oauth.ClientApp
}

// NewApp creates an App. Callers provide the config (directly from indigo's
// oauth.NewPublicConfig / NewLocalhostConfig) and a persistent store.
func NewApp(config *oauth.ClientConfig, store oauth.ClientAuthStore) *App

// StartAuthFlow resolves identifier (handle, DID, or auth server URL) and
// returns a redirect URL for the user's browser.
func (a *App) StartAuthFlow(ctx context.Context, identifier string) (string, error)

// ProcessCallback handles OAuth callback query params, exchanges code for
// tokens, persists session data via ClientAuthStore.
func (a *App) ProcessCallback(ctx context.Context, params url.Values) (*oauth.ClientSessionData, error)

// ResumeSession fetches and returns an existing session from the store.
func (a *App) ResumeSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSession, error)

// Logout revokes tokens (if AS supports it) and deletes session from store.
func (a *App) Logout(ctx context.Context, did syntax.DID, sessionID string) error
```

## Files

```
pkg/oauth_client/
  go.mod              — module github.com/habitat-network/habitat/pkg/oauth_client
  oauth_client.go     — App struct, NewApp
  oauth_client_test.go
```

## Changes to internal/sap/

- **`org_manager.go` removed** — OAuth logic gone. The SAP coordination
  (crawl state, subscription lifecycle) is absorbed into `sap.go`.
- **`server.go` removed** — `/oauth-callback` and `/client-metadata.json`
  handlers move to `cmd/sap/server.go`.
- **`sap.go`** — `SapConfig` loses `Secret` field. `NewSap` takes an
  `*oauth_client.App` + implements `oauth.ClientAuthStore` via GORM on
  `managedOrg`.
- **`models.go`** — `managedOrg` schema adjusts to accommodate
  `oauth.ClientSessionData` fields (DPoP keys, session ID, multi-session).
- **Crawler/resyncer** — use `oauth.ClientSession.APIClient()` or
  `sess.DoWithAuth()` instead of `orgManager.GetClient()`.

## Changes to cmd/sap/

- **`server.go`** — gains `/oauth-callback` and `/client-metadata.json` handlers.
  `/client-metadata.json` serves `config.ClientMetadata()` directly.
- **`main.go`** — wires `oauth_client.App` with `oauth.ClientConfig` (using
  `oauth.NewPublicConfig`) and passes it to `NewSap`.
- **`go.mod`** — adds dependency on `pkg/oauth_client`.
- **Tests** — update to use new wiring.

## Persistence boundary

`oauth.ClientAuthStore` interface (unchanged from indigo):
```go
type ClientAuthStore interface {
    GetSession(ctx, did, sessionID) (*ClientSessionData, error)
    SaveSession(ctx, ClientSessionData) error
    DeleteSession(ctx, did, sessionID) error
    GetAuthRequestInfo(ctx, state) (*AuthRequestData, error)
    SaveAuthRequestInfo(ctx, AuthRequestData) error
    DeleteAuthRequestInfo(ctx, state) error
}
```

Callers (e.g. `internal/sap/` via GORM) implement this interface.
`pkg/oauth_client` has zero knowledge of the backing store.

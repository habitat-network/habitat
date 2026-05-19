# OAuth Login Methods Design

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor the OAuth authorize flow to route login providers based on an org's login method instead of inspecting identity services via `CanHandle`.

**Architecture:** The `Provider` interface loses `CanHandle` and `Type()`, gaining `LoginMethod() string`. The `Router` dispatches by login method (atproto, google, password) instead of probing identity services. Each org stores its login method, and the OAuth server looks up the org during `HandleAuthorize` to determine the provider. The `pdsProvider` gets a mapping store for org DID → public ATProto DID, enabling org-scoped PDS auth. A new `googleProvider` stubs out Google Sign-In with a similar mapping.

**Tech Stack:** Go, Fosite (OAuth 2.0), GORM, AT Protocol (indigo)

---

## Changes

### 1. Org model — LoginMethod field

**File:** `internal/org/models.go`

Add `LoginMethod` column to the `organization` struct. New orgs created via `CreateOrg` default to `"password"`.

```go
type organization struct {
    ID            string `gorm:"primaryKey"`
    Name          string
    LoginMethod   string // "atproto", "google", "password"
    SigningSecret string
    CreatedAt     time.Time
}
```

### 2. Org interface — add LoginMethod()

**File:** `internal/org/org.go`

Add `LoginMethod() string` to the `Org` interface.

**File:** `internal/org/public.go`

`everyoneOrg.LoginMethod()` returns `"atproto"`.

**File:** `internal/org/store.go`

The `orgImpl.LoginMethod()` reads from the `organization.LoginMethod` in the DB. `GetOrgForDID` and `CreateOrg` propagate login method. `CreateOrg` stores `"password"` as default for new orgs. `orgFromModel` maps it from the model.

### 3. Provider interface — remove CanHandle and Type()

**File:** `internal/login/provider.go`

New interface:

```go
type Provider interface {
    LoginMethod() string
    Authorize(ctx context.Context, id *identity.Identity) (redirectUri string, state []byte, err error)
    Exchange(ctx context.Context, did syntax.DID, code string, issuer string, state []byte) error
}
```

The `LoginMethod()` replaces both `Type()` and `CanHandle(id)`.

**Router changes:**

```go
type Router struct {
    providers map[string]Provider
}

func NewRouter(providers ...Provider) *Router
// ByLoginMethod returns the provider registered for the given login method.
func (r *Router) ByLoginMethod(method string) (Provider, error)
```

Registration is explicit: each provider declares its login method in `LoginMethod()`. Remove `For(id)` (was CanHandle-based) and `ByType()`.

The `ProviderType` and constants are removed.

### 4. pdsProvider — mapping store for org DID → public ATProto DID

**File:** `internal/login/pds.go`

Requires a `PublicDIDMappingStore` interface and an `identity.Directory` to resolve public DIDs:

```go
type PublicDIDMappingStore interface {
    GetPublicDID(ctx context.Context, orgDID syntax.DID) (*syntax.DID, error)
}
```

In `Authorize`:
- Look up the mapping for the caller's DID (which is the org DID)
- If a mapping exists, resolve the public DID via directory and use that identity's PDS service for the OAuth flow
- If no mapping (everyone org), use the passed identity directly (current behavior)

Constructor gains `mappingStore PublicDIDMappingStore` and `dir identity.Directory`. The `LoginMethod()` returns `"atproto"`.

`Authorize(ctx, id)` signature stays the same — the org DID is `id.DID`.

### 5. googleProvider — new

**File:** `internal/login/google.go` (new)

```go
type GoogleEmailMappingStore interface {
    GetEmail(ctx context.Context, orgDID syntax.DID) (string, error)
}
```

Stub implementation:
- `LoginMethod()` returns `"google"`
- `Authorize(ctx, id)` — looks up email mapping for `id.DID`, configures Google OAuth URL with `login_hint`, returns redirect. Falls back to empty/no-op if no mapping.
- `Exchange(ctx, did, code, issuer, state)` — handles Google OAuth callback, stores credentials. Stub implementation that returns nil (no-op for now).

### 6. HandleAuthorize — org-based provider routing

**File:** `internal/oauthserver/oauth_server.go`

Replace `authRequestFlash.ProviderType` with `LoginMethod string`. Current flow:

```go
id, _ := o.directory.Lookup(ctx, atid)
provider, _ := o.loginRouter.For(id)   // CanHandle-based
redirect, state, _ := provider.Authorize(ctx, id)
flash := &authRequestFlash{
    ProviderType: provider.Type(),
    ...
}
```

New flow:

```go
id, _ := o.directory.Lookup(ctx, atid)
org, _ := o.orgStore.GetOrgForDID(ctx, id.DID)
provider, _ := o.loginRouter.ByLoginMethod(org.LoginMethod())
redirect, state, _ := provider.Authorize(ctx, id)
flash := &authRequestFlash{
    LoginMethod: provider.LoginMethod(),
    ...
}
```

### 7. HandleCallback — login method routing

**File:** `internal/oauthserver/oauth_server.go`

Current:

```go
o.orgStore.GetOrgForDID(r.Context(), arf.Did)   // membership check
provider, _ := o.loginRouter.ByType(arf.ProviderType)
provider.Exchange(ctx, arf.Did, code, issuer, arf.ProviderState)
```

New:

```go
o.orgStore.GetOrgForDID(r.Context(), arf.Did)   // membership check
provider, _ := o.loginRouter.ByLoginMethod(arf.LoginMethod)
provider.Exchange(ctx, arf.Did, code, issuer, arf.ProviderState)
```

### 8. Wire-up in cmd/pear/main.go

```go
pdsProvider := login.NewPDSProvider(pdsOAuthClient, pdsCredStore, mappingStore, directory)
googleProvider := login.NewGoogleProvider(googleMappingStore, googleOAuthConfig) // stub
passwordProvider := org.NewLoginProvider(orgStore, frontendDomain, oauthSecret)
loginRouter := login.NewRouter(pdsProvider, googleProvider, passwordProvider)
```

Remove the `LoginProvider` being passed to `login.NewRouter` — actually it stays, but its `LoginMethod()` now returns `"password"` explicitly.

**Note:** `org.NewLoginProvider` needs a `LoginMethod() string` method added that returns `"password"`.

---

## File Change Summary

| File | Change |
|---|---|
| `internal/org/models.go` | Add `LoginMethod` column to `organization` |
| `internal/org/org.go` | Add `LoginMethod() string` to `Org` interface |
| `internal/org/store.go` | Implement `LoginMethod()` on `orgImpl`, update `orgFromModel`, `CreateOrg`, `GetOrgForDID` |
| `internal/org/public.go` | Add `LoginMethod() string` to `everyoneOrg` (returns `"atproto"`) |
| `internal/login/provider.go` | Remove `CanHandle`, `Type()`, `ProviderType`. Add `LoginMethod() string`. Rewrite Router to use `map[string]Provider` with `ByLoginMethod()` |
| `internal/login/pds.go` | Add `PublicDIDMappingStore`, `identity.Directory` to struct. Update `Authorize` for org mapping |
| `internal/login/google.go` | New file — `googleProvider` stub |
| `internal/oauthserver/oauth_server.go` | Update `authRequestFlash` (ProviderType → LoginMethod), `HandleAuthorize` and `HandleCallback` to use org-based routing |
| **Possibly** `cmd/pear/main.go` | Update wiring |

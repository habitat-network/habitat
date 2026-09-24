# OverrideDirectory design

Date: 2026-09-24

## Summary

Extract the DID-override and email-resolution logic currently embedded in
`internal/identity/server.go` into a new `OverrideDirectory` type that
implements indigo's `identity.Directory` interface. The identity server's
resolve handlers are then reduced to thin lookups against that directory plus
HTTP error mapping.

## Background

`internal/identity/server.go` currently mixes HTTP handling with two lookuplogic concerns:

- `overriddenDidDoc` rewrites a resolved identity's DID document so its
  `#atproto_pds` service points at this habitat instance, unless the
  identity's real PDS already supports the atproto spaces protocol (probed
  via `utils.SupportsSpaces`).
- `parseEmail` / `WithEmailResolver` let the resolve handlers accept a work
  email in place of a handle, minting an identity on first sight via
  `EmailResolver.ResolveEmailIdentity`.

Both concerns live in the server and are duplicated across `ResolveDID`,
`ResolveHandle`, and `ResolveIdentity`. We want the lookup logic to live in a
single `identity.Directory` implementation so the server just invokes it.

## Constraint: emails are not part of the Directory interface

indigo's `identity.Directory` interface (LookupHandle / LookupDID / Lookup /
Purge) only accepts `syntax.Handle`, `syntax.DID`, and `syntax.AtIdentifier`.
None of these can represent an email (`alice@acme.com` is not a valid
handle/at-identifier). Therefore:

- Email resolution cannot be an interface method.
- The concrete directory exposes an additional non-interface method
  (`LookupIdentifier`) that dispatches on an identifier string.
- The server handlers keep only their thin error-to-HTTP mapping (not-found
  vs invalid vs server error), which differs per handler today and must stay
  that way.

## Architecture

New type `OverrideDirectory` in a new file `internal/identity/override_dir.go`.

```go
type OverrideDirectory struct {
    base          identity.Directory // the existing WrappedDirectory chain
    emailResolver *EmailResolver     // optional
    domain        string             // habitat service domain, e.g. "pear.domain"
    httpClient    *http.Client       // for the SupportsSpaces probe
}
```

Constructor mirrors the existing option style:

```go
func NewOverrideDirectory(
    base identity.Directory,
    domain string,
    opts ...utils.Opt[OverrideDirectory],
) *OverrideDirectory
```

`WithClient` and `WithEmailResolver` become `utils.Opt[OverrideDirectory]` (no
change to their exported names or call sites).

### Interface methods

- `LookupDID(ctx, did)`, `LookupHandle(ctx, handle)`, `Lookup(ctx, atid)`:
  delegate to `base`, then apply the override to the returned identity.
- `Purge(ctx, atid)`: delegate to `base` (no override).

### Email / identifier methods

- `LookupEmail(ctx, email emaildomain.Email) (*identity.Identity, error)`:
  requires a configured `emailResolver`, else returns `identity.ErrInvalidHandle`.
  Calls `emailResolver.ResolveEmailIdentity` and applies the override on success.
  Unmapped email domains surface as `identity.ErrDIDNotFound` (as today).
- `LookupIdentifier(ctx, identifier string) (*identity.Identity, error)`:
  convenience dispatch used by the resolve handlers.
  - `syntax.ParseAtIdentifier(identifier)` succeeds → `Lookup`.
  - else `emaildomain.ParseEmail(identifier)` succeeds → `LookupEmail`.
  - else → `identity.ErrInvalidHandle`.

### DID override

```go
func (d *OverrideDirectory) applyOverride(ctx context.Context, ident *identity.Identity) *identity.Identity
```

- If `utils.SupportsSpaces(ctx, d.httpClient, ident)` → return `ident` unchanged.
- Else return a **new** identity with the same `DID`, `Handle`, `AlsoKnownAs`,
  and `Keys`, but `Services` replaced with
  `{"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: "https://" + d.domain}}`.

The override must return a copy, never mutate the identity handed back by the
base directory: indigo's `CacheDirectory` serves cached pointers, and mutating
`Services` in place would poison the cache for other consumers of the same
directory chain.

`ident.DIDDocument()` on the overridden identity produces byte-for-byte the
same document today's `overriddenDidDoc` returns (single `#atproto_pds`
service, keys and alsoKnownAs preserved).

## Server changes (`internal/identity/server.go`)

- `Server` field `directory` becomes `*OverrideDirectory`.
- Drop the `parseEmail` helper, the `overriddenDidDoc` method, and the
  `emailResolver`, `domain`, and `httpClient` fields (now owned by the
  directory).
- `NewServer` constructs
  `NewOverrideDirectory(NewWrappedDirectory(hive, identity.DefaultDirectory()), domain, opts...)`.
  Its `opts` parameter type changes to `...utils.Opt[OverrideDirectory]`
  (call sites in `cmd/pear` and the integration test pass no-arg or the same
  named option functions, so they compile unchanged).
- `GetServiceAuth`, `ServeDIDDoc`, `ServeHandle` are unchanged (they use
  `hive` directly).

Resolve handlers become thin:

- `ResolveDID`: `LookupDID(ctx, did)` → serialize `ident.DIDDocument()`.
  Errors: `ErrDIDNotFound` → 404 `DidNotFound`, else 500.
- `ResolveIdentity`: `LookupIdentifier(ctx, identifier)` → serialize
  `ident.DIDDocument()` with `Did`/`Handle`. Errors: `ErrDIDNotFound` →
  404 `DidNotFound`, `ErrHandleNotFound` → 404 `HandleNotFound`,
  `ErrInvalidHandle` → 400 `invalid identifier`, else 500.
- `ResolveHandle`: reject input that parses as a DID (400 `invalid handle`,
  preserving today's behavior), then `LookupIdentifier(ctx, handleStr)` →
  serialize the DID. Errors: `ErrDIDNotFound` (unmapped email domain) →
  `ErrHandleNotFound` → 404 `HandleNotFound`, `ErrInvalidHandle` → 400
  `invalid handle`, else 500.

Note `orgStore` on `Server` is vestigial (stored, never read). It stays; not
in scope.

## Behavioral parity

Preserved exactly:

- Override conditions (SupportsSpaces probe) and resulting DID document.
- Email mint-on-first-sight semantics, including the `ErrDIDNotFound` (DidNotFound) vs `ErrHandleNotFound` distinction between the two handlers.
- 400-on-invalid-input behavior, including when the email resolver is disabled.

Changed (internal only):

- The override now lives in the directory rather than the handler.
- Cache-safety: the override never mutates base-directory identities.

## Testing

New `internal/identity/override_dir_test.go`:

- Override applied when the PDS does not support spaces: mock identity with a
  404 PDS → returned identity's `DIDDocument()` has `#atproto_pds` pointing at
  the override domain.
- Override skipped when the PDS supports spaces (getSpace returns 200 or
  `InvalidRequest`).
- `LookupIdentifier` handles DID, handle, and email inputs; unknown email
  domain → `ErrDIDNotFound`; invalid identifier → `ErrInvalidHandle`.
- Email lookup mints an identity and applies the override, matching
  `EmailResolver` semantics.
- `Purge` delegates to the base directory.

Updated fixtures: `server_test.go` `testResolveServer` and `email_test.go`
`emailServer` wrap their current directory in `NewOverrideDirectory` with the
client/resolver options. Existing handler assertions must pass unchanged.

## Verification

- `go test ./internal/identity/...`
- `golangci-lint run` (or `moon :lint-check`)
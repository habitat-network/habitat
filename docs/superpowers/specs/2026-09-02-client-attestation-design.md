# Client Attestation — Design

Status: approved for planning
Date: 2026-09-02

## Summary

Implement client attestation as defined in the [AT Protocol permissioned-data
proposal](https://github.com/bluesky-social/proposals/blob/main/0016-permissioned-data/README.md#client-attestation):
a short-lived, self-signed JWT that lets an OAuth client prove its app
identity (`client_id`) to a space authority independently of the user
identity established by the delegation token. Habitat acts only as a space
**authority verifying** attestations presented to
`network.habitat.space.getSpaceCredential` — Habitat does not need to
*produce* attestations of its own for this work (out of scope; see below).

This closes the existing stub in `internal/spaces/server/server.go`
(`GetSpaceCredential` currently returns `NotSupported` whenever
`clientAttestation` is non-empty) and makes the app-identity gating
described by the proposal's `appAccess` concept actually enforceable.

## Out of scope

- Habitat acting as a *client* generating its own attestation when calling
  another authority's `getSpaceCredential` (relevant to future
  cross-instance/p2p credential requests, not needed today).
- Replay protection for the attestation `jti`. Attestations are short-lived
  (seconds) and single-space-scoped; a replay window on that order is an
  accepted risk for this iteration. The `jti` claim is still required and
  format-checked so a replay-protection store can be added later without a
  claims-shape change.
- The simplespace-specific `policy` (public/member-list/managing-app) user
  authorization concept from `network.habitat.simplespace.defs#spaceConfig`.
  That's a pre-existing, separate gap in `GetSpaceCredential`'s user-side
  authorization and is not touched by this work.

## Data model — `network.habitat.space.appAccess` record

A new lexicon, `network.habitat.space.appAccess`, defines a record type
(not a procedure). One record represents one allowed OAuth client for a
space:

- Lives in the space owner's repo, collection
  `network.habitat.space.appAccess`, alongside the space's other records.
- `rkey` = base64url (no padding) encoding of the client's `client_id` URL.
  This charset (`A-Za-z0-9-_`) is a valid subset of atproto's `RecordKey`
  syntax, and makes the grant addressable/idempotent per client without a
  lookup index.
- Record body: no required fields beyond `$type`. The client identity is
  fully recoverable from the rkey. An optional `note` string field may be
  added for admin-UI display purposes, but it is not load-bearing.
- **Semantics: presence, not a policy enum, drives enforcement.** If a
  space's `network.habitat.space.appAccess` collection is non-empty, the
  space is in allow-list mode — client attestation becomes mandatory, and
  the verified `client_id` must have a matching grant record. If the
  collection is empty, the space is open: attestation is optional (verified
  if present, per proposal, but never required).
- Grants are written and deleted through the **existing generic**
  `network.habitat.space.putRecord` / `deleteRecord` procedures — no new
  procedure is introduced. Write access is gated the same way as other
  space-management record writes, by the existing space-role check
  (`SpaceRoleManager`/`SpaceRoleOwner`), reusing `internal/authn`'s existing
  `WithSpace(...)` validator option. No new authorization concept is needed.

This keeps `appAccess` a property of the generic `network.habitat.space`
record/repo abstraction (usable by any space type, not just simplespace),
sidestepping the need for a new space-config table or a simplespace-specific
hook into `internal/spaces/server`.

## Attestation JWT verification

Per the proposal, the attestation JWT has header
`{"typ": "atproto-client-attestation+jwt", "alg": "ES256", "kid": "..."}`
and payload `{iss, sub, aud, iat, exp, jti}` where `iss == sub == client_id`.

### Shared resolver: `internal/clientmeta`

A new package, imported by both `internal/oauthserver` and
`internal/spaces/server`, providing client-id-metadata-document + JWKS
resolution:

- Fetch `client-metadata.json` for a `client_id` URL over HTTP.
- Carry over the existing localhost-dev-client synthesis behavior from
  `internal/oauthserver/localhost.go` (so local dev/testing works the same
  way it already does for the JWT-bearer grant path).
- Extract the client's JWKS (inline `jwks` field, or fetch `jwks_uri`).
- Locate a key by `kid` and convert it from atcrypto JWK to a usable
  verification key (reusing the conversion logic pattern from
  `internal/oauthserver/fosite_storage.go`'s `atcryptoJWKtoJose`, generalized
  out of that file).

`internal/oauthserver`'s existing RFC7523 JWT-bearer-grant key resolution
(`fosite_storage.go`) is refactored to call into this shared package rather
than duplicating fetch/convert logic. Its own semantics (hardcoded
`approvedJwtBearerClients` allow-list, JWT-bearer-grant-specific claims)
stay in `oauthserver` unchanged.

### Attestation-specific verification

New code (in `internal/spaces`, alongside the space-credential minting
logic) that, given a raw attestation JWT and the space's owner DID:

1. Parses the JWT header; rejects if `typ` isn't
   `atproto-client-attestation+jwt` or `alg` isn't `ES256`.
2. Resolves the signing key via `internal/clientmeta`, using the header's
   `kid` and the claimed `iss` as the client_id.
3. Verifies the signature.
4. Validates claims:
   - `iss == sub` (both treated as the verified `client_id`)
   - `aud == "<spaceOwnerDID>#atproto_space_host"`
   - `iat` and `exp` present, `exp` after `iat`, `exp` not in the past, and
     the `exp - iat` window bounded to a short max (reject anomalously
     long-lived attestations)
   - `jti` present and non-empty (format-checked only; no replay store, see
     Out of scope)
5. Returns the verified `client_id` (the `iss`) on success, or a typed
   error distinguishing "malformed/unverifiable attestation" (→
   `InvalidClientAttestation`) from other failure classes.

No new signing key or Habitat-side JWT minting is needed for this feature —
Habitat only verifies attestations produced by clients.

## `GetSpaceCredential` changes

`internal/spaces/server/server.go`'s `GetSpaceCredential` handler:

```
GetSpaceCredential(space, clientAttestation, delegationToken):
  verify delegationToken as today (unchanged) — user-side auth via
    s.validator.Request(WithMethods(DelegationToken), WithSpace(space, Reader))

  hasGrants := store.CollectionNonEmpty(space, "network.habitat.space.appAccess")

  if clientAttestation == "":
    if hasGrants:
      return error InvalidClientAttestation  // space requires attestation
    // else: open space, proceed exactly as today

  else:
    claims, err := attestation.Verify(clientAttestation, spaceOwnerDID)
    if err != nil:
      return error InvalidClientAttestation
    if hasGrants:
      rkey := base64url(claims.ClientID)
      if !store.RecordExists(space, "network.habitat.space.appAccess", rkey):
        return error AppNotAuthorized
    // else: space is open; attestation was verified but no allow-list
    // check applies — accept

  mint and return space credential exactly as today
```

Both `InvalidClientAttestation` and `AppNotAuthorized` are already declared
on the `getSpaceCredential` lexicon — no lexicon error changes needed there.
The `httpx.WriteNotSupported` stub is removed.

`CollectionNonEmpty` / `RecordExists`-shaped helpers are added to (or
composed from existing methods on) `internal/spaces`' store — these are
small, targeted reads against the same repo storage other record reads
already use; no new storage backend or schema.

## Testing

Following the `go-tdd` skill, table-driven tests for:

- `internal/clientmeta`: metadata-document fetch (happy path, localhost dev
  synthesis, 404/malformed), JWKS resolution (inline `jwks`, `jwks_uri`
  fetch), key lookup by `kid` (found, missing, wrong curve).
- Attestation verification: valid attestation; wrong/missing `kid`; bad
  signature; `iss != sub`; wrong `aud`; expired; `exp` far in the future;
  missing `jti`; wrong `typ`/`alg`.
- `GetSpaceCredential` end-to-end (existing test file in
  `internal/spaces/server`): open space with no attestation (unchanged
  behavior), open space with a valid attestation (accepted, no allow-list
  check), allow-list space with a valid+granted attestation (accepted),
  allow-list space with a valid but non-granted attestation
  (`AppNotAuthorized`), allow-list space with no attestation
  (`InvalidClientAttestation`), allow-list space with an invalid/expired
  attestation (`InvalidClientAttestation`).
- Grant lifecycle: adding/removing an `appAccess` record via the existing
  `PutRecord`/`DeleteRecord` procedures, gated to
  `SpaceRoleManager`/`SpaceRoleOwner` (permission-denied case included).

## Open items carried into implementation planning

- Exact shared-package boundary for `internal/clientmeta` (what moves out
  of `oauthserver/fosite_storage.go` vs. what stays) is an implementation
  detail to work out during planning/execution, not a design blocker.
- Whether an optional `note` field belongs on the `appAccess` record now or
  can be added later without a breaking lexicon change (it can — additive,
  non-breaking) — deferred; not included in the initial implementation.

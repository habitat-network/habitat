# Serve spaces endpoints under `com.atproto` in `internal/pearserver`

Date: 2026-09-17

## Context

Habitat served the permissioned-data endpoints from [proposal 0016](https://github.com/bluesky-social/proposals/blob/main/0016-permissioned-data/README.md) under the `network.habitat` namespace (e.g. `network.habitat.space.listRepos`) while the proposal was WIP. Now that the proposal is more hardened and the atproto lexicons are stabilizing, we want to also serve the same endpoints under the proposal's official `com.atproto` namespace so third-party tooling can target them.

## Approach

**Pure route aliasing.** Register additional `com.atproto.*` paths in `internal/pearserver/routes.go` that dispatch to the existing handlers. No handler changes, no lexicon/codegen changes, no schema work. The `network.habitat.*` registrations stay; both namespaces remain live and behave identically.

The handlers are NSID-agnostic: they parse request bodies independently and never read the endpoint NSID from the request path. The one path-sensitive path is `authn/service_auth.go:61`, which derives the NSID from the request URL to validate a service-auth token's `lxm` claim — this works correctly for the `com.atproto.*` paths as long as clients mint service-auth tokens with the `com.atproto.*` NSID, which is the correct behavior.

## Endpoint mapping

| `network.habitat` (existing) | `com.atproto` alias |
|---|---|
| `network.habitat.space.listSpaces` | `com.atproto.space.listSpaces` |
| `network.habitat.space.listRepos` | `com.atproto.space.listRepos` |
| `network.habitat.space.putRecord` | `com.atproto.space.putRecord` |
| `network.habitat.space.getRecord` | `com.atproto.space.getRecord` |
| `network.habitat.space.getBlob` | `com.atproto.space.getBlob` |
| `network.habitat.space.listRecords` | `com.atproto.space.listRecords` |
| `network.habitat.space.deleteRecord` | `com.atproto.space.deleteRecord` |
| `network.habitat.space.listRepoOps` | `com.atproto.space.listRepoOps` |
| `network.habitat.space.getLatestCommit` | `com.atproto.space.getLatestCommit` |
| `network.habitat.space.getRepo` | `com.atproto.space.getRepo` |
| `network.habitat.space.getDelegationToken` | `com.atproto.space.getDelegationToken` |
| `network.habitat.space.getSpaceCredential` | `com.atproto.space.getSpaceCredential` |
| `network.habitat.repo.uploadBlob` | `com.atproto.repo.uploadBlob` |
| `network.habitat.simplespace.createSpace` | `com.atproto.simplespace.createSpace` |
| `network.habitat.simplespace.addMember` | `com.atproto.simplespace.putMember` |
| `network.habitat.simplespace.removeMember` | `com.atproto.simplespace.removeMember` |
| `network.habitat.simplespace.listMembers` | `com.atproto.simplespace.listMembers` |
| `network.habitat.simplespace.deleteSpace` | `com.atproto.simplespace.deleteSpace` |

Note the `addMember` -> `putMember` rename: the proposal's `com.atproto.simplespace.putMember` is our `addMember`, and the proposal's `putMember` carries `read`/`write` booleans that our implementation does not support. Known deviations (policy/capacity gaps in `simplespace`) already exist and are documented in `api-docs/docs/space-proxy/endpoints.mdx`; this change does not attempt to close them.

## Behavior

- Responses, errors, and auth are byte-for-byte identical to the `network.habitat` counterparts.
- Service-auth: clients must mint their token's `lxm` as the `com.atproto.*` NSID when calling the new paths (correct per the proposal; no code change needed because the NSID is read from the request path).
- Space credentials and DPoP are path-agnostic.

## Implementation

- In `internal/pearserver/routes.go`, after the existing Spaces and Simplespace blocks, add a single `com.atproto` aliasing block registering each alias path with the corresponding handler method, with a short comment explaining the aliasing and the `putMember` naming.
- No other files change.

## Testing

Add `internal/pearserver/routes_test.go`:

- A table mapping each alias path to its `network.habitat` counterpart.
- For each pair, use `router.Match` with a request for each path and assert both resolve to the same handler (reflect func-pointer comparison).
- Assert every alias path is distinct within the table (no duplicate registrations).

## Out of scope

- `registerNotify` / `notifyWrite` / `notifySpaceDeleted` — served by `internal/notify`, not `internal/pearserver`.
- `network.habitat.opensocial.*` and `community.opensocial.*`, `network.habitat.relationship.*` — no proposal equivalent.
- `com.atproto.simplespace.getSpace` / `updateSpace` — not implemented.
- Migrating internal callers (`sap`, frontend) to the `com.atproto` paths.
- Defining `com.atproto.*` lexicons, regenerating `api/habitat`, `typescript/api`, or the OpenAPI docs.
- Implementing the proposal's simplespace policies (`readPolicy`/`writePolicy`/`appAccess`) or `putMember`'s `read`/`write` across the two namespaces.
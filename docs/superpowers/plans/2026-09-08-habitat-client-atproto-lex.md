# Plan: Migrate XRPC client to `@atproto/lex`

Spec: `docs/superpowers/specs/2026-09-08-habitat-client-atproto-lex-design.md` (approved).

## Goal

Replace the hand-rolled XRPC layer in `typescript/internal/src/habitatClient.ts`
(`query()`/`procedure()` string-keyed API, `QueryEndpoints`/`ProcedureEndpoints`
tables, `XRPCError`, `castRecord`) with `@atproto/lex` codegen + runtime:
`lex build` for `typescript/api`, `xrpc()`/`xrpcSafe()` for calls, and
`XrpcFailure`/`XrpcResponseError`/`matchesSchemaErrors()` for errors.

## Conventions decided in the spec (do not re-litigate)

- One-shot swap: no dual codegen, no `XRPCError` compat shim.
- Authed calls: `xrpc(agentFor(authManager), …)`; unauthenticated/proxied
  expressed at call sites (plain-domain agent / `Atproto-Proxy` header).
- New types referenced as `network.habitat.<leaf>.<Type>` (e.g.
  `network.habitat.groups.listGroups.$Output`, `network.habitat.groups.defs.GroupView`).
- `import { network } from "api"` at call sites (vite refers to the barrel);
  `import type` for type-only use. Deep imports under the old `api/types/...`
  and old `api` type namespaces (`NetworkX.OutputSchema`) go away.
- No unit tests exist for habitatClient; correctness gate is typecheck + builds
  + dev smoke. (TDD not applicable here.)

## Current-state inventory (on-disk reference)

Complete per-site inventory (36 `query` sites / 37 `procedure` sites, options,
NSIDs, error handling):
`/var/folders/3h/n5m9zy0d0ygcb5r_xnwnpzr00000gn/T/opencode/inventory.md`

`lex build` output shape probe (validates `--defs-export --index-file` output,
`main`, `$nsid`, `$params`, `$output`, errors, and `@atproto/lex` runtime API):
`/var/folders/3h/n5m9zy0d0ygcb5r_xnwnpzr00000gn/T/opencode/lexprobe/`

Key runtime facts verified from the probe:
- `xrpc(agentOrOpts, method.main, opts)`; json outputs auto-decoded in
  `res.body`; `xrpcSafe` returns `Response` (body bytes for binary endpoints).
- `agentFor(authManager)` → `{ did, fetchHandler(path, init) }` adapting
  `AuthManager.fetch` (path+query → relative URL, method/body/headers passthrough).
- Errors: `XrpcResponseError` (`.error`, `.status`, `.payload.message`),
  `XrpcFetchError`, `matchesSchemaErrors(err, ["Code"])`.
- Unauthenticated endpoints (5): `org.create`, `org.loginMember`,
  `org.mintMemberIdentity`, `instance.describeInstance`.
- No-op output handling: call headers via `headers` in opts (not init)

## Files

### New / rewritten
- `typescript/api/src/generated/…` — `lex build` output (generated; gitignored
  nothing, committed).
- `typescript/api/index.ts` — hand-written barrel
  (`export * as com/community/network from "./src/generated/index.js"`).
- `typescript/internal/src/rpc.ts` — replaces `habitatClient.ts`:
  `agentFor`, `getPrivateRecord`, `listPrivateRecords`, `castRecord`,
  `TypedRecord` (same signatures).

### Deleted
- `typescript/api/types/`, `typescript/api/lexicons.ts`, `typescript/api/util.ts`,
  `typescript/api/src/generated` old outputs.
- `typescript/internal/src/habitatClient.ts`.
- `typescript/internal/src/client/` (if any derived files exist — verify).

### Edited (call-site batches; ~20 files, 73 sites)
Frontend queries: `groups.ts`, `collections.ts`, `spaces.ts`, `org.ts`,
`permissions.ts`, `instance.ts`, `opensocial.ts`.
Frontend routes: `org/join.tsx`, `org/create.tsx`, `community/create.tsx`,
`login/habitat.tsx` (if it calls org.*), `_requireAuth/index.tsx`,
`_requireAuth/data.tsx`, `_requireAuth/blob-test/index.tsx`,
`_requireAuth/pear-test/index.tsx`, `_requireAuth/pear-test/view.tsx`,
`_requireAuth/permissions/lexicons/index.tsx`,
`_requireAuth/permissions/lexicons/$collection.tsx`,
`_requireAuth/permissions/people.tsx`, `_requireAuth/permissions/people/$did.tsx`,
`_requireAuth/spaces/index.tsx`,
`_requireAuth/spaces/$spaceOwner.$spaceType.$spaceKey.index.tsx`,
`_requireAuth/spaces/$spaceOwner.$spaceType.$spaceKey.$recordOwner.index.tsx`,
`components/header.tsx` (type-only), `lib/renderSchemas.ts` (type-only).
Internal components: `GroupCombobox.tsx`, `ShareDialogV2.tsx`,
`GranteeAvatars.tsx`.
Docs app: `queries/docs.tsx`, `_requireAuth/index.tsx`,
`_requireAuth/$uri.tsx` (+ any others per inventory).
`typescript/internal/src/index.ts` — update re-exports.

### Config / deps
- `pnpm-workspace.yaml` catalog: add `@atproto/lex` (0.3.8, as spec'd).
- `typescript/api/{package.json,moon.yml}`: dep swap + bake moon `lex build` task.
- `typescript/internal/package.json`, `frontend/package.json`,
  `typescript/apps/docs/package.json`: add `@atproto/lex` dep.

## Tasks

### Phase A — codegen swap (typescript/api)
1. Add `@atproto/lex: 0.3.8` to `pnpm-workspace.yaml` catalog.
2. `typescript/api/package.json`: replace `@atproto/lexicon`,
   `@atproto/xrpc`, `@atproto/lex-cli`, `multiformats` dev/runtime deps with
   `@atproto/lex` (catalog); keep `com/**` consumer deps none.
3. `typescript/api/moon.yml`: generate task →
   `pnpx @atproto/lex lex build --lexicons ../../lexicons --out ./src/generated --index-file --defs-export`, `outputs: [src/generated]`. Add `@atproto/lex` to `deps`/inputs as needed.
4. Rewrite `typescript/api/index.ts` as the hand-written barrel over
   `./src/generated/index.js` (passthrough namespaces).
5. Delete `types/`, `lexicons.ts`, `util.ts`.
6. `pnpm install`; `moon run typescript/api:generate`.
7. **Verify**: `moon run typescript/api:generate` emits
   `src/generated/network/habitat/{org,space,groups,simplespace,permissions,…}/*.defs.ts`;
   inspect `main`/`$nsid`/`$output`/errors on a handful (org.getMetadata,
   space.listSpaces, groups.listGroups, repo.getRecord). Commit.

### Phase B — typescript/internal: rpc.ts
8. Rename `habitatClient.ts` → `rpc.ts`; delete `query`/`procedure`/
   `QueryEndpoints`/`ProcedureEndpoints`/`XRPCError`.
   Implement `agentFor(authManager)` per spec §2 (adapt `AuthManager.fetch`
   incl. relative-URL path+query, method, body, Headers merge).
   Re-implement `getPrivateRecord`/`listPrivateRecords`/`castRecord`/`TypedRecord`
   over `xrpc()`/`xrpcSafe()` (same signatures).
9. Update `typescript/internal/src/index.ts` exports (remove query/procedure/
   XRPCError; keep helpers, re-export type `TypedRecord`, add `agentFor`).
10. Sweep any other internal consumers of the deleted symbols
    (grep `query(`, `procedure(`, `XRPCError`, `castRecord`, `TypedRecord`,
    `habitatClient` across `typescript/` and `frontend/`). Fix `group/…`
    helpers if any.
11. **Verify**: `moon run typescript/build` (or `typescript:typecheck` per moon
    config) passes with frontend/docs temporarily still broken if needed —
    otherwise fold into Phase C gate. Commit when `typescript/internal` alone
    typechecks (use `tsc -p` on internal; frontend cross-package check deferred).

### Phase C — call-site migration
12. Frontend queries batch (7 files): rewrite `query(...)`→`xrpc(agentFor(...), main, { params })`,
    `procedure(...)`→`{ body }`, proxied→`headers`, type imports from `network.habitat.*`.
    - `spaces.ts`: keep `SpaceView`/`Repo`/`SpaceRecord`/`Member` type aliases
      (now via `network.habitat.space.*.defs` / `simplespace.*.defs`); replace
      `error === "RepoNotFound"` (line ~100) with
      `matchesSchemaErrors(err, ["RepoNotFound"])`.
    - `groups.ts`: `homeProxyHeaders()` + `Atproto-Proxy` header.
    - `collections.ts`, `org.ts`, `permissions.ts`, `instance.ts`
      (`describeInstance` unauthenticated), `opensocial.ts`.
13. Frontend routes batch:
    - `org/join.tsx`: raw fetch + `new XRPCError(res.status, data)` →
      `xrpcSafe(domain, network.habitat.org.getMetadata.main, { params: { orgId }, headers: { Authorization: Bearer ${token} } })`; error handling via `XrpcResponseError`.
    - `blob-test/index.tsx`: raw `authManager.fetch` + `new XRPCError(res.status, data)`
      → `xrpcSafe(agentFor(authManager), network.habitat.repo.getBlob.main, …)`
      (headers stay).
    - `_requireAuth/index.tsx`: deep import `App` type updated.
    - `data.tsx`: `listPrivateRecords` migration + type updates.
    - permissions routes (4 files): `Permission` type imports → defs; procedure bodies.
    - spaces routes (3 files): createSpace/putRecord/addMember/removeMember/
      deleteRecord procedure calls.
    - `pear-test/index.tsx`, `pear-test/view.tsx`: per inventory.
    - `community/create.tsx`, `org/create.tsx`, `login/habitat.tsx`: remaining.
14. Internal components batch: `GroupCombobox.tsx` (keep local `homeProxyHeader()`),
    `ShareDialogV2.tsx`, `GranteeAvatars.tsx` (type-only).
15. Docs app batch: `queries/docs.tsx` (`getPrivateRecord`/`listPrivateRecords`/
    `TypedRecord` lines 18/26/98), `_requireAuth/index.tsx`,
    `_requireAuth/$uri.tsx` (`XRPCError` `status === 403` → `XrpcResponseError`
    `.status === 403` via `matchesSchemaErrors`/`isXrpcErrorPayload`).
16. `lib/renderSchemas.ts` (`NetworkHabitatRenderSchema` → defs ref),
    `components/header.tsx` (`NetworkHabitatOrgGetMetadata` type update).
17. **Verify**: `moon run typescript:build`, `moon run frontend:build`,
    `moon run typescript/docs:build`. Fix all type errors (pin defs names from
    emitted files as spec §Risks instructs). Commit.

### Phase D — cleanup & gate
18. Remove stray references to deleted exports anywhere remaining
    (grep `XRPCError`, `api/types`, `AtpBaseClient`, `homeProxyHeader` dup review — keep as-is).
19. `moon run lint-check`, `moon run format`. `moon run ci`.
    Optional dev smoke `moon run dev-all` (ngrok/Caddy local).
20. Final commit; present for review / merge per finishing-a-development-branch.

## Out of scope
`bskyPublicApi.ts`, Go, `apps/chalk`, `xrpc-openapi-gen`, `api-docs`, Go `lexgen`.

## Verification commands
```
moon run typescript/api:generate
moon run typescript:build
moon run frontend:build
moon run typescript/docs:build
moon run lint-check
moon run ci
```

## Risks
- Names in defs files differ from guess (e.g. `.defs.defs.ts`); pin from emitted
  files (Phase A step 7) before mass-migration (Phase C).
- Bundle: barrel namespace import pulls all ~60 method modules; acceptable
  (small schema objects), note only.
- `AuthManager` 401/refresh interplay must stay untouched inside `fetchHandler`
  (did aggregator is `AuthManager.fetch`).
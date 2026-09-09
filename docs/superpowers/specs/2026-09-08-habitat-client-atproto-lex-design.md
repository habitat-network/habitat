# Habitat XRPC client migration to `@atproto/lex`

Date: 2026-09-08

## Goal

Replace the hand-rolled XRPC layer in `typescript/internal/src/habitatClient.ts`
with `@atproto/lex` (the new lexicon toolchain in `packages/lex`), eliminating
the hand-maintained endpoint type tables and hand-rolled XRPC plumbing
(query-string serialization, `jsonToLex` post-processing, error normalization).

Decisions taken during brainstorming:

- Adopt `lex build` as the TypeScript codegen for `typescript/api` (replacing
  `@atproto/lex-cli gen-api`).
- Adopt the idiomatic lex call style at call sites (`xrpc(agent, method.main, opts)`),
  deleting the `query()`/`procedure()` string-keyed API and `XRPCError`.
- No wrapper/tag table: unauthenticated and proxied endpoints are expressed at
  call sites (plain-domain agent / explicit `Atproto-Proxy` header).
- Fully adopt lex error types (`XrpcFailure`/`XrpcResponseError`).

## Current state

`typescript/internal/src/habitatClient.ts` (568 lines) contains:

- Hand-written `QueryEndpoints` / `ProcedureEndpoints` maps (lines 75–387)
  binding each of ~60 NSIDs to generated `Params`/`OutputSchema` types, plus a
  per-endpoint `unauthenticated` flag and prose annotations about which server
  implements each endpoint (pear vs. home vs. docs).
- `query()` / `procedure()` — fetch orchestration over `AuthManager.fetch`
  (DPoP/OAuth/refresh/401 handling) or plain domain fetch for unauthenticated
  calls; query-string building; `jsonToLex` decoding of response bodies.
- `XRPCError` class.
- Helpers `castRecord`, `TypedRecord`, `getPrivateRecord`, `listPrivateRecords`.

`typescript/api` is generated from `/lexicons/**/*.json` via
`@atproto/lex-cli gen-api` (see `typescript/api/moon.yml`), emitting
`types/`, `lexicons.ts`, `util.ts`, and `index.ts` with namespaced classes
(`AtpBaseClient`, `*NS` record clients) and per-method type namespaces
(`NetworkX.OutputSchema / InputSchema / QueryParams / Response`).

Consumers of `api`:

- 10 files import named type namespaces from `"api"` (frontend routes/queries,
  `typescript/internal` components, habitatClient itself).
- 8 files deep-import `api/types/...` (e.g. `SpaceView`, `Permission`, `GroupView`).
- `AtpBaseClient` / `schemas` are unused outside the generated package; `util.ts`
  (`OmitKey`, `Un$Typed`) is only used by generated `index.ts`.

Call sites of `query()`/`procedure()`: ~50 across `frontend/src` and
`typescript/apps/docs`. `typescript/apps/chalk` does not use them (own sapClient).

## Design

### 1. `typescript/api` — regenerate with `lex build`

- Add `@atproto/lex: 0.3.8` to the `catalog` in `pnpm-workspace.yaml`;
  `typescript/api` depends on it. Drop now-unused deps `@atproto/lexicon`,
  `@atproto/xrpc`, `multiformats`.
- Add `@atproto/lex` (catalog) to `typescript/internal` (rpc.ts), `frontend`
  and `typescript/apps/docs` (call sites import `xrpc`/`xrpcSafe`/error types
  directly; pnpm requires declared deps).
- Change the `generate` task in `typescript/api/moon.yml`:

  ```
  pnpx @atproto/lex lex build
    --lexicons ../../lexicons
    --out ./src/generated
    --index-file
    --defs-export
  ```

  with `outputs: [src/generated]`. The existing `lexicons/` JSON remains the
  single source of truth (no `lex install`, no network resolution). The Go
  toolchain (`cmd/lexgen`) and `typescript/xrpc-openapi-gen` read the same JSON
  and are unaffected.

- Generated output: `typescript/api/src/generated/{com,network,community}/…`.
  Each method module (`foo/bar/baz.defs.ts`) exports `$nsid`, `$params`,
  `$Params`, `$input`, `$Input`, `$output`, `$Output`, `main` (the schema
  object), `$lxm`, and named defs. `--index-file` emits `generated/index.ts`
  re-exporting `com`, `network`, `community`.
- `typescript/api/index.ts` becomes a hand-written barrel:
  `export * as com/network/community from "./src/generated/index.js"`.
- Delete obsolete lex-cli outputs: `types/`, `lexicons.ts`, `util.ts`, and the
  `AtpBaseClient`/namespaced-client code.

### 2. `typescript/internal` — replace habitatClient with `rpc.ts`

Rename `habitatClient.ts` → `rpc.ts`:

- `agentFor(authManager: AuthManager): Agent` — returns
  `{ did, fetchHandler }` where `fetchHandler(path: \`/${string}\`, init: RequestInit)`
  adapts `@atproto/lex`'s `FetchHandler` to the existing `AuthManager.fetch`
  (path+query → relative URL, `init.method`, `init.body`, `Headers(init.headers)`).
  `AuthManager` itself is unchanged.
- Preserved helpers, re-implemented over `xrpc()`: `getPrivateRecord`,
  `listPrivateRecords`, `castRecord`, `TypedRecord` (same signatures).
- Deleted: `query`, `procedure`, `XRPCError`, `QueryEndpoints`/`ProcedureEndpoints`.
- `typescript/internal/src/index.ts` re-exports the new surface. Apps import
  `xrpc`, `xrpcSafe`, `XrpcResponseError` directly from `@atproto/lex`.

Behavioral note: `xrpc()` wraps fetch-handler rejections in `XrpcFetchError`
(cause preserved). `AuthManager`'s `UnauthenticatedError` therefore surfaces as
`XrpcFetchError`; no consumer currently catches `UnauthenticatedError`, so no
functional change.

### 3. Call-site migration

Approximately 50 `query()`/`procedure()` call sites in `frontend/src` and
`typescript/apps/docs`.

Conventions:

- Authed query: `const { spaces } = (await xrpc(agentFor(authManager), network.habitat.space.listSpaces.main, { params })).body`
- Authed procedure: `await xrpc(agent, network.habitat.org.addAdmin.main, { body: { admin } })`
  (no-output endpoints ignore `.body`).
- Unauthenticated (org.create, org.loginMember, org.mintMemberIdentity,
  instance.describeInstance): `xrpc(domain, method.main, { params })`.
- Proxied (docs.*, groups.*, collections.*): `headers: { "Atproto-Proxy": "did:web:…" }`
  in the options; existing `homeProxyHeader()` helper is retained where it lives.
- Errors (6 spots): `XrpcResponseError` + `matchesSchemaErrors()` for narrowing
  to declared error codes. `org/join.tsx`'s raw fetch + manual `XRPCError`
  becomes `xrpcSafe(domain, network.habitat.org.getMetadata.main,
  { params: { orgId }, headers: { Authorization: \`Bearer ${token}\` } })`.

Type reference updates:

- `NetworkX.OutputSchema` → `network.x.$Output`
- `NetworkX.InputSchema` → `network.x.$Input`
- Deep imports `api/types/network/habitat/…` → `network.habitat.….DefType`
  (final names taken from the generated `.defs.ts` files).

Explicitly out of scope / untouched: `bskyPublicApi.ts` (@atproto/api Agent),
all Go code, `apps/chalk` (own sapClient), Go `lexgen`,
`xrpc-openapi-gen`, `api-docs`.

## Verification

- `moon run typescript/api:generate` regenerates the TS bindings.
- Typecheck/build: `moon run typescript:build`, `moon run frontend:build`,
  `moon run typescript/docs:build`, `moon run typescript/chalk:build`.
- Lint/format: `moon run format-check`, `moon run lint-check`.
- `moon run ci`.
- `go test ./...` (unaffected by this change).
- No habitatClient unit tests exist today; correctness is established by
  typecheck + build + manual dev smoke (`moon run dev-all`).

## Risks / follow-ups

- `lex build` output shape is new to this repo; the generated barrel/types must
  be verified against all 18 consumer files during implementation (names are
  pinned down per-file from the emitted `.defs.ts`).
- `@atproto/lex@0.3.8` is pre-1.0; catalog pin chosen deliberately.
- The `network.*` / `community.*` / `com.*` namespaces replace the previous
  `AtpBaseClient`/`*NS` clients; anything relying on those must be updated or
  will fail typecheck (audit found none outside the generated package).
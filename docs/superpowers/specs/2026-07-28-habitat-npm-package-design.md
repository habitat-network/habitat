# `@habitat-network/habitat` — Publishable TypeScript SDK

## Motivation

Third-party developers building on Habitat need to resolve AT Protocol identities
through a Habitat instance rather than through the public network. Habitat orgs use
`did:web` identities that Habitat itself hosts, so the stock
`AtprotoIdentityResolver` (DID resolution + handle resolution as separate
primitives) does not know about them.

Pear already exposes `com.atproto.identity.resolveIdentity`, whose handler wraps
indigo's `DefaultDirectory`, so a single call resolves both Habitat-hosted and
public-network identities. This package wraps that endpoint behind the
`IdentityResolver` interface from `@atproto-labs/identity-resolver`, which is a
pass-through constructor option on `@atproto/oauth-client` and
`@atproto/oauth-client-browser`:

```ts
new BrowserOAuthClient({
  clientMetadata,
  handleResolver,
  identityResolver: new HabitatIdentityResolver(),
});
```

The package is scoped generically (not `@habitat-network/identity-resolver`) so
future Habitat client utilities — spaces, permissions, cliques — ship from the same
package.

## Scope

In scope:

- A new publishable workspace package at `typescript/habitat/`.
- `HabitatIdentityResolver`, satisfying `IdentityResolver`.
- Publish-ready packaging: manual `pnpm publish`, no CI release workflow.

Out of scope:

- Migrating `frontend/` or `typescript/internal/` to consume the package. The
  audience is external apps; in-repo code is untouched.
- Changesets, version automation, or a tag-triggered publish workflow.
- Any change to `internal/identity/` or the lexicons. The endpoint exists and is
  used as-is.

## Package layout

```
typescript/habitat/
  package.json
  tsconfig.json
  vitest.config.ts
  moon.yml
  README.md
  LICENSE
  src/
    index.ts                                 # barrel
    identity/
      index.ts
      habitat-identity-resolver.ts
      habitat-identity-resolver.test.ts
      errors.ts
    test/
      msw.ts                                 # shared setupServer + lifecycle hooks
```

`src/identity/` is the first of what will become several sibling domain folders.
`src/index.ts` re-exports each one, so adding `src/spaces/` later is additive.

Registration in existing config:

- `pnpm-workspace.yaml` — add `typescript/habitat` to `packages`.
- `.moon/workspace.yml` — add `typescript/habitat: typescript/habitat` to
  `projects.sources`.
- Root `tsconfig.json` — add `{ "path": "./typescript/habitat" }` to `references`.

### Packaging

ESM-only, matching every `@atproto-labs/*` and `@atproto/oauth-client-*` package:

```jsonc
{
  "name": "@habitat-network/habitat",
  "version": "0.1.0",
  "type": "module",
  "license": "Apache-2.0",
  "sideEffects": false,
  "exports": {
    ".": { "types": "./dist/index.d.ts", "default": "./dist/index.js" }
  },
  "files": ["dist", "README.md", "LICENSE"],
  "publishConfig": { "access": "public" },
  "scripts": {
    "build": "tsc --build",
    "test": "vitest run"
  }
}
```

`LICENSE` is a copy of the repository's root Apache-2.0 license, since npm packages
must carry their own license file.

A single `.` export. Subpath exports (`./identity`) are a non-breaking addition if
tree-shaking pressure ever warrants them.

`tsconfig.json` extends `../../tsconfig.options.json` with `composite: true`,
`declaration: true`, `declarationMap: true`, `outDir: "./dist"`, `noEmit: false`.
Note this differs from `typescript/api`, which sets `emitDeclarationOnly: true`
because it is consumed from source inside the monorepo. This package must emit real
JavaScript.

`moon.yml` overrides the inherited `typescript.yml` `build` task, which otherwise
depends on `typescript/api:build` and `typescript/internal:build`. This package
depends on neither:

```yaml
language: typescript
tasks:
  build:
    deps: []
    options:
      mergeDeps: replace
  test:
    command: pnpm test
```

## Dependencies

**Zero runtime dependencies.**

`@atproto-labs/identity-resolver` is a peer dependency (`>=0.4.0 <1`) and a
devDependency, imported with `import type` only.

The peer is *required*, not optional. Anyone using this package is already on
`@atproto/oauth-client` or `@atproto/oauth-client-browser`, both of which depend on
`@atproto-labs/identity-resolver` transitively. Marking it optional would break
typechecking for any consumer who did not install it, because our emitted `.d.ts`
references its types. Keeping it a peer rather than a regular dependency guarantees
our types resolve against the same copy the consumer's OAuth client uses, avoiding
structurally-identical-but-nominally-distinct duplicate installs.

`HANDLE_INVALID` (`"handle.invalid"`) is inlined as a module-local constant rather
than imported, which is what keeps the runtime dependency count at zero. The value
is fixed by the AT Protocol spec.

devDependencies: `@atproto-labs/identity-resolver`, `msw`, `typescript`, `vitest`.
`msw` 2.15.0 is already in `pnpm-lock.yaml`; `typescript` and `vitest` come from the
workspace catalog.

## Public API

```ts
// src/identity/habitat-identity-resolver.ts

export const DEFAULT_HABITAT_SERVICE_URL = "https://pear.habitat.network";

export class HabitatIdentityResolver implements IdentityResolver {
  constructor(serviceUrl?: string | URL);
  resolve(
    identifier: string,
    options?: ResolveIdentityOptions,
  ): Promise<IdentityInfo>;
}
```

`serviceUrl` defaults to `DEFAULT_HABITAT_SERVICE_URL` and exists so developers on a
self-hosted Habitat instance, or on `pear.local.habitat.network` in local dev, can
point elsewhere. The constructor normalizes it to a `URL` once and stores it.

There is deliberately no exported pre-configured singleton instance and no `fetch`
injection option. Both are non-breaking additions later if a need appears; MSW makes
the `fetch` seam unnecessary for testing.

Type re-exports so consumers need no direct `@atproto-labs/*` import:

```ts
export type {
  AtprotoDid,
  AtprotoDidDocument,
  IdentityInfo,
  IdentityResolver,
  ResolveIdentityOptions,
} from "@atproto-labs/identity-resolver";
```

### Errors

```ts
// src/identity/errors.ts

export class HabitatIdentityResolverError extends Error {
  readonly name = "HabitatIdentityResolverError";
  readonly status?: number;
  readonly xrpcError?: string; // "DidNotFound", "HandleNotFound", "InvalidRequest"
  constructor(
    message: string,
    options?: { cause?: unknown; status?: number; xrpcError?: string },
  );
}
```

## Resolution flow

`resolve(identifier, options)`:

1. Build `new URL("/xrpc/com.atproto.identity.resolveIdentity", serviceUrl)` and set
   the `identifier` search param. Using `URLSearchParams` handles encoding, which
   matters because handles and `did:web:` identifiers contain characters (`:`, `.`)
   that must survive intact.
2. `fetch(url, { method: "GET", headers: { accept: "application/json" }, signal:
   options?.signal, cache: options?.noCache ? "no-store" : undefined })`.
3. On non-2xx: attempt to parse the XRPC error envelope `{ error, message }`. Throw
   `HabitatIdentityResolverError` with `status` and `xrpcError` populated. A body
   that fails to parse still yields an error carrying `status`.
4. Parse the JSON body and validate:
   - `did` is a string matching `/^did:(plc|web):/`, which is what the `AtprotoDid`
     type admits.
   - `didDoc` is a non-null object whose `id` strictly equals `did`.
   - `handle` is a string.

   Any failure throws `HabitatIdentityResolverError`. This validation is what makes
   the `as AtprotoDid` / `as AtprotoDidDocument` assertions at the return honest
   rather than blind.
5. Normalize `handle` to lowercase. If it is not a syntactically valid AT Protocol
   handle, substitute `"handle.invalid"`. Validity is the standard handle grammar —
   at least two dot-separated segments, each 1–63 characters of alphanumerics and
   hyphens, not starting or ending with a hyphen, with the final segment not
   starting with a digit:

   ```
   /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*\.[a-z]([a-z0-9-]{0,61}[a-z0-9])?$/
   ```

   Applied after lowercasing, and total length capped at 253. Note `handle.invalid`
   itself satisfies this grammar, so the passthrough case needs no special-casing.
6. Return `{ did, didDoc, handle }`.

### Abort handling

`AbortError` (and any `DOMException` with `name === "AbortError"`) propagates
unwrapped. Wrapping it in `HabitatIdentityResolverError` would break every consumer
that distinguishes cancellation from failure, including `@atproto/oauth-client`.

Other `fetch` rejections (DNS failure, TLS failure, offline) are wrapped in
`HabitatIdentityResolverError` with the original error as `cause`.

### Response shape

Pear's handler writes `atproto.IdentityDefs_IdentityInfo`:

```go
httpx.WriteJSON(ctx, w, atproto.IdentityDefs_IdentityInfo{
    Did:    ident.DID.String(),
    Handle: ident.Handle.String(),
    DidDoc: s.didDocumentWithContext(ident),
})
```

That is `{ did, handle, didDoc }` — field-for-field identical to `IdentityInfo`. The
adapter is a thin translation layer, not a remapping. Indigo already emits
`handle.invalid` for unverifiable handles, so step 5 is defensive rather than
load-bearing.

### Trust model

Unlike `AtprotoIdentityResolver`, this resolver performs no client-side
bidirectional handle↔DID verification. It delegates that to the Habitat instance:
`internal/identity/server.go` resolves through `NewWrappedDirectory(hive,
identity.DefaultDirectory())`, and indigo's directory verifies bidirectionally and
falls back to public-network resolution for identities Habitat does not host.

Pointing `serviceUrl` at an untrusted host therefore means trusting that host's
identity claims outright. The README states this explicitly.

## Testing

Vitest in a `node` environment, with MSW (`msw/node` `setupServer`) intercepting at
the network layer.

`src/test/msw.ts` exports the shared server and installs lifecycle hooks:

```ts
server.listen({ onUnhandledRequest: "error" });
```

`onUnhandledRequest: "error"` earns its keep here: any request to a host or path the
test did not explicitly register fails the test, which means the default-service-URL
and custom-service-URL cases are verified structurally rather than by asserting on a
captured string.

Cases:

| Case | Assertion |
| --- | --- |
| Resolves a `did:web` Habitat identity | returns `{ did, didDoc, handle }` from the body |
| Resolves a handle input | same, handler asserts the `identifier` param |
| Default service URL | handler registered on `https://pear.habitat.network` is hit |
| Custom `serviceUrl` | handler on the custom origin is hit; default origin is not registered, so a regression fails via `onUnhandledRequest` |
| `serviceUrl` with a trailing path | resolves against origin correctly |
| Identifier encoding | handler reads back the exact `did:web:` / handle string |
| 404 `DidNotFound` | throws `HabitatIdentityResolverError` with `status: 404`, `xrpcError: "DidNotFound"` |
| 400 `InvalidRequest` | same shape, `status: 400` |
| Non-JSON error body | still throws with `status` set |
| Malformed success body (missing `did`) | throws `HabitatIdentityResolverError` |
| `didDoc.id !== did` | throws `HabitatIdentityResolverError` |
| Aborted signal | rejects with `AbortError`, not `HabitatIdentityResolverError` |
| Network failure (`HttpResponse.error()`) | throws `HabitatIdentityResolverError` with `cause` set |
| `handle: "handle.invalid"` | passes through unchanged |
| `handle: "NotAHandle"` (invalid syntax) | coerced to `"handle.invalid"` |
| `handle: "Alice.Example.Com"` | lowercased to `"alice.example.com"` |
| Type-level conformance | `HabitatIdentityResolver satisfies IdentityResolver` compiles; drift in the upstream interface fails the build |

One known uncertainty: the `noCache` → `cache: "no-store"` assertion depends on
whether undici surfaces `request.cache` to an MSW handler. If it does not, drop that
single assertion rather than adding a production-code seam to make it observable —
the behavior is a one-line pass-through to `fetch`, and the other sixteen cases carry
the suite.

## Verification

- `moon typescript/habitat:test` passes.
- `moon typescript/habitat:build` emits `dist/index.js` and `dist/index.d.ts`.
- `npm pack --dry-run` in the package shows only `dist/`, `README.md`, `LICENSE`.
- A scratch consumer outside the monorepo imports the built package and typechecks
  `new BrowserOAuthClient({ identityResolver: new HabitatIdentityResolver() })`,
  confirming the peer-dependency type resolution actually works end to end.
- `moon :lint-check` and `moon :format-check` pass.

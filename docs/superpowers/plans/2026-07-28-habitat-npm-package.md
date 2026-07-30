# `@habitat-network/habitat` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a publishable, zero-runtime-dependency TypeScript package exporting `HabitatIdentityResolver`, which satisfies `IdentityResolver` from `@atproto-labs/identity-resolver` by calling `com.atproto.identity.resolveIdentity` on a Habitat instance.

**Architecture:** A new pnpm workspace package at `typescript/habitat/`, built with `tsc` to ESM-only `dist/`. `HabitatIdentityResolver` is a thin adapter: it issues one `GET` to the XRPC endpoint, validates the response, and returns `{ did, didDoc, handle }`. Pear's handler already returns exactly that shape, so there is no field remapping. `@atproto-labs/identity-resolver` is a *type-only peer dependency* — imported with `import type` and never at runtime.

**Tech Stack:** TypeScript 5.9 (`module: NodeNext`), Vitest 3, MSW 2 (`msw/node`), Moon, pnpm workspaces.

**Spec:** `docs/superpowers/specs/2026-07-28-habitat-npm-package-design.md`

## Global Constraints

- **Zero runtime dependencies.** `package.json` has no `dependencies` key at all. Every import of `@atproto-labs/identity-resolver` must be `import type`. If you find yourself needing a runtime value from it, inline the value instead.
- **ESM only.** No CJS build, no dual output. `"type": "module"`.
- Package name: `@habitat-network/habitat`. Version `0.1.0`. License `Apache-2.0`.
- Default service URL: `https://pear.habitat.network` (no trailing slash).
- XRPC path: `/xrpc/com.atproto.identity.resolveIdentity`, query param `identifier`.
- All relative imports use explicit `.js` extensions (required by `moduleResolution: nodenext`), even though the source files are `.ts`.
- `AbortError` must propagate unwrapped. Never wrap it in `HabitatIdentityResolverError`.
- Node 20+ (`fetch`, `AbortSignal`, `Error` `cause` are all assumed built in).
- Commit after every task. Do not `git push` and do not `pnpm publish` — publishing is manual and out of scope.

## Deviations from the spec (decided, not open questions)

1. **Spec's tsconfig said `declarationMap: true`, but its `files` list and `npm pack` verification allow only `dist`, `README.md`, `LICENSE`.** Those contradict — declaration maps point at `src/`, which is not published. Resolution: `declarationMap: false` and `sourceMap: false`. The published artifact stays minimal and the pack verification stays true.
2. **The spec's file list omits `src/identity/handle.ts`.** Handle normalization is a pure function with its own regex and deserves its own test cycle, so it gets its own module. It stays internal — not re-exported from the barrel.
3. **No separate type-level conformance test file.** The `implements IdentityResolver` clause on the class is itself the compile-time check, and unlike a test file it *is* included in `tsc --build`. Upstream interface drift fails the build. Task 6 still adds a runtime assignment test for documentation value.

## File Structure

| File | Responsibility |
| --- | --- |
| `typescript/habitat/package.json` | Package manifest; publish config, peer dep, scripts |
| `typescript/habitat/tsconfig.json` | Emits real JS + `.d.ts` to `dist/`; excludes tests |
| `typescript/habitat/vitest.config.ts` | Node environment; registers the MSW setup file |
| `typescript/habitat/moon.yml` | Moon `build`/`test` tasks; drops inherited monorepo build deps |
| `typescript/habitat/LICENSE` | Copy of the root Apache-2.0 license |
| `typescript/habitat/README.md` | Install, usage, self-hosting, trust model |
| `typescript/habitat/src/index.ts` | Top-level barrel; re-exports each domain folder |
| `typescript/habitat/src/identity/index.ts` | Identity domain barrel + type re-exports |
| `typescript/habitat/src/identity/errors.ts` | `HabitatIdentityResolverError` |
| `typescript/habitat/src/identity/handle.ts` | `HANDLE_INVALID`, `normalizeHandle` (internal) |
| `typescript/habitat/src/identity/habitat-identity-resolver.ts` | The resolver + response validation |
| `typescript/habitat/src/test/msw.ts` | Shared `setupServer` + lifecycle hooks |
| `pnpm-workspace.yaml` | Register the package |
| `.moon/workspace.yml` | Register the Moon project |
| `tsconfig.json` (root) | Add project reference |

---

### Task 1: Package scaffolding and workspace registration

Nothing here is testable in isolation, so the deliverable is: `pnpm install` succeeds, `pnpm build` emits `dist/index.js` and `dist/index.d.ts`, and `pnpm test` runs green with zero tests.

**Files:**
- Create: `typescript/habitat/package.json`
- Create: `typescript/habitat/tsconfig.json`
- Create: `typescript/habitat/vitest.config.ts`
- Create: `typescript/habitat/moon.yml`
- Create: `typescript/habitat/LICENSE`
- Create: `typescript/habitat/src/index.ts`
- Create: `typescript/habitat/src/test/msw.ts`
- Modify: `pnpm-workspace.yaml`
- Modify: `.moon/workspace.yml`
- Modify: `tsconfig.json` (root)

**Interfaces:**
- Consumes: nothing.
- Produces: the `server` export from `src/test/msw.ts` (an MSW `SetupServerApi`), used by every later test task.

- [ ] **Step 1: Create `typescript/habitat/package.json`**

Note there is deliberately no `dependencies` key. `typescript` and `vitest` come from the workspace catalog; `msw` and `@atproto-labs/identity-resolver` are pinned directly because they are not catalog entries.

```json
{
  "name": "@habitat-network/habitat",
  "version": "0.1.0",
  "description": "TypeScript SDK for building on Habitat, a data ownership layer built on AT Protocol.",
  "license": "Apache-2.0",
  "type": "module",
  "sideEffects": false,
  "repository": {
    "type": "git",
    "url": "git+https://github.com/habitat-network/habitat.git",
    "directory": "typescript/habitat"
  },
  "keywords": ["habitat", "atproto", "at-protocol", "identity", "oauth"],
  "exports": {
    ".": {
      "types": "./dist/index.d.ts",
      "default": "./dist/index.js"
    }
  },
  "files": ["dist", "README.md", "LICENSE"],
  "publishConfig": {
    "access": "public"
  },
  "scripts": {
    "build": "tsc --build",
    "test": "vitest run"
  },
  "peerDependencies": {
    "@atproto-labs/identity-resolver": ">=0.4.0 <1"
  },
  "devDependencies": {
    "@atproto-labs/identity-resolver": "^0.4.5",
    "msw": "^2.15.0",
    "typescript": "catalog:",
    "vitest": "catalog:"
  }
}
```

- [ ] **Step 2: Create `typescript/habitat/tsconfig.json`**

Three of these overrides are load-bearing and easy to get wrong:
- `allowImportingTsExtensions` is `true` in the inherited `tsconfig.options.json`, and TypeScript **refuses to emit** when it is on. It must be turned off.
- `moduleResolution` is `bundler` in the inherited config. A published package must be resolvable by Node, so this switches to `nodenext` — which is why every relative import needs an explicit `.js` extension.
- The `exclude` list keeps tests out of `dist/`. Without it, `dist/` would ship test files and the build would require MSW types.

```json
{
  "extends": "../../tsconfig.options.json",
  "compilerOptions": {
    "composite": true,
    "declaration": true,
    "declarationMap": false,
    "sourceMap": false,
    "noEmit": false,
    "emitDeclarationOnly": false,
    "allowImportingTsExtensions": false,
    "module": "NodeNext",
    "moduleResolution": "nodenext",
    "verbatimModuleSyntax": true,
    "noEmitOnError": true,
    "outDir": "./dist",
    "rootDir": "./src"
  },
  "include": ["src/**/*"],
  "exclude": ["node_modules", "dist", "src/**/*.test.ts", "src/test/**"],
  "references": []
}
```

- [ ] **Step 3: Create `typescript/habitat/vitest.config.ts`**

```typescript
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
    setupFiles: ["./src/test/msw.ts"],
  },
});
```

- [ ] **Step 4: Create `typescript/habitat/src/test/msw.ts`**

`onUnhandledRequest: "error"` is the point of this file. Any request to a host or path a test did not explicitly register fails that test, which is how the default-service-URL and custom-service-URL cases get verified structurally instead of by string comparison.

```typescript
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll } from "vitest";

export const server = setupServer();

beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
});

afterEach(() => {
  server.resetHandlers();
});

afterAll(() => {
  server.close();
});
```

- [ ] **Step 5: Create a placeholder `typescript/habitat/src/index.ts`**

Task 6 replaces this. It exists now only so `tsc --build` has an entry point to emit.

```typescript
export {};
```

- [ ] **Step 6: Copy the root license into the package**

npm packages must carry their own license file.

```bash
cp LICENSE typescript/habitat/LICENSE
```

- [ ] **Step 7: Create `typescript/habitat/moon.yml`**

The inherited `build` task in `.moon/tasks/typescript.yml` depends on `typescript/api:build` and `typescript/internal:build`. This package depends on neither, so `mergeDeps: replace` drops them (the same pattern `typescript/api/moon.yml` uses). The inherited `start` and `dev` tasks reference `pnpm serve` / `pnpm dev` scripts that do not exist here, so they are excluded.

```yaml
language: typescript

workspace:
  inheritedTasks:
    exclude:
      - start
      - dev

tasks:
  build:
    deps: []
    options:
      mergeDeps: replace
  test:
    command: pnpm test
    inputs:
      - src/**/*
      - package.json
      - tsconfig.json
      - vitest.config.ts
```

- [ ] **Step 8: Register the package in `pnpm-workspace.yaml`**

Add `typescript/habitat` to the `packages` list, after the `typescript/api` entry:

```yaml
packages:
  - frontend
  - camera
  - typescript/internal
  - typescript/apps/*
  - typescript/api
  - typescript/habitat
  - typescript/xrpc-openapi-gen
  - api-docs
  - website
```

- [ ] **Step 9: Register the Moon project in `.moon/workspace.yml`**

Add to `projects.sources`, after the `typescript/api` line:

```yaml
    typescript/habitat: typescript/habitat
```

- [ ] **Step 10: Add the project reference to the root `tsconfig.json`**

The existing array is ordered `./typescript/api`, then the `./typescript/apps/*`
entries, then `./typescript/internal`. Insert this entry directly after the
`./typescript/api` object to keep that ordering:

```json
    {
      "path": "./typescript/habitat"
    },
```

- [ ] **Step 11: Install and verify the build**

```bash
pnpm install
cd typescript/habitat && pnpm build
```

Expected: exits 0, and `dist/index.js` + `dist/index.d.ts` exist. Verify:

```bash
ls typescript/habitat/dist
```

- [ ] **Step 12: Verify the test runner starts**

```bash
cd typescript/habitat && pnpm test --passWithNoTests
```

Expected: PASS with "No test files found" (the flag is needed only until Task 2 adds the first test; do not add it to the `test` script).

- [ ] **Step 13: Commit**

```bash
git add typescript/habitat pnpm-workspace.yaml .moon/workspace.yml tsconfig.json pnpm-lock.yaml
git commit -m "feat(habitat-sdk): scaffold @habitat-network/habitat package"
```

---

### Task 2: `HabitatIdentityResolverError`

**Files:**
- Create: `typescript/habitat/src/identity/errors.ts`
- Test: `typescript/habitat/src/identity/errors.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `class HabitatIdentityResolverError extends Error` with constructor `(message: string, options?: HabitatIdentityResolverErrorOptions)` and readonly properties `status?: number`, `xrpcError?: string`. Sets `this.name = "HabitatIdentityResolverError"`.
  - `type HabitatIdentityResolverErrorOptions = { cause?: unknown; status?: number; xrpcError?: string }`

- [ ] **Step 1: Write the failing test**

Create `typescript/habitat/src/identity/errors.test.ts`:

```typescript
import { describe, expect, it } from "vitest";

import { HabitatIdentityResolverError } from "./errors.js";

describe("HabitatIdentityResolverError", () => {
  it("is an Error with a distinguishable name", () => {
    const error = new HabitatIdentityResolverError("boom");

    expect(error).toBeInstanceOf(Error);
    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
    expect(error.name).toBe("HabitatIdentityResolverError");
    expect(error.message).toBe("boom");
  });

  it("defaults status and xrpcError to undefined", () => {
    const error = new HabitatIdentityResolverError("boom");

    expect(error.status).toBeUndefined();
    expect(error.xrpcError).toBeUndefined();
  });

  it("carries status and xrpcError when provided", () => {
    const error = new HabitatIdentityResolverError("not found", {
      status: 404,
      xrpcError: "DidNotFound",
    });

    expect(error.status).toBe(404);
    expect(error.xrpcError).toBe("DidNotFound");
  });

  it("preserves the underlying cause", () => {
    const cause = new TypeError("fetch failed");
    const error = new HabitatIdentityResolverError("unreachable", { cause });

    expect(error.cause).toBe(cause);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd typescript/habitat && pnpm test
```

Expected: FAIL — cannot resolve `./errors.js`.

- [ ] **Step 3: Write the implementation**

Create `typescript/habitat/src/identity/errors.ts`:

```typescript
export type HabitatIdentityResolverErrorOptions = {
  cause?: unknown;
  /** HTTP status returned by the Habitat instance, when there was a response. */
  status?: number;
  /** The XRPC `error` field, e.g. "DidNotFound" or "HandleNotFound". */
  xrpcError?: string;
};

/**
 * Raised for every failure originating from this package: unreachable host,
 * non-2xx XRPC response, or a malformed identity payload.
 *
 * Abort errors are deliberately *not* wrapped in this type, so callers can keep
 * distinguishing cancellation from failure.
 */
export class HabitatIdentityResolverError extends Error {
  readonly status?: number;
  readonly xrpcError?: string;

  constructor(
    message: string,
    options: HabitatIdentityResolverErrorOptions = {},
  ) {
    super(message, { cause: options.cause });
    this.name = "HabitatIdentityResolverError";
    this.status = options.status;
    this.xrpcError = options.xrpcError;
  }
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd typescript/habitat && pnpm test
```

Expected: PASS, 4 tests.

- [ ] **Step 5: Commit**

```bash
git add typescript/habitat/src/identity/errors.ts typescript/habitat/src/identity/errors.test.ts
git commit -m "feat(habitat-sdk): add HabitatIdentityResolverError"
```

---

### Task 3: Handle normalization

**Files:**
- Create: `typescript/habitat/src/identity/handle.ts`
- Test: `typescript/habitat/src/identity/handle.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `const HANDLE_INVALID = "handle.invalid"`
  - `function normalizeHandle(handle: string): string` — lowercases, returns `HANDLE_INVALID` for anything that is not a syntactically valid AT Protocol handle.

- [ ] **Step 1: Write the failing test**

Create `typescript/habitat/src/identity/handle.test.ts`:

```typescript
import { describe, expect, it } from "vitest";

import { HANDLE_INVALID, normalizeHandle } from "./handle.js";

describe("normalizeHandle", () => {
  it("lowercases a valid handle", () => {
    expect(normalizeHandle("Alice.Example.Com")).toBe("alice.example.com");
  });

  it("passes a Habitat handle through unchanged", () => {
    expect(normalizeHandle("acme.habitat.network")).toBe(
      "acme.habitat.network",
    );
  });

  it("passes handle.invalid through unchanged", () => {
    expect(normalizeHandle(HANDLE_INVALID)).toBe(HANDLE_INVALID);
  });

  it("rejects a single-segment handle", () => {
    expect(normalizeHandle("NotAHandle")).toBe(HANDLE_INVALID);
  });

  it("rejects an empty handle", () => {
    expect(normalizeHandle("")).toBe(HANDLE_INVALID);
  });

  it("rejects a segment starting with a hyphen", () => {
    expect(normalizeHandle("-bad.example.com")).toBe(HANDLE_INVALID);
  });

  it("rejects a segment ending with a hyphen", () => {
    expect(normalizeHandle("bad-.example.com")).toBe(HANDLE_INVALID);
  });

  it("rejects a numeric top-level segment", () => {
    expect(normalizeHandle("alice.example.123")).toBe(HANDLE_INVALID);
  });

  it("rejects characters outside the handle grammar", () => {
    expect(normalizeHandle("did:web:acme.habitat.network")).toBe(
      HANDLE_INVALID,
    );
    expect(normalizeHandle("alice_smith.example.com")).toBe(HANDLE_INVALID);
  });

  it("rejects a handle longer than 253 characters", () => {
    const tooLong = `${"a".repeat(60)}.${"b".repeat(60)}.${"c".repeat(60)}.${"d".repeat(60)}.example.com`;

    expect(tooLong.length).toBeGreaterThan(253);
    expect(normalizeHandle(tooLong)).toBe(HANDLE_INVALID);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd typescript/habitat && pnpm test handle
```

Expected: FAIL — cannot resolve `./handle.js`.

- [ ] **Step 3: Write the implementation**

Create `typescript/habitat/src/identity/handle.ts`:

```typescript
/**
 * Inlined rather than imported from `@atproto-labs/identity-resolver`, which is
 * a type-only peer dependency. The value is fixed by the AT Protocol spec.
 */
export const HANDLE_INVALID = "handle.invalid";

const MAX_HANDLE_LENGTH = 253;

/**
 * The AT Protocol handle grammar: two or more dot-separated segments of
 * alphanumerics and hyphens, each 1-63 characters and neither starting nor
 * ending with a hyphen, with a final segment that does not start with a digit.
 *
 * Note that `handle.invalid` itself satisfies this grammar, so the passthrough
 * case needs no special-casing.
 */
const HANDLE_REGEX =
  /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*\.[a-z]([a-z0-9-]{0,61}[a-z0-9])?$/;

/**
 * Lowercases `handle`, substituting `handle.invalid` for anything that is not a
 * syntactically valid handle.
 *
 * Habitat already returns `handle.invalid` for handles it could not verify, so
 * this is defensive rather than load-bearing.
 */
export function normalizeHandle(handle: string): string {
  const lowered = handle.toLowerCase();

  if (lowered.length > MAX_HANDLE_LENGTH) {
    return HANDLE_INVALID;
  }

  return HANDLE_REGEX.test(lowered) ? lowered : HANDLE_INVALID;
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd typescript/habitat && pnpm test
```

Expected: PASS, 14 tests total (4 from Task 2, 10 here).

- [ ] **Step 5: Commit**

```bash
git add typescript/habitat/src/identity/handle.ts typescript/habitat/src/identity/handle.test.ts
git commit -m "feat(habitat-sdk): add AT Protocol handle normalization"
```

---

### Task 4: `HabitatIdentityResolver` — successful resolution

**Files:**
- Create: `typescript/habitat/src/identity/habitat-identity-resolver.ts`
- Test: `typescript/habitat/src/identity/habitat-identity-resolver.test.ts`

**Interfaces:**
- Consumes: `HabitatIdentityResolverError` from `./errors.js`; `normalizeHandle` from `./handle.js`; `server` from `../test/msw.js`.
- Produces:
  - `const DEFAULT_HABITAT_SERVICE_URL = "https://pear.habitat.network"`
  - `class HabitatIdentityResolver implements IdentityResolver` with constructor `(serviceUrl?: string | URL)` and method `resolve(identifier: string, options?: ResolveIdentityOptions): Promise<IdentityInfo>`

- [ ] **Step 1: Write the failing test**

Create `typescript/habitat/src/identity/habitat-identity-resolver.test.ts`:

```typescript
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "../test/msw.js";
import {
  DEFAULT_HABITAT_SERVICE_URL,
  HabitatIdentityResolver,
} from "./habitat-identity-resolver.js";

const XRPC_PATH = "/xrpc/com.atproto.identity.resolveIdentity";
const DID = "did:web:acme.habitat.network";
const HANDLE = "acme.habitat.network";

const DID_DOC = {
  "@context": ["https://www.w3.org/ns/did/v1"],
  id: DID,
  alsoKnownAs: [`at://${HANDLE}`],
  verificationMethod: [],
  service: [
    {
      id: "#atproto_pds",
      type: "AtprotoPersonalDataServer",
      serviceEndpoint: "https://pds.example.com",
    },
  ],
};

/**
 * Registers a success handler on `origin` and returns a getter for the
 * `identifier` query param the resolver actually sent.
 */
function stubResolveIdentity(
  origin: string,
  body: unknown = { did: DID, handle: HANDLE, didDoc: DID_DOC },
): () => string | null {
  let received: string | null = null;

  server.use(
    http.get(`${origin}${XRPC_PATH}`, ({ request }) => {
      received = new URL(request.url).searchParams.get("identifier");
      return HttpResponse.json(body);
    }),
  );

  return () => received;
}

describe("HabitatIdentityResolver", () => {
  it("resolves a did:web Habitat identity", async () => {
    const identifier = stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL);

    const info = await new HabitatIdentityResolver().resolve(DID);

    expect(identifier()).toBe(DID);
    expect(info).toEqual({ did: DID, handle: HANDLE, didDoc: DID_DOC });
  });

  it("resolves a handle", async () => {
    const identifier = stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL);

    const info = await new HabitatIdentityResolver().resolve(HANDLE);

    expect(identifier()).toBe(HANDLE);
    expect(info.did).toBe(DID);
  });

  it("calls a custom service URL and never the default", async () => {
    // The default origin is intentionally left unregistered: MSW's
    // onUnhandledRequest: "error" turns a regression here into a failure.
    const identifier = stubResolveIdentity("https://pear.example.internal");

    const info = await new HabitatIdentityResolver(
      "https://pear.example.internal",
    ).resolve(HANDLE);

    expect(identifier()).toBe(HANDLE);
    expect(info.did).toBe(DID);
  });

  it("accepts a URL instance", async () => {
    stubResolveIdentity("https://pear.example.internal");

    const info = await new HabitatIdentityResolver(
      new URL("https://pear.example.internal"),
    ).resolve(HANDLE);

    expect(info.did).toBe(DID);
  });

  it("resolves the XRPC path against the origin, discarding any base path", async () => {
    stubResolveIdentity("https://pear.example.internal");

    const info = await new HabitatIdentityResolver(
      "https://pear.example.internal/some/base/path",
    ).resolve(HANDLE);

    expect(info.did).toBe(DID);
  });

  it("round-trips identifiers needing URL encoding", async () => {
    const identifier = stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL);

    await new HabitatIdentityResolver().resolve(
      "did:web:acme.habitat.network:sub:path",
    );

    expect(identifier()).toBe("did:web:acme.habitat.network:sub:path");
  });

  it("normalizes the returned handle", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: DID,
      handle: "ACME.Habitat.Network",
      didDoc: DID_DOC,
    });

    const info = await new HabitatIdentityResolver().resolve(DID);

    expect(info.handle).toBe("acme.habitat.network");
  });

  it("substitutes handle.invalid for a syntactically invalid handle", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: DID,
      handle: "not a handle",
      didDoc: DID_DOC,
    });

    const info = await new HabitatIdentityResolver().resolve(DID);

    expect(info.handle).toBe("handle.invalid");
  });

  it("passes handle.invalid through unchanged", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: DID,
      handle: "handle.invalid",
      didDoc: DID_DOC,
    });

    const info = await new HabitatIdentityResolver().resolve(DID);

    expect(info.handle).toBe("handle.invalid");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd typescript/habitat && pnpm test habitat-identity-resolver
```

Expected: FAIL — cannot resolve `./habitat-identity-resolver.js`.

- [ ] **Step 3: Write the implementation**

Create `typescript/habitat/src/identity/habitat-identity-resolver.ts`. Error paths are stubbed minimally here; Task 5 fills them in against tests.

```typescript
import type {
  AtprotoDid,
  AtprotoDidDocument,
  IdentityInfo,
  IdentityResolver,
  ResolveIdentityOptions,
} from "@atproto-labs/identity-resolver";

import { HabitatIdentityResolverError } from "./errors.js";
import { normalizeHandle } from "./handle.js";

/** The Habitat instance operated by the Habitat project. */
export const DEFAULT_HABITAT_SERVICE_URL = "https://pear.habitat.network";

const RESOLVE_IDENTITY_PATH = "/xrpc/com.atproto.identity.resolveIdentity";

/** The DID methods the `AtprotoDid` type admits. */
const ATPROTO_DID_REGEX = /^did:(plc|web):/;

/**
 * Resolves AT Protocol identities through a Habitat instance's
 * `com.atproto.identity.resolveIdentity` endpoint.
 *
 * Unlike `AtprotoIdentityResolver`, this performs no client-side bidirectional
 * handle/DID verification — it delegates that to the Habitat instance, which
 * resolves through indigo's directory and falls back to the public network for
 * identities it does not host. Pointing `serviceUrl` at a host you do not trust
 * means trusting that host's identity claims.
 */
export class HabitatIdentityResolver implements IdentityResolver {
  readonly #serviceUrl: URL;

  constructor(serviceUrl: string | URL = DEFAULT_HABITAT_SERVICE_URL) {
    this.#serviceUrl = new URL(serviceUrl);
  }

  async resolve(
    identifier: string,
    options?: ResolveIdentityOptions,
  ): Promise<IdentityInfo> {
    const url = new URL(RESOLVE_IDENTITY_PATH, this.#serviceUrl);
    url.searchParams.set("identifier", identifier);

    const response = await fetch(url, {
      method: "GET",
      headers: { accept: "application/json" },
      signal: options?.signal,
      ...(options?.noCache ? { cache: "no-store" as const } : {}),
    });

    return toIdentityInfo(await response.json());
  }
}

function toIdentityInfo(body: unknown): IdentityInfo {
  const { did, didDoc, handle } = body as Record<string, unknown>;

  if (typeof did !== "string" || !ATPROTO_DID_REGEX.test(did)) {
    throw new HabitatIdentityResolverError(
      `Habitat instance returned an invalid DID: ${JSON.stringify(did)}`,
    );
  }

  return {
    did: did as AtprotoDid,
    didDoc: didDoc as AtprotoDidDocument,
    handle: normalizeHandle(handle as string),
  };
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd typescript/habitat && pnpm test
```

Expected: PASS, 23 tests total.

If `didDoc as AtprotoDidDocument` produces a "Conversion of type ... may be a mistake" error, widen it to `didDoc as unknown as AtprotoDidDocument` — Task 5 adds the runtime validation that makes the assertion sound.

- [ ] **Step 5: Verify the build still emits**

```bash
cd typescript/habitat && pnpm build
```

Expected: exits 0. This confirms the `implements IdentityResolver` clause typechecks against the real upstream interface, and that test files stayed out of `dist/`:

```bash
ls typescript/habitat/dist/identity
```

Expected: `errors.js`, `handle.js`, `habitat-identity-resolver.js` and their `.d.ts` files — no `*.test.js`.

- [ ] **Step 6: Commit**

```bash
git add typescript/habitat/src/identity/habitat-identity-resolver.ts typescript/habitat/src/identity/habitat-identity-resolver.test.ts
git commit -m "feat(habitat-sdk): add HabitatIdentityResolver happy path"
```

---

### Task 5: `HabitatIdentityResolver` — error paths

**Files:**
- Modify: `typescript/habitat/src/identity/habitat-identity-resolver.ts`
- Modify: `typescript/habitat/src/identity/habitat-identity-resolver.test.ts`

**Interfaces:**
- Consumes: everything from Task 4.
- Produces: no new exports. `resolve()` gains its full failure behavior.

- [ ] **Step 1: Write the failing tests**

Append to `typescript/habitat/src/identity/habitat-identity-resolver.test.ts`. Add `delay` to the `msw` import at the top of the file so it reads `import { delay, http, HttpResponse } from "msw";`, and add `import { HabitatIdentityResolverError } from "./errors.js";`.

Note the `await promise.catch((e) => e)` pattern rather than `rejects.toThrow` — it lets each assertion inspect the concrete error object, including the negative `not.toBeInstanceOf` check that the abort test depends on.

```typescript
describe("HabitatIdentityResolver error handling", () => {
  it("throws with status and xrpcError for a 404 DidNotFound", async () => {
    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, () =>
        HttpResponse.json(
          { error: "DidNotFound", message: "DID not found" },
          { status: 404 },
        ),
      ),
    );

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
    expect((error as HabitatIdentityResolverError).status).toBe(404);
    expect((error as HabitatIdentityResolverError).xrpcError).toBe(
      "DidNotFound",
    );
    expect((error as Error).message).toContain("DID not found");
  });

  it("throws with status and xrpcError for a 400 InvalidRequest", async () => {
    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, () =>
        HttpResponse.json(
          { error: "InvalidRequest", message: "invalid identifier" },
          { status: 400 },
        ),
      ),
    );

    const error = await new HabitatIdentityResolver()
      .resolve("!!!")
      .catch((e: unknown) => e);

    expect((error as HabitatIdentityResolverError).status).toBe(400);
    expect((error as HabitatIdentityResolverError).xrpcError).toBe(
      "InvalidRequest",
    );
  });

  it("still reports the status when the error body is not JSON", async () => {
    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, () =>
        HttpResponse.text("upstream exploded", { status: 502 }),
      ),
    );

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
    expect((error as HabitatIdentityResolverError).status).toBe(502);
    expect((error as HabitatIdentityResolverError).xrpcError).toBeUndefined();
  });

  it("throws when the success body is not JSON", async () => {
    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, () =>
        HttpResponse.text("not json", { status: 200 }),
      ),
    );

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
  });

  it("throws when the payload has no DID", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      handle: HANDLE,
      didDoc: DID_DOC,
    });

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
  });

  it("throws when the DID uses an unsupported method", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: "did:example:nope",
      handle: HANDLE,
      didDoc: { ...DID_DOC, id: "did:example:nope" },
    });

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
  });

  it("throws when the DID document is missing", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: DID,
      handle: HANDLE,
    });

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
  });

  it("throws when the DID document id does not match the resolved DID", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: DID,
      handle: HANDLE,
      didDoc: { ...DID_DOC, id: "did:web:someone-else.habitat.network" },
    });

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
    expect((error as Error).message).toContain("someone-else");
  });

  it("throws when the handle is not a string", async () => {
    stubResolveIdentity(DEFAULT_HABITAT_SERVICE_URL, {
      did: DID,
      handle: 42,
      didDoc: DID_DOC,
    });

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
  });

  it("wraps a network failure and preserves the cause", async () => {
    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, () =>
        HttpResponse.error(),
      ),
    );

    const error = await new HabitatIdentityResolver()
      .resolve(DID)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
    expect((error as Error).cause).toBeDefined();
    expect((error as Error).message).toContain("pear.habitat.network");
  });

  it("requests an uncached response when noCache is set", async () => {
    let cacheMode: string | undefined;

    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, ({ request }) => {
        cacheMode = request.cache;
        return HttpResponse.json({ did: DID, handle: HANDLE, didDoc: DID_DOC });
      }),
    );

    const info = await new HabitatIdentityResolver().resolve(DID, {
      noCache: true,
    });

    expect(info.did).toBe(DID);
    // Known fragility: undici does not reliably surface `request.cache` to an
    // MSW handler. If `cacheMode` comes back as "default" or undefined, delete
    // this single assertion rather than adding a seam to production code to
    // make it observable — the behavior is a one-line pass-through to `fetch`.
    expect(cacheMode).toBe("no-store");
  });

  it("propagates AbortError without wrapping it", async () => {
    server.use(
      http.get(`${DEFAULT_HABITAT_SERVICE_URL}${XRPC_PATH}`, async () => {
        await delay("infinite");
        return HttpResponse.json({});
      }),
    );

    const controller = new AbortController();
    const promise = new HabitatIdentityResolver().resolve(DID, {
      signal: controller.signal,
    });
    controller.abort();

    const error = await promise.catch((e: unknown) => e);

    expect((error as Error).name).toBe("AbortError");
    expect(error).not.toBeInstanceOf(HabitatIdentityResolverError);
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd typescript/habitat && pnpm test
```

Expected: several FAIL — the resolver currently throws raw `TypeError`/`SyntaxError` instead of `HabitatIdentityResolverError`, and does not check `response.ok`, `didDoc`, or handle type.

- [ ] **Step 3: Rewrite `habitat-identity-resolver.ts` with full error handling**

This is the complete final contents of the file — replace it wholesale.

```typescript
import type {
  AtprotoDid,
  AtprotoDidDocument,
  IdentityInfo,
  IdentityResolver,
  ResolveIdentityOptions,
} from "@atproto-labs/identity-resolver";

import { HabitatIdentityResolverError } from "./errors.js";
import { normalizeHandle } from "./handle.js";

/** The Habitat instance operated by the Habitat project. */
export const DEFAULT_HABITAT_SERVICE_URL = "https://pear.habitat.network";

const RESOLVE_IDENTITY_PATH = "/xrpc/com.atproto.identity.resolveIdentity";

/** The DID methods the `AtprotoDid` type admits. */
const ATPROTO_DID_REGEX = /^did:(plc|web):/;

/**
 * Resolves AT Protocol identities through a Habitat instance's
 * `com.atproto.identity.resolveIdentity` endpoint.
 *
 * Unlike `AtprotoIdentityResolver`, this performs no client-side bidirectional
 * handle/DID verification — it delegates that to the Habitat instance, which
 * resolves through indigo's directory and falls back to the public network for
 * identities it does not host. Pointing `serviceUrl` at a host you do not trust
 * means trusting that host's identity claims.
 */
export class HabitatIdentityResolver implements IdentityResolver {
  readonly #serviceUrl: URL;

  constructor(serviceUrl: string | URL = DEFAULT_HABITAT_SERVICE_URL) {
    this.#serviceUrl = new URL(serviceUrl);
  }

  async resolve(
    identifier: string,
    options?: ResolveIdentityOptions,
  ): Promise<IdentityInfo> {
    const url = new URL(RESOLVE_IDENTITY_PATH, this.#serviceUrl);
    url.searchParams.set("identifier", identifier);

    let response: Response;
    try {
      response = await fetch(url, {
        method: "GET",
        headers: { accept: "application/json" },
        signal: options?.signal,
        ...(options?.noCache ? { cache: "no-store" as const } : {}),
      });
    } catch (cause) {
      // Cancellation must stay distinguishable from failure.
      if (isAbortError(cause)) throw cause;
      throw new HabitatIdentityResolverError(
        `Could not reach the Habitat instance at ${this.#serviceUrl.origin}`,
        { cause },
      );
    }

    if (!response.ok) {
      throw await toXrpcError(response);
    }

    let body: unknown;
    try {
      body = await response.json();
    } catch (cause) {
      if (isAbortError(cause)) throw cause;
      throw new HabitatIdentityResolverError(
        `Habitat instance at ${this.#serviceUrl.origin} returned a non-JSON identity payload`,
        { cause },
      );
    }

    return toIdentityInfo(body);
  }
}

function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === "AbortError";
}

/** Translates a non-2xx response into an error, tolerating non-XRPC bodies. */
async function toXrpcError(
  response: Response,
): Promise<HabitatIdentityResolverError> {
  let xrpcError: string | undefined;
  let detail: string | undefined;

  try {
    const body = (await response.json()) as {
      error?: unknown;
      message?: unknown;
    };
    if (typeof body?.error === "string") xrpcError = body.error;
    if (typeof body?.message === "string") detail = body.message;
  } catch {
    // A non-JSON error body still yields a useful error via `status`.
  }

  const summary = detail ?? xrpcError ?? response.statusText;

  return new HabitatIdentityResolverError(
    `Habitat identity resolution failed with status ${response.status}${
      summary ? `: ${summary}` : ""
    }`,
    { status: response.status, xrpcError },
  );
}

/**
 * Validates the XRPC payload before asserting it into `IdentityInfo`. This is
 * what makes the type assertions below honest rather than blind.
 */
function toIdentityInfo(body: unknown): IdentityInfo {
  if (typeof body !== "object" || body === null) {
    throw new HabitatIdentityResolverError(
      "Habitat instance returned a non-object identity payload",
    );
  }

  const { did, didDoc, handle } = body as Record<string, unknown>;

  if (typeof did !== "string" || !ATPROTO_DID_REGEX.test(did)) {
    throw new HabitatIdentityResolverError(
      `Habitat instance returned an invalid DID: ${JSON.stringify(did)}`,
    );
  }

  if (typeof didDoc !== "object" || didDoc === null) {
    throw new HabitatIdentityResolverError(
      `Habitat instance returned no DID document for ${did}`,
    );
  }

  const documentId = (didDoc as Record<string, unknown>).id;
  if (documentId !== did) {
    throw new HabitatIdentityResolverError(
      `DID document id ${JSON.stringify(documentId)} does not match resolved DID ${did}`,
    );
  }

  if (typeof handle !== "string") {
    throw new HabitatIdentityResolverError(
      `Habitat instance returned a non-string handle for ${did}`,
    );
  }

  return {
    did: did as AtprotoDid,
    didDoc: didDoc as AtprotoDidDocument,
    handle: normalizeHandle(handle),
  };
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd typescript/habitat && pnpm test
```

Expected: PASS, 35 tests total (34 if you deleted the `request.cache` assertion and its test).

If the abort test fails because MSW's `delay("infinite")` swallows the signal, replace the handler body with `await new Promise(() => {})` — the assertion is unchanged.

- [ ] **Step 5: Verify the build**

```bash
cd typescript/habitat && pnpm build
```

Expected: exits 0.

- [ ] **Step 6: Commit**

```bash
git add typescript/habitat/src/identity/habitat-identity-resolver.ts typescript/habitat/src/identity/habitat-identity-resolver.test.ts
git commit -m "feat(habitat-sdk): handle XRPC, network, and payload errors"
```

---

### Task 6: Public barrel exports

**Files:**
- Create: `typescript/habitat/src/identity/index.ts`
- Modify: `typescript/habitat/src/index.ts`
- Test: `typescript/habitat/src/index.test.ts`

**Interfaces:**
- Consumes: everything from Tasks 2–5.
- Produces: the package's entire public surface, re-exported from `src/index.ts`.

- [ ] **Step 1: Write the failing test**

Create `typescript/habitat/src/index.test.ts`. This is the package's contract test — it fails if anything stops being reachable from the entry point.

```typescript
import { describe, expect, it } from "vitest";

import type { IdentityResolver } from "@atproto-labs/identity-resolver";

import {
  DEFAULT_HABITAT_SERVICE_URL,
  HabitatIdentityResolver,
  HabitatIdentityResolverError,
} from "./index.js";

describe("package entry point", () => {
  it("exports the default Habitat service URL", () => {
    expect(DEFAULT_HABITAT_SERVICE_URL).toBe("https://pear.habitat.network");
  });

  it("exports a resolver assignable to IdentityResolver", () => {
    const resolver: IdentityResolver = new HabitatIdentityResolver();

    expect(resolver).toBeInstanceOf(HabitatIdentityResolver);
    expect(typeof resolver.resolve).toBe("function");
  });

  it("exports the error type", () => {
    expect(new HabitatIdentityResolverError("boom")).toBeInstanceOf(Error);
  });

  it("does not leak internal handle helpers", async () => {
    const entry: Record<string, unknown> = await import("./index.js");

    expect(entry.normalizeHandle).toBeUndefined();
    expect(entry.HANDLE_INVALID).toBeUndefined();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd typescript/habitat && pnpm test index
```

Expected: FAIL — `./index.js` exports nothing (it is still the `export {}` placeholder).

- [ ] **Step 3: Create `typescript/habitat/src/identity/index.ts`**

`handle.ts` is deliberately absent: `normalizeHandle` and `HANDLE_INVALID` are internal.

```typescript
export {
  DEFAULT_HABITAT_SERVICE_URL,
  HabitatIdentityResolver,
} from "./habitat-identity-resolver.js";
export { HabitatIdentityResolverError } from "./errors.js";
export type { HabitatIdentityResolverErrorOptions } from "./errors.js";

/**
 * Re-exported so consumers can type against the resolver without adding a
 * direct `@atproto-labs/identity-resolver` import.
 */
export type {
  AtprotoDid,
  AtprotoDidDocument,
  IdentityInfo,
  IdentityResolver,
  ResolveIdentityOptions,
} from "@atproto-labs/identity-resolver";
```

- [ ] **Step 4: Replace `typescript/habitat/src/index.ts`**

Future domain folders (`spaces/`, `permissions/`) add one line each here.

```typescript
export * from "./identity/index.js";
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd typescript/habitat && pnpm test
```

Expected: PASS, 39 tests total.

- [ ] **Step 6: Verify the emitted declarations expose the public surface**

```bash
cd typescript/habitat && pnpm build && cat dist/index.d.ts dist/identity/index.d.ts
```

Expected: `dist/identity/index.d.ts` re-exports `HabitatIdentityResolver`, `HabitatIdentityResolverError`, `DEFAULT_HABITAT_SERVICE_URL`, and the five types.

- [ ] **Step 7: Commit**

```bash
git add typescript/habitat/src/index.ts typescript/habitat/src/index.test.ts typescript/habitat/src/identity/index.ts
git commit -m "feat(habitat-sdk): expose public package exports"
```

---

### Task 7: README and publish readiness

**Files:**
- Create: `typescript/habitat/README.md`

**Interfaces:**
- Consumes: the public surface from Task 6.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write `typescript/habitat/README.md`**

````markdown
# @habitat-network/habitat

TypeScript SDK for building on [Habitat](https://habitat.network), a data
ownership layer for organizations built on [AT Protocol](https://atproto.com)
primitives.

## Install

```bash
npm install @habitat-network/habitat
```

`@atproto-labs/identity-resolver` is a peer dependency. If you are already using
`@atproto/oauth-client` or `@atproto/oauth-client-browser`, you have it.

This package has **no runtime dependencies** — the peer is used for types only.

## Identity resolution

Habitat organizations use `did:web` identities that Habitat itself hosts, so the
stock `AtprotoIdentityResolver` cannot see them. `HabitatIdentityResolver`
resolves through a Habitat instance's `com.atproto.identity.resolveIdentity`
endpoint, which handles both Habitat-hosted and public-network identities.

```ts
import { HabitatIdentityResolver } from "@habitat-network/habitat";

const resolver = new HabitatIdentityResolver();

const { did, handle, didDoc } = await resolver.resolve("acme.habitat.network");
```

It satisfies `IdentityResolver`, so it drops straight into an OAuth client:

```ts
import { BrowserOAuthClient } from "@atproto/oauth-client-browser";
import { HabitatIdentityResolver } from "@habitat-network/habitat";

const client = new BrowserOAuthClient({
  clientMetadata,
  handleResolver: "https://bsky.social",
  identityResolver: new HabitatIdentityResolver(),
});
```

### Self-hosted instances

Pass a service URL to target your own Habitat instance. It defaults to
`https://pear.habitat.network`.

```ts
new HabitatIdentityResolver("https://pear.example.com");
```

### Errors

Every failure — unreachable host, non-2xx XRPC response, malformed payload —
throws `HabitatIdentityResolverError`:

```ts
import { HabitatIdentityResolverError } from "@habitat-network/habitat";

try {
  await resolver.resolve("nobody.example.com");
} catch (error) {
  if (error instanceof HabitatIdentityResolverError) {
    console.error(error.status, error.xrpcError); // 404, "HandleNotFound"
  }
}
```

Aborts are the exception: passing an aborted `signal` rejects with the original
`AbortError`, never wrapped, so cancellation stays distinguishable from failure.

```ts
await resolver.resolve("acme.habitat.network", { signal: controller.signal });
```

## Trust model

Unlike `AtprotoIdentityResolver`, this resolver does **no client-side
bidirectional handle↔DID verification**. It delegates that to the Habitat
instance, which resolves through indigo's directory — that directory verifies
bidirectionally and falls back to public-network resolution for identities
Habitat does not host.

Pointing `serviceUrl` at a host you do not trust therefore means trusting that
host's identity claims outright.

## License

Apache-2.0
````

- [ ] **Step 2: Verify the published file list**

```bash
cd typescript/habitat && npm pack --dry-run
```

Expected: exactly `dist/**`, `README.md`, `LICENSE`, `package.json`. If any `*.test.js`, `*.map`, or `src/**` file appears, the `tsconfig.json` `exclude` list or the `files` array is wrong — fix before continuing.

- [ ] **Step 3: Verify the package resolves from outside the monorepo**

This is the check that catches peer-dependency and `exports`-map mistakes that every in-repo test would miss.

```bash
cd typescript/habitat && pnpm build && npm pack
mkdir -p /tmp/habitat-sdk-check && cd /tmp/habitat-sdk-check
npm init -y >/dev/null
npm install "$OLDPWD"/habitat-network-habitat-0.1.0.tgz @atproto/oauth-client-browser typescript
```

Create `/tmp/habitat-sdk-check/check.ts`:

```typescript
import { BrowserOAuthClient } from "@atproto/oauth-client-browser";
import {
  HabitatIdentityResolver,
  type IdentityResolver,
} from "@habitat-network/habitat";

const resolver: IdentityResolver = new HabitatIdentityResolver();

export const client = new BrowserOAuthClient({
  clientMetadata: undefined,
  handleResolver: "https://bsky.social",
  identityResolver: resolver,
});
```

Then typecheck it:

```bash
cd /tmp/habitat-sdk-check && npx tsc --noEmit --strict --module nodenext --moduleResolution nodenext --target es2022 --lib es2022,dom check.ts
```

Expected: exits 0. Clean up:

```bash
rm -rf /tmp/habitat-sdk-check "$OLDPWD"/habitat-network-habitat-0.1.0.tgz
```

- [ ] **Step 4: Run the repo-wide checks**

```bash
moon typescript/habitat:test
moon typescript/habitat:build
moon :lint-check
moon :format-check
```

Expected: all exit 0. If `format-check` fails, run `moon typescript/habitat:format` and amend.

- [ ] **Step 5: Commit**

```bash
git add typescript/habitat/README.md
git commit -m "docs(habitat-sdk): add package README"
```

---

## Publishing (manual, out of scope for this plan)

Do not run these as part of implementation. Recorded here so the steps exist:

```bash
cd typescript/habitat
npm login
pnpm publish --access public --no-git-checks
```

The `@habitat-network` npm organization must exist and the publishing account
must have write access to it. Neither has been verified.

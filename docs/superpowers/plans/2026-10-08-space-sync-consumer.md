# Space Sync Consumer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `internal/spaceSync`, an Effect v4 library implementing the syncer (consumer) side of the atproto spaces sync protocol (proposal 0016) for a long-running Node service tracking tens of thousands of spaces.

**Architecture:** Host-provided ports (`DelegationSource`, `SyncStore`, `SyncSink`) are wired to internal services (`Identity`, `Credentials`, `SpaceClient`) through Effect `Layer`s. `SpaceSyncer` starts a per-space fiber on demand in a `FiberMap`, capped by a global `Semaphore`. Each fiber runs coalesced reconcile passes (`listRepos` walk → per-repo `listRepoOps` incremental sync with `getRepo` CAR recovery) and exits when idle. A single scheduler fiber reads due spaces from the store. Crypto and verification come from `@atproto/space`.

**Tech Stack:** TypeScript (workspace `typescript ^7`), `effect@^4.0.2`, `@effect/vitest@^4.0.2` on vitest 5, `msw@^2.15` for network mocking, `@atproto/space` / `@atproto/crypto` / `@atproto/identity` / `@atproto/common-web` / `@atproto/lex` (spaces alpha `0.0.0-spaces-alpha-20261001173819`).

**Spec:** `docs/superpowers/specs/2026-10-08-space-sync-consumer-design.md`. Read the "Planning revisions" section at its end; it overrides earlier sections.

## Global Constraints

- Code lives in `typescript/internal/src/spaceSync/` and is exported as subpath `internal/spaceSync` (source-exported like the package's other subpaths; no build step).
- Effect v4 idioms per https://github.com/Effect-TS/effect/blob/main/LLMS.md: `Context.Service` classes with static `layer`s, `Effect.fn("Name")` for public operations, `Effect.fnUntraced` for internals, `Schema.TaggedError` for errors, `Schema` to decode all untrusted data, `Predicate` for guards (never hand-written `isRecord`-style helpers).
- Service keys are namespaced `"internal/spaceSync/<Name>"`.
- Wire format comes from the generated `api` package (`lexicons/` matches the 2026-10-01 alpha since #1140). Use `com.atproto.space.*` types and schemas directly; do not alias or restate them. Make outbound calls with `@atproto/lex` `xrpc`/`xrpcSafe` on `signedAgent` (`getRepo` alone reads its body directly so the CAR streams). If `api` types look stale, run `tsc --build` in `typescript/api`.
- Space refs and DIDs are `SpaceRefString` / `DidString` from `@atproto/syntax`, not plain strings.
- Timestamps are epoch milliseconds from `Clock.currentTimeMillis`.
- Assumes the workspace is already on vitest 5 (done separately by the user). If `pnpm --filter internal exec vitest --version` reports 4.x, stop and tell the user.
- Every test file starts with `// @vitest-environment node` (the package default is jsdom). Network calls in tests go through msw handlers registered on the shared `server` from `typescript/internal/src/test/msw.ts`, which errors on unhandled requests.
- Imports inside the package are extensionless (match `typescript/internal/src`).
- Do not remove existing comments. Use Prettier formatting (`moon run internal:format`).
- Run tests from the repo root with `pnpm --filter internal exec vitest run src/spaceSync/<file>`.

## Review Focus

1. **Notification storm on one space.** Hundreds of `notifyWrite` for the same space while a pass runs should cause at most one extra pass, not hundreds. Covered in Task 9 test "coalesces a burst of notifications into one extra pass".
2. **Writer appears on several `listRepos` pages** (updated mid-walk). It should be synced once, at its latest `repoRev`. Covered in Task 8 test "dedupes a writer that reappears in the listing".
3. **Host returns `listRepos` entries out of order or with a mismatched cursor.** The pass should fail (backoff) without advancing the checkpoint. Covered in Task 8 test "rejects an unordered listRepos response".
4. **Record deleted then recreated within one oplog window.** The batch should contain a single final change for that path. Covered in Task 7 test "collapses multiple ops on one path into the final change".
5. **`unwatch` during a long `Reset` download.** The sink apply is interrupted, the store doesn't advance, and nothing is written for the space afterwards. Covered in Task 9 test "unwatch interrupts an in-flight sink apply and leaves no state".

---

## File Structure

```
typescript/internal/
  package.json                       (modify: deps + "./spaceSync" export)
  src/spaceSync/
    index.ts                         public exports
    types.ts                         SpaceState, RepoState, Change, RepoBatch, SyncEvent
    errors.ts                        Schema.TaggedError classes
    wire.ts                          signedAgent, failureStatus, decodeLex, toSpaceRef, parseSpaceRef
    config.ts                        SpaceSyncConfig reference + spaceSyncConfigLayer
    Identity.ts                      Identity service (DID → pds / signing key / space host)
    Credentials.ts                   DelegationSource port + Credentials service
    SpaceClient.ts                   signed XRPC calls with retry + credential refresh
    SyncStore.ts                     SyncStore port + memory layer
    SyncSink.ts                      SyncSink port
    repoSync.ts                      syncRepo: incremental + recovery + commit
    spacePass.ts                     runSpacePass, recordPassFailure, backoffMillis
    SpaceSyncer.ts                   SpaceSyncer service (triggers, FiberMap, scheduler, events)
    notification.ts                  verifyNotification
    promise.ts                       createSpaceSyncer Promise facade
    test/fakeNetwork.ts              in-memory PLC + space host + repo hosts as msw handlers
    test/recordingSink.ts            SyncSink that materializes a view and records batches
    test/harness.ts                  testConfig, fakeDelegation, makeHarness, runWithHarness
    wire.test.ts  Identity.test.ts  fakeNetwork.test.ts  Credentials.test.ts
    SpaceClient.test.ts  SyncStore.test.ts  repoSync.test.ts  spacePass.test.ts
    SpaceSyncer.test.ts  notification.test.ts  promise.test.ts
```

---

### Task 1: Package scaffold, core types, errors, wire decoders, config

**Files:**
- Modify: `typescript/internal/package.json`
- Create: `typescript/internal/src/spaceSync/types.ts`, `errors.ts`, `wire.ts`, `config.ts`, `index.ts`
- Test: `typescript/internal/src/spaceSync/wire.test.ts`

**Interfaces:**
- Produces:
  - `types.ts`: `SpaceState`, `RepoState`, `Change`, `RepoBatch`, `SyncEvent`, `ListedRepo`
  - `errors.ts`: `InvalidSpaceRefError`, `IdentityError`, `CredentialError` (with `reason`), `XrpcError`, `RepoVerificationError`, `RepoSyncError`, `SinkError`, `StoreError`, `NotificationAuthError`, `errorMessage(e: unknown): string`
  - `wire.ts`: `signedAgent(service, sign: () => Promise<Record<string,string>>) → Agent`, `failureStatus(failure: XrpcFailure) → number`, `decodeLex(schema) → (json: unknown) => Effect<InferOutput<S>, WireDecodeError>`, `toSpaceRef(space) → Effect<SpaceRefString, InvalidSpaceRefError>`, `parseSpaceRef(space) → Effect<{authority: DidString, type: NsidString, skey: RecordKeyString}, InvalidSpaceRefError>`. No wire type aliases: consumers import `com` from `"api"`.
  - `errors.ts` also has `WireDecodeError`.
  - `config.ts`: `SpaceSyncOptions`, `defaultSpaceSyncOptions`, `SpaceSyncConfig` (Context.Reference), `spaceSyncConfigLayer(options)`

- [ ] **Step 1: Add dependencies and the subpath export**

In `typescript/internal/package.json`, add to `"exports"` (after `"./hooks"`):

```json
    "./spaceSync": {
      "types": "./src/spaceSync/index.ts",
      "import": "./src/spaceSync/index.ts"
    },
```

Add to `"dependencies"` (keep alphabetical order):

```json
    "@atproto/crypto": "catalog:",
    "effect": "^4.0.2",
```

Add to `"devDependencies"`:

```json
    "@effect/vitest": "^4.0.2",
```

Run: `pnpm install`
Expected: completes. `pnpm --filter internal exec vitest --version` prints `5.x`.

- [ ] **Step 2: Write `types.ts`**

```ts
import type { Cid, LexMap } from "@atproto/lex";
import type { VerifiedRecord } from "@atproto/space";
import type { Stream } from "effect";
import type { RepoVerificationError } from "./errors";

/** `at://{authority}/space/{type}/{skey}` */
export type SpaceRef = string;
export type Did = string;

/** Durable per-space sync state, owned by the host's SyncStore. Times are epoch ms. */
export interface SpaceState {
  readonly space: SpaceRef;
  readonly authority: Did;
  /** Last listRepos checkpoint every repo up to which has been applied. */
  readonly spaceRev?: string | undefined;
  readonly registrationExpiresAt?: number | undefined;
  readonly nextDueAt: number;
  readonly lastFullPassAt?: number | undefined;
  readonly failures: number;
  readonly lastError?: string | undefined;
}

/** Durable per-repo sync state: the verified revision and its LtHash state. */
export interface RepoState {
  readonly space: SpaceRef;
  readonly did: Did;
  readonly rev: string;
  /** 2048-byte LtHash state (`RepoCommit.setHash.state()`). */
  readonly ltHash: Uint8Array;
}

/** One record change. `cid: null` is a delete. `value` is absent when the host did not inline it. */
export interface Change {
  readonly uri: string;
  readonly collection: string;
  readonly rkey: string;
  readonly cid: Cid | null;
  readonly value?: LexMap | undefined;
}

export type RepoBatch =
  | {
      readonly _tag: "Ops";
      readonly space: SpaceRef;
      readonly did: Did;
      readonly rev: string;
      readonly changes: ReadonlyArray<Change>;
    }
  | {
      /** Replace the repo's contents with exactly these records. The sink must drain `records`. */
      readonly _tag: "Reset";
      readonly space: SpaceRef;
      readonly did: Did;
      readonly rev: string;
      readonly records: Stream.Stream<VerifiedRecord, RepoVerificationError>;
    }
  | { readonly _tag: "RepoRemoved"; readonly space: SpaceRef; readonly did: Did }
  | { readonly _tag: "SpaceDeleted"; readonly space: SpaceRef };

/** What SpaceSyncer.events publishes after a batch is committed. */
export type SyncEvent =
  | Extract<RepoBatch, { _tag: "Ops" | "RepoRemoved" | "SpaceDeleted" }>
  | { readonly _tag: "Reset"; readonly space: SpaceRef; readonly did: Did; readonly rev: string };

/** A writer as reported by listRepos. */
export interface ListedRepo {
  readonly did: Did;
  readonly repoRev: string;
  readonly spaceRev: string;
}
```

- [ ] **Step 3: Write `errors.ts`**

```ts
import { Predicate, Schema } from "effect";

export class InvalidSpaceRefError extends Schema.TaggedError<InvalidSpaceRefError>()(
  "InvalidSpaceRefError",
  { space: Schema.String },
) {}

export class IdentityError extends Schema.TaggedError<IdentityError>()("IdentityError", {
  did: Schema.String,
  message: Schema.String,
}) {}

/**
 * Failure to obtain or use a space credential. Only `SpaceDeleted` says anything
 * about the space itself (proposal: "A renewal that fails for any other reason
 * says nothing about the space").
 */
export class CredentialError extends Schema.TaggedError<CredentialError>()("CredentialError", {
  space: Schema.String,
  reason: Schema.Literals(["SpaceDeleted", "SpaceNotFound", "NotAuthorized", "NoDelegation", "Transport"]),
  message: Schema.String,
}) {}

/** A non-2xx (or unreadable) XRPC response. `status: 0` means the request never completed. */
export class XrpcError extends Schema.TaggedError<XrpcError>()("XrpcError", {
  method: Schema.String,
  status: Schema.Number,
  error: Schema.optional(Schema.String),
  message: Schema.String,
}) {}

/** Internal: a repo failed verification; triggers full-state recovery. */
export class RepoVerificationError extends Schema.TaggedError<RepoVerificationError>()(
  "RepoVerificationError",
  { space: Schema.String, did: Schema.String, message: Schema.String },
) {}

/** Recovery itself failed; the repo is retried on a later pass. */
export class RepoSyncError extends Schema.TaggedError<RepoSyncError>()("RepoSyncError", {
  space: Schema.String,
  did: Schema.String,
  message: Schema.String,
  cause: Schema.Defect(),
}) {}

export class SinkError extends Schema.TaggedError<SinkError>()("SinkError", {
  message: Schema.String,
  cause: Schema.Defect(),
}) {}

export class StoreError extends Schema.TaggedError<StoreError>()("StoreError", {
  message: Schema.String,
  cause: Schema.Defect(),
}) {}

export class NotificationAuthError extends Schema.TaggedError<NotificationAuthError>()(
  "NotificationAuthError",
  { message: Schema.String },
) {}

export const errorMessage = (error: unknown): string =>
  Predicate.hasProperty(error, "message") && Predicate.isString(error.message)
    ? error.message
    : String(error);
```

- [ ] **Step 4: Write the failing test `wire.test.ts`**

> **Superseded (2026-10-08, after #1140):** the code below predates the switch to the `api` package (fixtures now need valid TIDs/CIDs). The source in `typescript/internal/src/spaceSync/wire.test.ts` is authoritative; see the spec's Planning revision #1.

```ts
// @vitest-environment node
import { it } from "@effect/vitest";
import { toBase64 } from "@atproto/lex";
import { Effect, Exit, Schema } from "effect";
import { describe, expect } from "vitest";
import { ListRepoOpsOutput, NotifyWriteInput, lexJson, parseSpaceRef } from "./wire";

const SPACE = "at://did:plc:alice/space/com.example.board/main";

describe("parseSpaceRef", () => {
  it.effect("splits a space ref into authority, type and skey", () =>
    Effect.gen(function* () {
      const ref = yield* parseSpaceRef(SPACE);
      expect(ref).toEqual({ authority: "did:plc:alice", type: "com.example.board", skey: "main" });
    }),
  );

  it.effect("rejects record URIs and public repo URIs", () =>
    Effect.gen(function* () {
      for (const bad of [
        `${SPACE}/did:plc:bob/com.example.post/1`,
        "at://did:plc:alice/com.example.post/1",
        "https://example.com",
      ]) {
        const exit = yield* Effect.exit(parseSpaceRef(bad));
        expect(Exit.isFailure(exit)).toBe(true);
      }
    }),
  );
});

describe("wire decoding", () => {
  it.effect("decodes listRepoOps JSON with $bytes into a signed commit", () =>
    Effect.gen(function* () {
      const bytes = (n: number) => ({ $bytes: toBase64(new Uint8Array(32).fill(n)) });
      const json = {
        ops: [
          { rev: "3l2", collection: "com.example.post", rkey: "1", cid: null, prev: "bafyprev" },
        ],
        commit: { ver: 1, hash: bytes(1), ikm: bytes(2), sig: bytes(3), mac: bytes(4), rev: "3l2" },
      };
      const out = yield* Schema.decodeUnknownEffect(ListRepoOpsOutput)(lexJson(json));
      expect(out.commit?.hash).toBeInstanceOf(Uint8Array);
      expect(out.commit?.hash[0]).toBe(1);
      expect(out.ops[0].cid).toBeNull();
      expect(out.cursor).toBeUndefined();
    }),
  );

  it.effect("decodes a forwarded notifyWrite body", () =>
    Effect.gen(function* () {
      const body = {
        space: SPACE,
        repo: "did:plc:bob",
        repoRev: "3l2",
        hash: { $bytes: toBase64(new Uint8Array(32)) },
        spaceRev: "3l3",
      };
      const input = yield* Schema.decodeUnknownEffect(NotifyWriteInput)(lexJson(body));
      expect(input.spaceRev).toBe("3l3");
      expect(input.prevSpaceRev).toBeUndefined();
      expect(input.hash).toBeInstanceOf(Uint8Array);
    }),
  );
});
```

- [ ] **Step 5: Run it to see it fail**

Run: `pnpm --filter internal exec vitest run src/spaceSync/wire.test.ts`
Expected: FAIL. Cannot resolve `./wire`.

- [ ] **Step 6: Write `wire.ts`**

> **Superseded (2026-10-08, after #1140):** the code below predates the switch to the `api` package. The source in `typescript/internal/src/spaceSync/wire.ts` is authoritative; see the spec's Planning revision #1.

```ts
import { jsonToLex } from "@atproto/lex";
import { Effect, Schema } from "effect";
import { InvalidSpaceRefError, XrpcError } from "./errors";

// Wire shapes follow the upstream alpha lexicons (../atproto/lexicons/com/atproto/space),
// not habitat's lexicons/, which predate spaceRev/repoRev and HTTP message signatures.

const Bytes = Schema.Uint8Array;

export const SignedCommit = Schema.Struct({
  ver: Schema.Literal(1),
  hash: Bytes,
  ikm: Bytes,
  sig: Bytes,
  mac: Bytes,
  rev: Schema.String,
});

export const ListReposRepo = Schema.Struct({
  did: Schema.String,
  repoRev: Schema.String,
  spaceRev: Schema.String,
  hash: Bytes,
});

export const ListReposOutput = Schema.Struct({
  repos: Schema.Array(ListReposRepo),
  cursor: Schema.optional(Schema.String),
});

export const OpEntry = Schema.Struct({
  rev: Schema.String,
  collection: Schema.String,
  rkey: Schema.String,
  cid: Schema.NullOr(Schema.String),
  prev: Schema.NullOr(Schema.String),
  value: Schema.optional(Schema.Unknown),
});

export const ListRepoOpsOutput = Schema.Struct({
  ops: Schema.Array(OpEntry),
  commit: Schema.optional(SignedCommit),
  cursor: Schema.optional(Schema.String),
});

export const RegisterNotifyOutput = Schema.Struct({ expiresAt: Schema.String });

export const GetSpaceCredentialOutput = Schema.Struct({ credential: Schema.String });

export const NotifyWriteInput = Schema.Struct({
  space: Schema.String,
  repo: Schema.String,
  repoRev: Schema.String,
  hash: Bytes,
  spaceRev: Schema.optional(Schema.String),
  prevSpaceRev: Schema.optional(Schema.String),
});
export type NotifyWriteInput = typeof NotifyWriteInput.Type;

export const NotifySpaceDeletedInput = Schema.Struct({ space: Schema.String });

/** Convert atproto JSON (`{$bytes}`, `{$link}`) into lex values before Schema decoding. */
export const lexJson = (json: unknown): unknown =>
  jsonToLex(json as Parameters<typeof jsonToLex>[0]);

export const invalidResponse =
  (method: string) =>
  (error: Schema.SchemaError): XrpcError =>
    new XrpcError({ method, status: 200, error: "InvalidResponse", message: error.message });

const SPACE_REF = /^at:\/\/(did:[a-z]+:[a-zA-Z0-9._:%-]+)\/space\/([a-zA-Z][a-zA-Z0-9.-]*)\/([A-Za-z0-9._:~-]{1,512})$/;

export const parseSpaceRef = (
  space: string,
): Effect.Effect<{ authority: string; type: string; skey: string }, InvalidSpaceRefError> => {
  const match = SPACE_REF.exec(space);
  return match
    ? Effect.succeed({ authority: match[1], type: match[2], skey: match[3] })
    : Effect.fail(new InvalidSpaceRefError({ space }));
};
```

- [ ] **Step 7: Write `config.ts`**

```ts
import { Context, Duration, Layer } from "effect";

export interface SpaceSyncOptions {
  /** Our service identifier, sent to registerNotify and checked as `aud` on notifications. */
  readonly serviceDid: string;
  readonly plcUrl: string;
  /** Max spaces running a pass at once (global). */
  readonly maxActiveSpaces: number;
  /** Max repos synced at once within one space pass. */
  readonly repoConcurrency: number;
  readonly schedulerInterval: Duration.Duration;
  readonly schedulerPageSize: number;
  readonly fullPassInterval: Duration.Duration;
  readonly registrationRenewLead: Duration.Duration;
  readonly backoffBase: Duration.Duration;
  readonly backoffCap: Duration.Duration;
  readonly credentialCacheCapacity: number;
  readonly credentialRefreshLead: Duration.Duration;
  readonly requestRetryBase: Duration.Duration;
  readonly requestRetries: number;
}

export const defaultSpaceSyncOptions: SpaceSyncOptions = {
  serviceDid: "",
  plcUrl: "https://plc.directory",
  maxActiveSpaces: 64,
  repoConcurrency: 8,
  schedulerInterval: Duration.seconds(30),
  schedulerPageSize: 500,
  fullPassInterval: Duration.hours(6),
  registrationRenewLead: Duration.hours(1),
  backoffBase: Duration.seconds(30),
  backoffCap: Duration.hours(1),
  credentialCacheCapacity: 1024,
  credentialRefreshLead: Duration.seconds(30),
  requestRetryBase: Duration.millis(500),
  requestRetries: 2,
};

export const SpaceSyncConfig = Context.Reference<SpaceSyncOptions>(
  "internal/spaceSync/SpaceSyncConfig",
  { defaultValue: () => defaultSpaceSyncOptions },
);

export const spaceSyncConfigLayer = (
  options: Partial<SpaceSyncOptions> & { readonly serviceDid: string },
) => Layer.succeed(SpaceSyncConfig, { ...defaultSpaceSyncOptions, ...options });
```

- [ ] **Step 8: Write a minimal `index.ts`**

```ts
export * from "./config";
export * from "./errors";
export * from "./types";
export { NotifyWriteInput, parseSpaceRef } from "./wire";
```

- [ ] **Step 9: Run the test and typecheck**

Run: `pnpm --filter internal exec vitest run src/spaceSync/wire.test.ts && pnpm --filter internal exec tsc --noEmit -p .`
Expected: 4 tests PASS; tsc reports no errors in `src/spaceSync`.

- [ ] **Step 10: Commit**

```bash
git add typescript/internal/package.json pnpm-lock.yaml typescript/internal/src/spaceSync
git commit -m "spaceSync: scaffold types, errors, wire decoders and config

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Identity service

**Files:**
- Create: `typescript/internal/src/spaceSync/Identity.ts`
- Test: `typescript/internal/src/spaceSync/Identity.test.ts`

**Interfaces:**
- Consumes: `SpaceSyncConfig` (`plcUrl`), `IdentityError`.
- Produces: `ResolvedIdentity { did; pds; signingKey /* did:key */; spaceHost }`, `identityFromDoc(did, doc: DidDocument): Effect<ResolvedIdentity, IdentityError>`, `class Identity` with `resolve(did) → Effect<ResolvedIdentity, IdentityError>` and `static layer: Layer<Identity>`.

- [ ] **Step 1: Write the failing test**

```ts
// @vitest-environment node
import { it } from "@effect/vitest";
import { Secp256k1Keypair } from "@atproto/crypto";
import type { DidDocument } from "@atproto/identity";
import { Effect, Exit, Layer } from "effect";
import { HttpResponse, http } from "msw";
import { describe, expect } from "vitest";
import { server } from "../test/msw";
import { spaceSyncConfigLayer } from "./config";
import { Identity, identityFromDoc } from "./Identity";

const DID = "did:plc:aliceaaaaaaaaaaaaaaaaaaa";

const docWith = async (extra: Partial<DidDocument> = {}): Promise<DidDocument> => {
  const key = await Secp256k1Keypair.create();
  return {
    id: DID,
    verificationMethod: [
      { id: `${DID}#atproto`, type: "Multikey", controller: DID, publicKeyMultibase: key.did().slice("did:key:".length) },
    ],
    service: [{ id: "#atproto_pds", type: "AtprotoPersonalDataServer", serviceEndpoint: "https://pds.test" }],
    ...extra,
  } as DidDocument;
};

describe("identityFromDoc", () => {
  it.effect("falls back to the PDS when there is no #atproto_space_host", () =>
    Effect.gen(function* () {
      const doc = yield* Effect.promise(() => docWith());
      const id = yield* identityFromDoc(DID, doc);
      expect(id.pds).toBe("https://pds.test");
      expect(id.spaceHost).toBe("https://pds.test");
      expect(id.signingKey).toMatch(/^did:key:z/);
    }),
  );

  it.effect("uses a declared #atproto_space_host", () =>
    Effect.gen(function* () {
      const base = yield* Effect.promise(() => docWith());
      const doc = {
        ...base,
        service: [...(base.service ?? []), { id: "#atproto_space_host", type: "AtprotoSpaceHost", serviceEndpoint: "https://spaces.test" }],
      } as DidDocument;
      expect((yield* identityFromDoc(DID, doc)).spaceHost).toBe("https://spaces.test");
    }),
  );

  it.effect("errors instead of falling back when #atproto_space_host is malformed", () =>
    Effect.gen(function* () {
      const base = yield* Effect.promise(() => docWith());
      const doc = {
        ...base,
        service: [...(base.service ?? []), { id: "#atproto_space_host", type: "Wrong", serviceEndpoint: "https://spaces.test" }],
      } as DidDocument;
      const exit = yield* Effect.exit(identityFromDoc(DID, doc));
      expect(Exit.isFailure(exit)).toBe(true);
    }),
  );
});

describe("Identity.layer", () => {
  it.effect("resolves did:plc through the configured PLC directory and caches", () =>
    Effect.gen(function* () {
      const doc = yield* Effect.promise(() => docWith());
      let hits = 0;
      server.use(
        http.get("https://plc.test/:did", () => {
          hits++;
          return HttpResponse.json(doc);
        }),
      );
      const identity = yield* Identity;
      expect((yield* identity.resolve(DID)).pds).toBe("https://pds.test");
      yield* identity.resolve(DID);
      expect(hits).toBe(1);
    }).pipe(
      Effect.provide(
        Identity.layer.pipe(Layer.provide(spaceSyncConfigLayer({ serviceDid: "did:web:s.test", plcUrl: "https://plc.test" }))),
      ),
    ),
  );
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `pnpm --filter internal exec vitest run src/spaceSync/Identity.test.ts`
Expected: FAIL. Cannot resolve `./Identity`.

- [ ] **Step 3: Write `Identity.ts`**

```ts
import { getServiceEndpoint } from "@atproto/common-web";
import { type DidDocument, IdResolver, getKey, getPds } from "@atproto/identity";
import { Cache, Context, Duration, Effect, Exit, Layer } from "effect";
import { SpaceSyncConfig } from "./config";
import { IdentityError, errorMessage } from "./errors";

export interface ResolvedIdentity {
  readonly did: string;
  /** Repo host for this DID's space repos. */
  readonly pds: string;
  /** `#atproto` signing key as a did:key. Verifies commits and service auth. */
  readonly signingKey: string;
  /** Space host when this DID is a space authority (`#atproto_space_host`, else the PDS). */
  readonly spaceHost: string;
}

const hasEntry = (
  entries: ReadonlyArray<{ readonly id: string }> | undefined,
  did: string,
  fragment: string,
): boolean => (entries ?? []).some((entry) => entry.id === fragment || entry.id === `${did}${fragment}`);

/**
 * Applies the proposal's fallback rules: a missing #atproto_space_host falls back
 * to #atproto_pds, but a present-and-malformed one is an error.
 */
export const identityFromDoc = (
  did: string,
  doc: DidDocument,
): Effect.Effect<ResolvedIdentity, IdentityError> =>
  Effect.gen(function* () {
    const pds = getPds(doc);
    if (!pds) return yield* new IdentityError({ did, message: "missing #atproto_pds service" });
    const signingKey = getKey(doc);
    if (!signingKey) return yield* new IdentityError({ did, message: "missing #atproto signing key" });
    let spaceHost = pds;
    if (hasEntry(doc.service, doc.id, "#atproto_space_host")) {
      const endpoint = getServiceEndpoint(doc, { id: "#atproto_space_host", type: "AtprotoSpaceHost" });
      if (!endpoint) return yield* new IdentityError({ did, message: "malformed #atproto_space_host service" });
      spaceHost = endpoint;
    }
    return { did, pds, signingKey, spaceHost };
  });

export class Identity extends Context.Service<
  Identity,
  { readonly resolve: (did: string) => Effect.Effect<ResolvedIdentity, IdentityError> }
>()("internal/spaceSync/Identity") {
  static readonly layer = Layer.effect(
    Identity,
    Effect.gen(function* () {
      const { plcUrl } = yield* SpaceSyncConfig;
      const resolver = new IdResolver({ plcUrl });
      const cache = yield* Cache.makeWith(
        (did: string) =>
          Effect.tryPromise({
            try: () => resolver.did.ensureResolve(did),
            catch: (error) => new IdentityError({ did, message: `could not resolve: ${errorMessage(error)}` }),
          }).pipe(Effect.flatMap((doc) => identityFromDoc(did, doc))),
        {
          capacity: 10_000,
          // Never cache failures: a transient PLC outage must not stick for 10 minutes.
          timeToLive: (exit) => (Exit.isSuccess(exit) ? Duration.minutes(10) : Duration.zero),
        },
      );
      return Identity.of({ resolve: (did) => Cache.get(cache, did) });
    }),
  );
}
```

- [ ] **Step 4: Run the test**

Run: `pnpm --filter internal exec vitest run src/spaceSync/Identity.test.ts`
Expected: 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add typescript/internal/src/spaceSync/Identity.ts typescript/internal/src/spaceSync/Identity.test.ts
git commit -m "spaceSync: add Identity service

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Fake network test harness (PLC + space host + repo hosts on msw)

**Files:**
- Create: `typescript/internal/src/spaceSync/test/fakeNetwork.ts`
- Test: `typescript/internal/src/spaceSync/fakeNetwork.test.ts`

**Interfaces:**
- Consumes: `@atproto/space` provider helpers (real crypto).
- Produces (used by every later test):
  - `PLC_URL = "https://plc.test"`, `SPACE_TYPE = "com.example.board"`
  - `interface FakeAccount { did; keypair: Secp256k1Keypair; pds }`
  - `class FakeSpace`: `ref`, `authority`, `write(author, collection, rkey, record | null) → Promise<{ repoRev; spaceRev }>`, `truncateOplog(did)`, `revokeAllCredentials()`, `expectedView(did) → Map<path, cidString>`, `notifyWriteBody(did) → JSON`, `repoRevOf(did)`, `spaceRevOf(did)`, knobs: `deleted`, `delisted: Set`, `failingRepos: Set`, `corruptOpsCommits: Set`, `deniedUsers: Set`, `unorderedListRepos`, `maxOpsPage`, `listReposPageSize`, `onListRepos` (async hook run after each page is computed), `registrationLifetimeMs`, `credentialLifetimeSec`; counters: `registrations: string[]`, `credentialJtis: string[]`, `listReposCalls`, `getRepoCalls`, `listRepoOpsCalls`
  - `class FakeNetwork`: `createAccount(name)`, `createSpace(authority, skey?)`, `delegationToken(user, space)`, `delegationTokenFor(space)`, `serviceAuth(account, { aud, lxm, expSec? })`, `handlers: HttpHandler[]`

- [ ] **Step 1: Write the self-test (fails until the fake exists)**

```ts
// @vitest-environment node
import { P256Keypair } from "@atproto/crypto";
import { createSpaceSigHeaders, verifyRepoCarFull } from "@atproto/space";
import type { DidString } from "@atproto/syntax";
import { describe, expect, it } from "vitest";
import { server } from "../test/msw";
import { FakeNetwork } from "./test/fakeNetwork";

describe("FakeNetwork", () => {
  it("issues a credential and serves a verifiable repo CAR", async () => {
    const net = new FakeNetwork();
    server.use(...net.handlers);
    const alice = await net.createAccount("alice");
    const bob = await net.createAccount("bob");
    const space = net.createSpace(alice);
    await space.write(bob, "com.example.post", "a", { text: "hi" });
    await space.write(bob, "com.example.post", "b", { text: "yo" });
    await space.write(bob, "com.example.post", "a", null);

    const key = await P256Keypair.create();
    const delegation = await net.delegationToken(alice, space.ref);
    const credRes = await fetch(`${alice.pds}/xrpc/com.atproto.space.getSpaceCredential`, {
      method: "POST",
      headers: {
        ...(await createSpaceSigHeaders(key, { authorization: `Bearer ${delegation}` })),
        "content-type": "application/json",
      },
      body: JSON.stringify({ space: space.ref }),
    });
    expect(credRes.status).toBe(200);
    const { credential } = (await credRes.json()) as { credential: string };

    const url = new URL(`${bob.pds}/xrpc/com.atproto.space.getRepo`);
    url.searchParams.set("space", space.ref);
    url.searchParams.set("repo", bob.did);
    const repoRes = await fetch(url, {
      headers: await createSpaceSigHeaders(key, {
        authorization: `Atproto-Space ${credential}`,
        audience: bob.did as DidString,
      }),
    });
    expect(repoRes.status).toBe(200);
    const repo = await verifyRepoCarFull([new Uint8Array(await repoRes.arrayBuffer())], {
      space: space.ref,
      author: bob.did,
      didKey: bob.keypair.did(),
    });
    expect(repo.records.map((r) => r.rkey)).toEqual(["b"]);
    expect(repo.commit.rev).toBe(space.repoRevOf(bob.did));
  });

  it("rejects repo calls signed for the wrong audience", async () => {
    const net = new FakeNetwork();
    server.use(...net.handlers);
    const alice = await net.createAccount("alice");
    const space = net.createSpace(alice);
    await space.write(alice, "com.example.post", "a", { text: "hi" });
    const key = await P256Keypair.create();
    const delegation = await net.delegationToken(alice, space.ref);
    const { credential } = (await (
      await fetch(`${alice.pds}/xrpc/com.atproto.space.getSpaceCredential`, {
        method: "POST",
        headers: { ...(await createSpaceSigHeaders(key, { authorization: `Bearer ${delegation}` })), "content-type": "application/json" },
        body: JSON.stringify({ space: space.ref }),
      })
    ).json()) as { credential: string };
    const url = new URL(`${alice.pds}/xrpc/com.atproto.space.listRepoOps`);
    url.searchParams.set("space", space.ref);
    url.searchParams.set("repo", alice.did);
    const res = await fetch(url, {
      headers: await createSpaceSigHeaders(key, {
        authorization: `Atproto-Space ${credential}`,
        audience: "did:plc:someoneelseaaaaaaaaaaaaa" as DidString,
      }),
    });
    expect(res.status).toBe(401);
  });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `pnpm --filter internal exec vitest run src/spaceSync/fakeNetwork.test.ts`
Expected: FAIL. Cannot resolve `./test/fakeNetwork`.

- [ ] **Step 3: Write `test/fakeNetwork.ts`**

```ts
import { TID } from "@atproto/common-web";
import { Secp256k1Keypair } from "@atproto/crypto";
import { type LexMap, type LexValue, lexToJson, toBase64 } from "@atproto/lex";
import {
  RepoCommit,
  type SerializedRecord,
  type SignedCommit,
  createSpaceToken,
  parseSpaceToken,
  serializeRecord,
  serializeRepo,
  spaceHostAud,
  verifySpaceSignature,
  verifySpaceToken,
} from "@atproto/space";
import type { DidString, NsidString, RecordKeyString } from "@atproto/syntax";
import { type HttpHandler, HttpResponse, http } from "msw";

// In-memory stand-in for a PLC directory, a space host and repo hosts. Every
// signature, credential and CAR uses the real @atproto/space code, so tests
// exercise real verification.

export const PLC_URL = "https://plc.test";
export const SPACE_TYPE = "com.example.board";

export interface FakeAccount {
  readonly did: string;
  readonly keypair: Secp256k1Keypair;
  readonly pds: string;
}

interface StoredRecord extends SerializedRecord {
  readonly record: LexMap;
}

interface OpLogEntry {
  readonly rev: string;
  readonly collection: string;
  readonly rkey: string;
  readonly cid: string | null;
  readonly prev: string | null;
}

class FakeRepo {
  readonly records = new Map<string, StoredRecord>();
  oplog: OpLogEntry[] = [];
  /** listRepoOps rejects a `since` older than this (simulates compaction). */
  oplogFloor = "";
  rev = "";
  spaceRev = "";
  readonly commit = new RepoCommit();
}

const xrpcError = (status: number, error: string, message = error) =>
  HttpResponse.json({ error, message }, { status });

const json = (value: unknown) => HttpResponse.json(lexToJson(value as LexValue) as never);

export class FakeSpace {
  readonly ref: string;
  readonly repos = new Map<string, FakeRepo>();
  readonly delisted = new Set<string>();
  readonly failingRepos = new Set<string>();
  readonly corruptOpsCommits = new Set<string>();
  readonly deniedUsers = new Set<string>();
  readonly registrations: string[] = [];
  readonly credentialJtis: string[] = [];
  readonly revokedJtis = new Set<string>();
  deleted = false;
  unorderedListRepos = false;
  maxOpsPage = 1000;
  listReposPageSize = 1000;
  /** Runs after each listRepos page is computed, before it is returned (simulates writes mid-walk). */
  onListRepos: (() => Promise<void>) | undefined = undefined;
  registrationLifetimeMs = 6 * 60 * 60 * 1000;
  credentialLifetimeSec = 600;
  listReposCalls = 0;
  listRepoOpsCalls = 0;
  getRepoCalls = 0;

  constructor(
    readonly authority: FakeAccount,
    readonly skey: string,
  ) {
    this.ref = `at://${authority.did}/space/${SPACE_TYPE}/${skey}`;
  }

  async write(author: FakeAccount, collection: string, rkey: string, record: LexMap | null) {
    let repo = this.repos.get(author.did);
    if (!repo) {
      repo = new FakeRepo();
      this.repos.set(author.did, repo);
    }
    const path = `${collection}/${rkey}`;
    const prev = repo.records.get(path);
    if (prev) repo.commit.remove(prev.collection, prev.rkey, prev.cid);
    let cid: string | null = null;
    if (record) {
      const serialized = await serializeRecord(collection as NsidString, rkey as RecordKeyString, record);
      repo.records.set(path, { ...serialized, record });
      repo.commit.add(serialized.collection, serialized.rkey, serialized.cid);
      cid = serialized.cid.toString();
    } else {
      repo.records.delete(path);
    }
    repo.rev = TID.nextStr(repo.rev || undefined);
    repo.oplog.push({ rev: repo.rev, collection, rkey, cid, prev: prev?.cid.toString() ?? null });
    repo.spaceRev = TID.nextStr(repo.rev);
    return { repoRev: repo.rev, spaceRev: repo.spaceRev };
  }

  truncateOplog(did: string): void {
    const repo = this.repos.get(did)!;
    repo.oplog = [];
    repo.oplogFloor = repo.rev;
  }

  revokeAllCredentials(): void {
    for (const jti of this.credentialJtis) this.revokedJtis.add(jti);
  }

  repoRevOf(did: string): string {
    return this.repos.get(did)!.rev;
  }

  spaceRevOf(did: string): string {
    return this.repos.get(did)!.spaceRev;
  }

  /** path → cid string, i.e. what a correct sink should hold for this repo. */
  expectedView(did: string): Map<string, string> {
    const repo = this.repos.get(did);
    return new Map([...(repo?.records ?? new Map()).entries()].map(([path, r]) => [path, r.cid.toString()]));
  }

  /** JSON body of the notifyWrite a space host would forward for this repo's latest write. */
  notifyWriteBody(did: string, prevSpaceRev?: string): unknown {
    const repo = this.repos.get(did)!;
    return lexToJson({
      space: this.ref,
      repo: did,
      repoRev: repo.rev,
      hash: repo.commit.setHash.digest(),
      spaceRev: repo.spaceRev,
      ...(prevSpaceRev ? { prevSpaceRev } : {}),
    } as LexValue);
  }
}

export class FakeNetwork {
  readonly accounts = new Map<string, FakeAccount>();
  readonly spaces = new Map<string, FakeSpace>();

  async createAccount(name: string): Promise<FakeAccount> {
    const keypair = await Secp256k1Keypair.create();
    const id = (name.toLowerCase().replace(/[^a-z]/g, "") + "a".repeat(24)).slice(0, 24);
    const account = { did: `did:plc:${id}`, keypair, pds: `https://${name.toLowerCase()}.pds.test` };
    this.accounts.set(account.did, account);
    return account;
  }

  createSpace(authority: FakeAccount, skey = "main"): FakeSpace {
    const space = new FakeSpace(authority, skey);
    this.spaces.set(space.ref, space);
    return space;
  }

  /** What getDelegationToken on `user`'s PDS would return. */
  delegationToken(user: FakeAccount, space: string): Promise<string> {
    const authority = space.split("/")[2];
    return createSpaceToken("delegation", { iss: user.did, sub: space, aud: spaceHostAud(authority) }, user.keypair);
  }

  /** Delegation from the space authority's own session. */
  delegationTokenFor(space: string): Promise<string> {
    const fake = this.spaces.get(space);
    if (!fake) return Promise.reject(new Error(`no session can reach ${space}`));
    return this.delegationToken(fake.authority, space);
  }

  /** A service-auth JWT as a space authority would send with notifyWrite. */
  async serviceAuth(
    issuer: FakeAccount,
    opts: { aud: string; lxm: string; expSec?: number; signer?: Secp256k1Keypair },
  ): Promise<string> {
    const enc = (v: unknown) => toBase64(new TextEncoder().encode(JSON.stringify(v)), "base64url");
    const now = Math.floor(Date.now() / 1000);
    const signer = opts.signer ?? issuer.keypair;
    const head = enc({ alg: signer.jwtAlg, typ: "JWT" });
    const body = enc({ iss: issuer.did, aud: opts.aud, lxm: opts.lxm, iat: now, exp: now + (opts.expSec ?? 60), jti: TID.nextStr() });
    const sig = await signer.sign(new TextEncoder().encode(`${head}.${body}`));
    return `${head}.${body}.${toBase64(sig, "base64url")}`;
  }

  private didDoc(account: FakeAccount) {
    return {
      "@context": ["https://www.w3.org/ns/did/v1"],
      id: account.did,
      alsoKnownAs: [],
      verificationMethod: [
        {
          id: `${account.did}#atproto`,
          type: "Multikey",
          controller: account.did,
          publicKeyMultibase: account.keypair.did().slice("did:key:".length),
        },
      ],
      service: [{ id: "#atproto_pds", type: "AtprotoPersonalDataServer", serviceEndpoint: account.pds }],
    };
  }

  private async signCommit(space: FakeSpace, did: string, opts: { corrupt: boolean }): Promise<SignedCommit> {
    const repo = space.repos.get(did)!;
    const author = this.accounts.get(did)!;
    // A corrupt commit signs a different rev into the ctx than the one it reports.
    const ctxRev = opts.corrupt ? TID.nextStr(repo.rev) : repo.rev;
    const commit = await repo.commit.sign({ space: space.ref, author: did, rev: ctxRev }, author.keypair);
    return { ...commit, rev: repo.rev };
  }

  /** Verifies `Atproto-Space` credential + HTTP signature. Returns an error response, or undefined if authorized. */
  private async authorize(request: Request, space: FakeSpace, audience: string): Promise<Response | undefined> {
    const headers = Object.fromEntries(request.headers);
    const token = headers.authorization?.match(/^Atproto-Space (.+)$/)?.[1];
    if (!token) return xrpcError(401, "AuthMissing");
    if (headers["atproto-space-audience"] !== audience) return xrpcError(401, "BadAudience");
    let jti: string;
    let kid: DidString;
    try {
      const parsed = await verifySpaceToken("credential", token, {
        getSigningKey: () => space.authority.keypair.did(),
        sub: space.ref,
      });
      jti = parsed.payload.jti;
      kid = parsed.payload.cnf!.kid;
    } catch (error) {
      const code = (error as { code?: string }).code;
      return xrpcError(401, code === "JwtExpired" ? "JwtExpired" : "InvalidToken");
    }
    if (space.revokedJtis.has(jti)) return xrpcError(401, "CredentialRevoked");
    try {
      await verifySpaceSignature(headers, kid);
    } catch {
      return xrpcError(401, "BadSignature");
    }
    return undefined;
  }

  private spaceFor(param: string | null): FakeSpace | Response {
    const space = this.spaces.get(param ?? "");
    if (!space) return xrpcError(400, "SpaceNotFound");
    if (space.deleted) return xrpcError(400, "SpaceDeleted");
    return space;
  }

  get handlers(): HttpHandler[] {
    return [
      http.get(`${PLC_URL}/:did`, ({ params }) => {
        const account = this.accounts.get(decodeURIComponent(String(params.did)));
        return account ? HttpResponse.json(this.didDoc(account)) : new HttpResponse(null, { status: 404 });
      }),

      http.post("*/xrpc/com.atproto.space.getSpaceCredential", async ({ request }) => {
        const body = (await request.json()) as { space?: string };
        const space = this.spaceFor(body.space ?? null);
        if (space instanceof Response) return space;
        if (new URL(request.url).origin !== space.authority.pds) return xrpcError(400, "WrongHost");
        const headers = Object.fromEntries(request.headers);
        const delegation = headers.authorization?.match(/^Bearer (.+)$/)?.[1];
        if (!delegation) return xrpcError(401, "InvalidDelegationToken");
        let keyId: DidString;
        try {
          const token = await verifySpaceToken("delegation", delegation, {
            getSigningKey: (iss) => this.accounts.get(iss)!.keypair.did(),
            aud: spaceHostAud(space.authority.did),
            sub: space.ref,
          });
          if (space.deniedUsers.has(token.payload.iss)) return xrpcError(403, "UserNotAuthorized");
          keyId = await verifySpaceSignature(headers);
        } catch {
          return xrpcError(400, "InvalidDelegationToken");
        }
        const credential = await createSpaceToken(
          "credential",
          { iss: space.authority.did, sub: space.ref, keyId, expiresInSec: space.credentialLifetimeSec },
          space.authority.keypair,
        );
        space.credentialJtis.push(parseSpaceToken("credential", credential).payload.jti);
        return HttpResponse.json({ credential });
      }),

      http.get("*/xrpc/com.atproto.space.listRepos", async ({ request }) => {
        const url = new URL(request.url);
        const space = this.spaceFor(url.searchParams.get("space"));
        if (space instanceof Response) return space;
        space.listReposCalls++;
        const denied = await this.authorize(request, space, space.authority.did);
        if (denied) return denied;
        const cursor = url.searchParams.get("cursor") ?? "";
        const limit = Math.min(Number(url.searchParams.get("limit") ?? 100), space.listReposPageSize);
        const rows = [...space.repos.entries()]
          .filter(([did, repo]) => !space.delisted.has(did) && repo.spaceRev > cursor)
          .sort((a, b) => (a[1].spaceRev < b[1].spaceRev ? -1 : 1))
          .slice(0, limit);
        if (space.unorderedListRepos) rows.reverse();
        if (rows.length === 0) return HttpResponse.json({ repos: [] });
        const response = {
          repos: rows.map(([did, repo]) => ({ did, repoRev: repo.rev, spaceRev: repo.spaceRev, hash: repo.commit.setHash.digest() })),
          cursor: rows.at(-1)![1].spaceRev,
        };
        await space.onListRepos?.();
        return json(response);
      }),

      http.post("*/xrpc/com.atproto.space.registerNotify", async ({ request }) => {
        const body = (await request.json()) as { space?: string; service?: string };
        const space = this.spaceFor(body.space ?? null);
        if (space instanceof Response) return space;
        const denied = await this.authorize(request, space, space.authority.did);
        if (denied) return denied;
        space.registrations.push(body.service ?? "");
        return HttpResponse.json({ expiresAt: new Date(Date.now() + space.registrationLifetimeMs).toISOString() });
      }),

      http.get("*/xrpc/com.atproto.space.listRepoOps", async ({ request }) => {
        const url = new URL(request.url);
        const space = this.spaceFor(url.searchParams.get("space"));
        if (space instanceof Response) return space;
        space.listRepoOpsCalls++;
        const did = url.searchParams.get("repo") ?? "";
        const repo = space.repos.get(did);
        if (!repo) return xrpcError(400, "RepoNotFound");
        if (url.origin !== this.accounts.get(did)?.pds) return xrpcError(400, "WrongHost");
        const denied = await this.authorize(request, space, did);
        if (denied) return denied;
        if (space.failingRepos.has(did)) return xrpcError(500, "InternalServerError");
        const since = url.searchParams.get("since") ?? "";
        if (since < repo.oplogFloor) return xrpcError(400, "InvalidRequest", "since is outside the retained oplog");
        const ops = repo.oplog.filter((op) => op.rev > since);
        const start = Number(url.searchParams.get("cursor") ?? 0);
        const limit = Math.min(Number(url.searchParams.get("limit") ?? 100), space.maxOpsPage);
        const page = ops.slice(start, start + limit);
        const last = start + limit >= ops.length;
        const commit = last ? await this.signCommit(space, did, { corrupt: space.corruptOpsCommits.has(did) }) : undefined;
        return json({
          ops: page.map((op) => {
            const current = repo.records.get(`${op.collection}/${op.rkey}`);
            // Only the current value for a path is inlined; stale ones are omitted.
            const value = op.cid && current?.cid.toString() === op.cid ? current.record : undefined;
            return { ...op, ...(value ? { value } : {}) };
          }),
          ...(commit ? { commit } : {}),
          ...(last ? {} : { cursor: String(start + limit) }),
        });
      }),

      http.get("*/xrpc/com.atproto.space.getRepo", async ({ request }) => {
        const url = new URL(request.url);
        const space = this.spaceFor(url.searchParams.get("space"));
        if (space instanceof Response) return space;
        space.getRepoCalls++;
        const did = url.searchParams.get("repo") ?? "";
        const repo = space.repos.get(did);
        if (!repo) return xrpcError(400, "RepoNotFound");
        if (url.origin !== this.accounts.get(did)?.pds) return xrpcError(400, "WrongHost");
        const denied = await this.authorize(request, space, did);
        if (denied) return denied;
        if (space.failingRepos.has(did)) return xrpcError(500, "InternalServerError");
        const commit = await this.signCommit(space, did, { corrupt: false });
        const car = serializeRepo(commit, repo.records.values());
        return new HttpResponse(ReadableStream.from(car), { headers: { "content-type": "application/vnd.ipld.car" } });
      }),
    ];
  }
}
```

- [ ] **Step 4: Run the self-test**

Run: `pnpm --filter internal exec vitest run src/spaceSync/fakeNetwork.test.ts`
Expected: 2 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add typescript/internal/src/spaceSync/test/fakeNetwork.ts typescript/internal/src/spaceSync/fakeNetwork.test.ts
git commit -m "spaceSync: add msw-backed fake space network for tests

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: DelegationSource port and Credentials service

**Files:**
- Create: `typescript/internal/src/spaceSync/Credentials.ts`
- Create: `typescript/internal/src/spaceSync/test/harness.ts` (first part: `testConfig`, `fakeDelegation`)
- Test: `typescript/internal/src/spaceSync/Credentials.test.ts`

**Interfaces:**
- Consumes: `Identity`, `SpaceSyncConfig`, `parseSpaceRef`, `GetSpaceCredentialOutput`, `CredentialError`.
- Produces:
  - `interface SpaceCredential { space; token; key: P256Keypair; expiresAt: number }`
  - `class DelegationSource` service: `issue(space) → Effect<string, CredentialError>`, optional `attestation?(space, aud) → Effect<string, CredentialError>`
  - `class Credentials` service: `get(space) → Effect<SpaceCredential, CredentialError>`, `invalidate(space) → Effect<void>`, `static layer: Layer<Credentials, never, Identity | DelegationSource>`
  - `harness.ts`: `testConfig(overrides?) → Layer`, `fakeDelegation(net) → Layer<DelegationSource>`

- [ ] **Step 1: Write the harness helpers**

`test/harness.ts`:

```ts
import { Duration, Effect, Layer } from "effect";
import { type SpaceSyncOptions, spaceSyncConfigLayer } from "../config";
import { DelegationSource } from "../Credentials";
import { CredentialError, errorMessage } from "../errors";
import { type FakeNetwork, PLC_URL } from "./fakeNetwork";

export const SERVICE_DID = "did:web:syncer.test";

/** Short intervals so live tests settle in milliseconds. */
export const testConfig = (overrides: Partial<SpaceSyncOptions> = {}) =>
  spaceSyncConfigLayer({
    serviceDid: SERVICE_DID,
    plcUrl: PLC_URL,
    requestRetryBase: Duration.millis(5),
    backoffBase: Duration.millis(50),
    backoffCap: Duration.millis(200),
    schedulerInterval: Duration.millis(20),
    ...overrides,
  });

export const fakeDelegation = (net: FakeNetwork) =>
  Layer.succeed(
    DelegationSource,
    DelegationSource.of({
      issue: (space) =>
        Effect.tryPromise({
          try: () => net.delegationTokenFor(space),
          catch: (error) => new CredentialError({ space, reason: "NoDelegation", message: errorMessage(error) }),
        }),
    }),
  );
```

- [ ] **Step 2: Write the failing test**

```ts
// @vitest-environment node
import { it } from "@effect/vitest";
import { Effect, Exit, Layer } from "effect";
import { TestClock } from "effect/testing";
import { describe, expect } from "vitest";
import { server } from "../test/msw";
import { Credentials } from "./Credentials";
import { Identity } from "./Identity";
import { FakeNetwork } from "./test/fakeNetwork";
import { fakeDelegation, testConfig } from "./test/harness";

const setup = Effect.promise(async () => {
  const net = new FakeNetwork();
  server.use(...net.handlers);
  const alice = await net.createAccount("alice");
  const space = net.createSpace(alice);
  const layer = Credentials.layer.pipe(
    Layer.provide(Identity.layer),
    Layer.provide(fakeDelegation(net)),
    Layer.provide(testConfig()),
  );
  return { net, alice, space, layer };
});

describe("Credentials", () => {
  it.effect("mints once and reuses the cached credential", () =>
    Effect.gen(function* () {
      const { space, layer } = yield* setup;
      yield* Effect.gen(function* () {
        yield* TestClock.setTime(Date.now());
        const credentials = yield* Credentials;
        const a = yield* credentials.get(space.ref);
        const b = yield* credentials.get(space.ref);
        expect(a.token).toBe(b.token);
        expect(space.credentialJtis).toHaveLength(1);
      }).pipe(Effect.provide(layer));
    }),
  );

  it.effect("re-mints once the credential is within the refresh lead of expiry", () =>
    Effect.gen(function* () {
      const { space, layer } = yield* setup;
      yield* Effect.gen(function* () {
        yield* TestClock.setTime(Date.now());
        const credentials = yield* Credentials;
        yield* credentials.get(space.ref);
        yield* TestClock.adjust("10 minutes");
        yield* credentials.get(space.ref);
        expect(space.credentialJtis).toHaveLength(2);
      }).pipe(Effect.provide(layer));
    }),
  );

  it.effect("re-mints after invalidate", () =>
    Effect.gen(function* () {
      const { space, layer } = yield* setup;
      yield* Effect.gen(function* () {
        yield* TestClock.setTime(Date.now());
        const credentials = yield* Credentials;
        yield* credentials.get(space.ref);
        yield* credentials.invalidate(space.ref);
        yield* credentials.get(space.ref);
        expect(space.credentialJtis).toHaveLength(2);
      }).pipe(Effect.provide(layer));
    }),
  );

  it.effect("maps SpaceDeleted and UserNotAuthorized to typed reasons", () =>
    Effect.gen(function* () {
      const { alice, space, layer } = yield* setup;
      yield* Effect.gen(function* () {
        const credentials = yield* Credentials;
        space.deniedUsers.add(alice.did);
        const denied = yield* Effect.flip(credentials.get(space.ref));
        expect(denied.reason).toBe("NotAuthorized");
        space.deniedUsers.clear();
        space.deleted = true;
        const deleted = yield* Effect.flip(credentials.get(space.ref));
        expect(deleted.reason).toBe("SpaceDeleted");
      }).pipe(Effect.provide(layer));
    }),
  );

  it.effect("does not cache failures", () =>
    Effect.gen(function* () {
      const { alice, space, layer } = yield* setup;
      yield* Effect.gen(function* () {
        const credentials = yield* Credentials;
        space.deniedUsers.add(alice.did);
        expect(Exit.isFailure(yield* Effect.exit(credentials.get(space.ref)))).toBe(true);
        space.deniedUsers.clear();
        expect(Exit.isSuccess(yield* Effect.exit(credentials.get(space.ref)))).toBe(true);
      }).pipe(Effect.provide(layer));
    }),
  );
});
```

- [ ] **Step 3: Run it to see it fail**

Run: `pnpm --filter internal exec vitest run src/spaceSync/Credentials.test.ts`
Expected: FAIL. Cannot resolve `./Credentials`.

- [ ] **Step 4: Write `Credentials.ts`**

> **Superseded (2026-10-08, after #1140):** the code below predates the switch to the `api` package. The source in `typescript/internal/src/spaceSync/Credentials.ts` is authoritative; see the spec's Planning revision #1.

```ts
import { P256Keypair } from "@atproto/crypto";
import { createSpaceSigHeaders, parseSpaceToken, spaceHostAud } from "@atproto/space";
import { Cache, Clock, Context, Duration, Effect, Exit, Layer, Predicate, Schema } from "effect";
import { SpaceSyncConfig } from "./config";
import { CredentialError, errorMessage } from "./errors";
import { Identity } from "./Identity";
import { GetSpaceCredentialOutput, parseSpaceRef } from "./wire";

export interface SpaceCredential {
  readonly space: string;
  readonly token: string;
  /** Fresh P-256 key the credential is bound to (`cnf.kid`). */
  readonly key: P256Keypair;
  /** Epoch ms. */
  readonly expiresAt: number;
}

/**
 * Host-provided: obtains a delegation token for a space from some user's PDS
 * (com.atproto.space.getDelegationToken). The host chooses which OAuth session to
 * use, e.g. the space authority first, then other members.
 */
export class DelegationSource extends Context.Service<
  DelegationSource,
  {
    readonly issue: (space: string) => Effect.Effect<string, CredentialError>;
    /** Client attestation JWT for spaces that gate on app identity. */
    readonly attestation?: (space: string, aud: string) => Effect.Effect<string, CredentialError>;
  }
>()("internal/spaceSync/DelegationSource") {}

const REASONS: Record<string, CredentialError["reason"]> = {
  SpaceDeleted: "SpaceDeleted",
  SpaceNotFound: "SpaceNotFound",
  UserNotAuthorized: "NotAuthorized",
  AppNotAuthorized: "NotAuthorized",
  NotAuthorized: "NotAuthorized",
  InvalidClientAttestation: "NotAuthorized",
  InvalidDelegationToken: "NoDelegation",
};

const credentialFailure = (space: string, status: number, body: unknown): CredentialError => {
  const code = Predicate.hasProperty(body, "error") && Predicate.isString(body.error) ? body.error : undefined;
  const message = Predicate.hasProperty(body, "message") && Predicate.isString(body.message) ? body.message : `HTTP ${status}`;
  const reason = (code && REASONS[code]) ?? (status >= 500 ? "Transport" : "NotAuthorized");
  return new CredentialError({ space, reason, message: code ? `${code}: ${message}` : message });
};

export class Credentials extends Context.Service<
  Credentials,
  {
    readonly get: (space: string) => Effect.Effect<SpaceCredential, CredentialError>;
    readonly invalidate: (space: string) => Effect.Effect<void>;
  }
>()("internal/spaceSync/Credentials") {
  static readonly layer = Layer.effect(
    Credentials,
    Effect.gen(function* () {
      const config = yield* SpaceSyncConfig;
      const identity = yield* Identity;
      const delegation = yield* DelegationSource;
      const refreshLeadMs = Duration.toMillis(config.credentialRefreshLead);

      const mint = Effect.fn("Credentials.mint")(function* (space: string) {
        const { authority } = yield* parseSpaceRef(space).pipe(
          Effect.mapError(() => new CredentialError({ space, reason: "SpaceNotFound", message: "invalid space ref" })),
        );
        const host = yield* identity.resolve(authority).pipe(
          Effect.mapError((e) => new CredentialError({ space, reason: "Transport", message: e.message })),
        );
        const token = yield* delegation.issue(space);
        const attestation = delegation.attestation
          ? yield* delegation.attestation(space, spaceHostAud(authority))
          : undefined;
        const response = yield* Effect.tryPromise({
          try: async (signal) => {
            // A new keypair per credential, per the proposal.
            const key = await P256Keypair.create();
            const headers = await createSpaceSigHeaders(key, { authorization: `Bearer ${token}` });
            const res = await fetch(new URL("/xrpc/com.atproto.space.getSpaceCredential", host.spaceHost), {
              method: "POST",
              redirect: "error",
              signal,
              headers: { ...headers, "content-type": "application/json", accept: "application/json" },
              body: JSON.stringify({ space, ...(attestation ? { clientAttestation: attestation } : {}) }),
            });
            return { key, status: res.status, body: (await res.json().catch(() => undefined)) as unknown };
          },
          catch: (error) => new CredentialError({ space, reason: "Transport", message: errorMessage(error) }),
        });
        if (response.status !== 200) return yield* credentialFailure(space, response.status, response.body);
        const { credential } = yield* Schema.decodeUnknownEffect(GetSpaceCredentialOutput)(response.body).pipe(
          Effect.mapError((e) => new CredentialError({ space, reason: "Transport", message: e.message })),
        );
        const exp = yield* Effect.try({
          try: () => parseSpaceToken("credential", credential).payload.exp,
          catch: (error) => new CredentialError({ space, reason: "Transport", message: errorMessage(error) }),
        });
        const now = yield* Clock.currentTimeMillis;
        const value: SpaceCredential = { space, token: credential, key: response.key, expiresAt: exp * 1000 };
        return { value, ttlMs: Math.max(0, value.expiresAt - now - refreshLeadMs) };
      });

      const cache = yield* Cache.makeWith(mint, {
        capacity: config.credentialCacheCapacity,
        timeToLive: (exit) => (Exit.isSuccess(exit) ? Duration.millis(exit.value.ttlMs) : Duration.zero),
      });

      return Credentials.of({
        get: (space) => Cache.get(cache, space).pipe(Effect.map((entry) => entry.value)),
        invalidate: (space) => Cache.invalidate(cache, space),
      });
    }),
  );
}
```

- [ ] **Step 5: Run the test**

Run: `pnpm --filter internal exec vitest run src/spaceSync/Credentials.test.ts`
Expected: 5 tests PASS. If `Cache.invalidate`'s return type isn't `Effect<void>`, add `Effect.asVoid`.

- [ ] **Step 6: Commit**

```bash
git add typescript/internal/src/spaceSync/Credentials.ts typescript/internal/src/spaceSync/Credentials.test.ts typescript/internal/src/spaceSync/test/harness.ts
git commit -m "spaceSync: add DelegationSource port and Credentials cache

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: SpaceClient (signed XRPC with retries and credential refresh)

**Files:**
- Create: `typescript/internal/src/spaceSync/SpaceClient.ts`
- Test: `typescript/internal/src/spaceSync/SpaceClient.test.ts`

**Interfaces:**
- Consumes: `Credentials`, `Identity`, `SpaceSyncConfig`, wire schemas, `XrpcError`, `CredentialError`.
- Produces: `type SpaceCallError = XrpcError | CredentialError`. `class SpaceClient` with:
  - `listRepos(space, cursor?: string) → Effect<typeof ListReposOutput.Type, SpaceCallError>`
  - `registerNotify(space, service) → Effect<{ expiresAt: number }, SpaceCallError>`
  - `listRepoOps(space, repo, since: string | undefined, cursor?: string) → Effect<typeof ListRepoOpsOutput.Type, SpaceCallError>`
  - `getRepo(space, repo) → Effect<AsyncIterable<Uint8Array>, SpaceCallError, Scope.Scope>` (the body is cancelled when the scope closes)
  - `getBlob(space, repo, cid) → Effect<Uint8Array, SpaceCallError>`
  - `static layer: Layer<SpaceClient, never, Credentials | Identity>`
- Any `SpaceDeleted` error from any host is surfaced as `CredentialError { reason: "SpaceDeleted" }`.

- [ ] **Step 1: Write the failing test**

```ts
// @vitest-environment node
import { it } from "@effect/vitest";
import { Effect, Layer } from "effect";
import { describe, expect } from "vitest";
import { server } from "../test/msw";
import { Credentials } from "./Credentials";
import { Identity } from "./Identity";
import { SpaceClient } from "./SpaceClient";
import { FakeNetwork } from "./test/fakeNetwork";
import { fakeDelegation, testConfig } from "./test/harness";

const setup = Effect.promise(async () => {
  const net = new FakeNetwork();
  server.use(...net.handlers);
  const alice = await net.createAccount("alice");
  const bob = await net.createAccount("bob");
  const space = net.createSpace(alice);
  const layer = SpaceClient.layer.pipe(
    Layer.provideMerge(Credentials.layer),
    Layer.provideMerge(Identity.layer),
    Layer.provide(fakeDelegation(net)),
    Layer.provide(testConfig()),
  );
  return { net, alice, bob, space, layer };
});

describe("SpaceClient", () => {
  it.live("lists writers in spaceRev order with a matching cursor", () =>
    Effect.gen(function* () {
      const { alice, bob, space, layer } = yield* setup;
      yield* Effect.promise(() => space.write(bob, "com.example.post", "1", { text: "b" }));
      yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
      const page = yield* Effect.flatMap(SpaceClient, (c) => c.listRepos(space.ref)).pipe(Effect.provide(layer));
      expect(page.repos.map((r) => r.did)).toEqual([bob.did, alice.did]);
      expect(page.cursor).toBe(space.spaceRevOf(alice.did));
    }),
  );

  it.live("pages listRepoOps and returns the commit on the last page", () =>
    Effect.gen(function* () {
      const { bob, space, layer } = yield* setup;
      for (const rkey of ["1", "2", "3"]) {
        yield* Effect.promise(() => space.write(bob, "com.example.post", rkey, { text: rkey }));
      }
      space.maxOpsPage = 2;
      yield* Effect.gen(function* () {
        const client = yield* SpaceClient;
        const first = yield* client.listRepoOps(space.ref, bob.did, undefined);
        expect(first.ops).toHaveLength(2);
        expect(first.commit).toBeUndefined();
        const second = yield* client.listRepoOps(space.ref, bob.did, undefined, first.cursor);
        expect(second.ops).toHaveLength(1);
        expect(second.commit?.rev).toBe(space.repoRevOf(bob.did));
      }).pipe(Effect.provide(layer));
    }),
  );

  it.live("re-mints and retries once when a host reports CredentialRevoked", () =>
    Effect.gen(function* () {
      const { bob, space, layer } = yield* setup;
      yield* Effect.promise(() => space.write(bob, "com.example.post", "1", { text: "x" }));
      yield* Effect.gen(function* () {
        const client = yield* SpaceClient;
        yield* client.listRepos(space.ref);
        space.revokeAllCredentials();
        yield* client.listRepoOps(space.ref, bob.did, undefined);
        expect(space.credentialJtis).toHaveLength(2);
      }).pipe(Effect.provide(layer));
    }),
  );

  it.live("surfaces SpaceDeleted as a CredentialError", () =>
    Effect.gen(function* () {
      const { space, layer } = yield* setup;
      space.deleted = true;
      const error = yield* Effect.flip(Effect.flatMap(SpaceClient, (c) => c.listRepos(space.ref))).pipe(Effect.provide(layer));
      expect(error._tag).toBe("CredentialError");
      expect(error._tag === "CredentialError" && error.reason).toBe("SpaceDeleted");
    }),
  );

  it.live("retries 5xx responses then fails with the last XrpcError", () =>
    Effect.gen(function* () {
      const { bob, space, layer } = yield* setup;
      yield* Effect.promise(() => space.write(bob, "com.example.post", "1", { text: "x" }));
      space.failingRepos.add(bob.did);
      const error = yield* Effect.flip(Effect.flatMap(SpaceClient, (c) => c.listRepoOps(space.ref, bob.did, undefined))).pipe(
        Effect.provide(layer),
      );
      expect(error._tag === "XrpcError" && error.status).toBe(500);
      expect(space.listRepoOpsCalls).toBe(3);
    }),
  );

  it.live("streams getRepo bytes", () =>
    Effect.gen(function* () {
      const { bob, space, layer } = yield* setup;
      yield* Effect.promise(() => space.write(bob, "com.example.post", "1", { text: "x" }));
      const size = yield* Effect.scoped(
        Effect.gen(function* () {
          const body = yield* Effect.flatMap(SpaceClient, (c) => c.getRepo(space.ref, bob.did));
          let n = 0;
          yield* Effect.promise(async () => {
            for await (const chunk of body) n += chunk.length;
          });
          return n;
        }),
      ).pipe(Effect.provide(layer));
      expect(size).toBeGreaterThan(0);
    }),
  );
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `pnpm --filter internal exec vitest run src/spaceSync/SpaceClient.test.ts`
Expected: FAIL. Cannot resolve `./SpaceClient`.

- [ ] **Step 3: Write `SpaceClient.ts`**

> **Superseded (2026-10-08, after #1140):** the code below predates the switch to the `api` package. The source in `typescript/internal/src/spaceSync/SpaceClient.ts` is authoritative; see the spec's Planning revision #1.

```ts
import type { DidString } from "@atproto/syntax";
import { createSpaceSigHeaders } from "@atproto/space";
import { Context, Effect, Layer, Predicate, Schedule, Schema, type Scope } from "effect";
import { SpaceSyncConfig } from "./config";
import { Credentials } from "./Credentials";
import { CredentialError, XrpcError, errorMessage } from "./errors";
import { Identity } from "./Identity";
import {
  ListRepoOpsOutput,
  ListReposOutput,
  RegisterNotifyOutput,
  invalidResponse,
  lexJson,
  parseSpaceRef,
} from "./wire";

export type SpaceCallError = XrpcError | CredentialError;

interface CallInput {
  readonly space: string;
  readonly method: string;
  /** Repo DID for repo calls, authority DID for space-host calls. */
  readonly audience: string;
  /** "repo" → audience's PDS; "space" → audience's space host. */
  readonly hostOf: "repo" | "space";
  readonly params?: Record<string, string | undefined>;
  readonly body?: unknown;
}

const isTransient = (error: SpaceCallError): boolean =>
  error._tag === "XrpcError" ? error.status === 0 || error.status >= 500 : error.reason === "Transport";

export class SpaceClient extends Context.Service<
  SpaceClient,
  {
    readonly listRepos: (space: string, cursor?: string) => Effect.Effect<typeof ListReposOutput.Type, SpaceCallError>;
    readonly registerNotify: (space: string, service: string) => Effect.Effect<{ readonly expiresAt: number }, SpaceCallError>;
    readonly listRepoOps: (
      space: string,
      repo: string,
      since: string | undefined,
      cursor?: string,
    ) => Effect.Effect<typeof ListRepoOpsOutput.Type, SpaceCallError>;
    readonly getRepo: (space: string, repo: string) => Effect.Effect<AsyncIterable<Uint8Array>, SpaceCallError, Scope.Scope>;
    readonly getBlob: (space: string, repo: string, cid: string) => Effect.Effect<Uint8Array, SpaceCallError>;
  }
>()("internal/spaceSync/SpaceClient") {
  static readonly layer = Layer.effect(
    SpaceClient,
    Effect.gen(function* () {
      const config = yield* SpaceSyncConfig;
      const credentials = yield* Credentials;
      const identity = yield* Identity;

      const attempt = (input: CallInput): Effect.Effect<Response, SpaceCallError> =>
        Effect.gen(function* () {
          const resolved = yield* identity.resolve(input.audience).pipe(
            Effect.mapError((e) => new XrpcError({ method: input.method, status: 0, error: "IdentityError", message: e.message })),
          );
          const credential = yield* credentials.get(input.space);
          const response = yield* Effect.tryPromise({
            try: async (signal) => {
              const url = new URL(`/xrpc/${input.method}`, input.hostOf === "repo" ? resolved.pds : resolved.spaceHost);
              for (const [name, value] of Object.entries(input.params ?? {})) {
                if (value !== undefined) url.searchParams.set(name, value);
              }
              const headers = await createSpaceSigHeaders(credential.key, {
                authorization: `Atproto-Space ${credential.token}`,
                audience: input.audience as DidString,
              });
              return fetch(url, {
                method: input.body === undefined ? "GET" : "POST",
                redirect: "error",
                signal,
                headers: input.body === undefined ? headers : { ...headers, "content-type": "application/json" },
                body: input.body === undefined ? undefined : JSON.stringify(input.body),
              });
            },
            catch: (error) => new XrpcError({ method: input.method, status: 0, error: "Transport", message: errorMessage(error) }),
          });
          if (response.ok) return response;
          const body: unknown = yield* Effect.promise(() => response.json().catch(() => undefined));
          const error = Predicate.hasProperty(body, "error") && Predicate.isString(body.error) ? body.error : undefined;
          const message = Predicate.hasProperty(body, "message") && Predicate.isString(body.message) ? body.message : `HTTP ${response.status}`;
          if (error === "SpaceDeleted") {
            return yield* new CredentialError({ space: input.space, reason: "SpaceDeleted", message });
          }
          return yield* new XrpcError({ method: input.method, status: response.status, error, message });
        });

      const call = Effect.fnUntraced(function* (input: CallInput) {
        return yield* attempt(input).pipe(
          // A rejected credential is re-minted and the call retried once.
          Effect.catchTag("XrpcError", (e) =>
            e.error === "JwtExpired" || e.error === "CredentialRevoked"
              ? credentials.invalidate(input.space).pipe(Effect.flatMap(() => attempt(input)))
              : Effect.fail(e),
          ),
          Effect.retry({
            schedule: Schedule.exponential(config.requestRetryBase).pipe(Schedule.jittered),
            times: config.requestRetries,
            while: isTransient,
          }),
        );
      });

      const readJson = (method: string, response: Response) =>
        Effect.tryPromise({
          try: async () => lexJson(await response.json()),
          catch: (error) => new XrpcError({ method, status: response.status, error: "InvalidResponse", message: errorMessage(error) }),
        });

      const authorityOf = (space: string) =>
        parseSpaceRef(space).pipe(
          Effect.map((ref) => ref.authority),
          Effect.mapError(() => new CredentialError({ space, reason: "SpaceNotFound", message: "invalid space ref" })),
        );

      const listRepos = Effect.fnUntraced(function* (space: string, cursor?: string) {
        const method = "com.atproto.space.listRepos";
        const authority = yield* authorityOf(space);
        const res = yield* call({ space, method, audience: authority, hostOf: "space", params: { space, limit: "1000", cursor } });
        return yield* Schema.decodeUnknownEffect(ListReposOutput)(yield* readJson(method, res)).pipe(
          Effect.mapError(invalidResponse(method)),
        );
      });

      const registerNotify = Effect.fnUntraced(function* (space: string, service: string) {
        const method = "com.atproto.space.registerNotify";
        const authority = yield* authorityOf(space);
        const res = yield* call({ space, method, audience: authority, hostOf: "space", body: { space, service } });
        const out = yield* Schema.decodeUnknownEffect(RegisterNotifyOutput)(yield* readJson(method, res)).pipe(
          Effect.mapError(invalidResponse(method)),
        );
        const expiresAt = Date.parse(out.expiresAt);
        if (!Number.isFinite(expiresAt)) {
          return yield* new XrpcError({ method, status: 200, error: "InvalidResponse", message: "unparseable expiresAt" });
        }
        return { expiresAt };
      });

      const listRepoOps = Effect.fnUntraced(function* (space: string, repo: string, since: string | undefined, cursor?: string) {
        const method = "com.atproto.space.listRepoOps";
        const res = yield* call({ space, method, audience: repo, hostOf: "repo", params: { space, repo, since, cursor, limit: "1000" } });
        return yield* Schema.decodeUnknownEffect(ListRepoOpsOutput)(yield* readJson(method, res)).pipe(
          Effect.mapError(invalidResponse(method)),
        );
      });

      const getRepo = (space: string, repo: string) =>
        Effect.acquireRelease(
          call({ space, method: "com.atproto.space.getRepo", audience: repo, hostOf: "repo", params: { space, repo } }),
          // Cancels an unfinished download when the scope closes (e.g. on interruption).
          (res) => Effect.promise(() => res.body?.cancel().catch(() => undefined) ?? Promise.resolve()),
        ).pipe(
          Effect.flatMap((res) =>
            res.body
              ? Effect.succeed(res.body as AsyncIterable<Uint8Array>)
              : Effect.fail(new XrpcError({ method: "com.atproto.space.getRepo", status: res.status, error: "InvalidResponse", message: "empty body" })),
          ),
        );

      const getBlob = Effect.fnUntraced(function* (space: string, repo: string, cid: string) {
        const method = "com.atproto.space.getBlob";
        const res = yield* call({ space, method, audience: repo, hostOf: "repo", params: { space, repo, cid } });
        return yield* Effect.tryPromise({
          try: async () => new Uint8Array(await res.arrayBuffer()),
          catch: (error) => new XrpcError({ method, status: res.status, error: "InvalidResponse", message: errorMessage(error) }),
        });
      });

      return SpaceClient.of({ listRepos, registerNotify, listRepoOps, getRepo, getBlob });
    }),
  );
}
```

- [ ] **Step 4: Run the test**

Run: `pnpm --filter internal exec vitest run src/spaceSync/SpaceClient.test.ts`
Expected: 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add typescript/internal/src/spaceSync/SpaceClient.ts typescript/internal/src/spaceSync/SpaceClient.test.ts
git commit -m "spaceSync: add signed SpaceClient with retry and credential refresh

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: SyncStore and SyncSink ports, memory store, recording sink, full harness

**Files:**
- Create: `typescript/internal/src/spaceSync/SyncStore.ts`, `SyncSink.ts`, `test/recordingSink.ts`
- Modify: `typescript/internal/src/spaceSync/test/harness.ts` (append `makeHarness`, `runWithHarness`)
- Test: `typescript/internal/src/spaceSync/SyncStore.test.ts`

**Interfaces:**
- Produces:
  - `class SyncStore` service: `getSpace(space) → Effect<Option<SpaceState>, StoreError>`, `putSpace(state)`, `removeSpace(space)` (drops repo states too), `dueSpaces(now, limit) → Effect<ReadonlyArray<SpaceState>, StoreError>` (`nextDueAt <= now`, oldest first), `getRepo(space, did) → Effect<Option<RepoState>, StoreError>`, `listRepoDids(space) → Effect<ReadonlyArray<string>, StoreError>`, `putRepo(state)`, `removeRepo(space, did)`. Static `memory: Layer<SyncStore>`.
  - `class SyncSink` service: `apply(batch: RepoBatch) → Effect<void, SinkError>`.
  - `makeRecordingSink() → { state: RecordingSinkState; layer: Layer<SyncSink> }`. `RecordingSinkState`: `batches: Array<{ _tag; space; did?; rev?; paths?: string[] }>`, `view(space, did) → Map<path, cidString>`, `failNext: number`, `applyDelayMs: number`, `inFlight: Set<string>`, `maxConcurrentSpaces: number`, `interrupted: number`.
  - `harness.ts`: `type Harness = { net; sink; layer }`, `makeHarness(overrides?) → Promise<Harness>`, `runWithHarness(body, overrides?)`. `layer` provides `SyncStore | SyncSink | SpaceClient | Credentials | Identity | DelegationSource`.

- [ ] **Step 1: Write the failing test**

```ts
// @vitest-environment node
import { it } from "@effect/vitest";
import { Effect, Option } from "effect";
import { describe, expect } from "vitest";
import { SyncStore } from "./SyncStore";

const space = (name: string, nextDueAt: number) => ({
  space: `at://did:plc:a/space/com.example.board/${name}`,
  authority: "did:plc:a",
  nextDueAt,
  failures: 0,
});

describe("SyncStore.memory", () => {
  it.effect("returns due spaces oldest first, limited", () =>
    Effect.gen(function* () {
      const store = yield* SyncStore;
      yield* store.putSpace(space("c", 30));
      yield* store.putSpace(space("a", 10));
      yield* store.putSpace(space("b", 20));
      yield* store.putSpace(space("later", 100));
      const due = yield* store.dueSpaces(50, 2);
      expect(due.map((s) => s.space.split("/").at(-1))).toEqual(["a", "b"]);
    }).pipe(Effect.provide(SyncStore.memory)),
  );

  it.effect("removeSpace drops its repo states", () =>
    Effect.gen(function* () {
      const store = yield* SyncStore;
      const s = space("x", 0);
      yield* store.putSpace(s);
      yield* store.putRepo({ space: s.space, did: "did:plc:b", rev: "1", ltHash: new Uint8Array(2048) });
      expect(yield* store.listRepoDids(s.space)).toEqual(["did:plc:b"]);
      yield* store.removeSpace(s.space);
      expect(Option.isNone(yield* store.getRepo(s.space, "did:plc:b"))).toBe(true);
      expect(Option.isNone(yield* store.getSpace(s.space))).toBe(true);
    }).pipe(Effect.provide(SyncStore.memory)),
  );

  it.effect("stores a copy of ltHash so callers can't mutate stored state", () =>
    Effect.gen(function* () {
      const store = yield* SyncStore;
      const ltHash = new Uint8Array(2048);
      yield* store.putRepo({ space: "s", did: "d", rev: "1", ltHash });
      ltHash[0] = 9;
      const stored = yield* store.getRepo("s", "d");
      expect(Option.getOrThrow(stored).ltHash[0]).toBe(0);
    }).pipe(Effect.provide(SyncStore.memory)),
  );
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `pnpm --filter internal exec vitest run src/spaceSync/SyncStore.test.ts`
Expected: FAIL. Cannot resolve `./SyncStore`.

- [ ] **Step 3: Write `SyncStore.ts` and `SyncSink.ts`**

`SyncStore.ts`:

```ts
import { Context, Effect, Layer, Option } from "effect";
import type { StoreError } from "./errors";
import type { RepoState, SpaceState } from "./types";

/** Host-provided durable sync state. Implementations should make each call atomic. */
export class SyncStore extends Context.Service<
  SyncStore,
  {
    readonly getSpace: (space: string) => Effect.Effect<Option.Option<SpaceState>, StoreError>;
    readonly putSpace: (state: SpaceState) => Effect.Effect<void, StoreError>;
    /** Also drops every RepoState of the space. */
    readonly removeSpace: (space: string) => Effect.Effect<void, StoreError>;
    /** Spaces with `nextDueAt <= now`, oldest first. */
    readonly dueSpaces: (now: number, limit: number) => Effect.Effect<ReadonlyArray<SpaceState>, StoreError>;
    readonly getRepo: (space: string, did: string) => Effect.Effect<Option.Option<RepoState>, StoreError>;
    readonly listRepoDids: (space: string) => Effect.Effect<ReadonlyArray<string>, StoreError>;
    readonly putRepo: (state: RepoState) => Effect.Effect<void, StoreError>;
    readonly removeRepo: (space: string, did: string) => Effect.Effect<void, StoreError>;
  }
>()("internal/spaceSync/SyncStore") {
  /** Process-local store for tests and ephemeral syncers. */
  static readonly memory = Layer.sync(SyncStore, () => {
    const spaces = new Map<string, SpaceState>();
    const repos = new Map<string, Map<string, RepoState>>();
    return SyncStore.of({
      getSpace: (space) => Effect.sync(() => Option.fromNullishOr(spaces.get(space))),
      putSpace: (state) => Effect.sync(() => void spaces.set(state.space, state)),
      removeSpace: (space) =>
        Effect.sync(() => {
          spaces.delete(space);
          repos.delete(space);
        }),
      dueSpaces: (now, limit) =>
        Effect.sync(() =>
          [...spaces.values()]
            .filter((s) => s.nextDueAt <= now)
            .sort((a, b) => a.nextDueAt - b.nextDueAt)
            .slice(0, limit),
        ),
      getRepo: (space, did) => Effect.sync(() => Option.fromNullishOr(repos.get(space)?.get(did))),
      listRepoDids: (space) => Effect.sync(() => [...(repos.get(space)?.keys() ?? [])]),
      putRepo: (state) =>
        Effect.sync(() => {
          let bySpace = repos.get(state.space);
          if (!bySpace) {
            bySpace = new Map();
            repos.set(state.space, bySpace);
          }
          bySpace.set(state.did, { ...state, ltHash: state.ltHash.slice() });
        }),
      removeRepo: (space, did) => Effect.sync(() => void repos.get(space)?.delete(did)),
    });
  });
}
```

`SyncSink.ts`:

```ts
import { Context, type Effect } from "effect";
import type { SinkError } from "./errors";
import type { RepoBatch } from "./types";

/**
 * Host-provided: applies verified batches to the host's index. Delivery is
 * at-least-once, so apply must be idempotent (key on uri + cid). apply may be
 * interrupted (e.g. unwatch during a Reset) and must roll back when it is.
 */
export class SyncSink extends Context.Service<
  SyncSink,
  { readonly apply: (batch: RepoBatch) => Effect.Effect<void, SinkError> }
>()("internal/spaceSync/SyncSink") {}
```

- [ ] **Step 4: Run the store test**

Run: `pnpm --filter internal exec vitest run src/spaceSync/SyncStore.test.ts`
Expected: 3 tests PASS.

- [ ] **Step 5: Write `test/recordingSink.ts`**

```ts
import { Effect, Layer, Stream } from "effect";
import { SinkError } from "../errors";
import { SyncSink } from "../SyncSink";

export interface RecordedBatch {
  readonly _tag: string;
  readonly space: string;
  readonly did?: string;
  readonly rev?: string;
  readonly paths?: ReadonlyArray<string>;
}

export interface RecordingSinkState {
  readonly batches: RecordedBatch[];
  /** space|did → path → cid string: the materialized view a real index would hold. */
  readonly views: Map<string, Map<string, string>>;
  view(space: string, did: string): Map<string, string>;
  failNext: number;
  applyDelayMs: number;
  readonly inFlight: Set<string>;
  maxConcurrentSpaces: number;
  interrupted: number;
}

export const makeRecordingSink = () => {
  const state: RecordingSinkState = {
    batches: [],
    views: new Map(),
    view: (space, did) => state.views.get(`${space}|${did}`) ?? new Map(),
    failNext: 0,
    applyDelayMs: 0,
    inFlight: new Set(),
    maxConcurrentSpaces: 0,
    interrupted: 0,
  };

  const layer = Layer.succeed(
    SyncSink,
    SyncSink.of({
      apply: (batch) =>
        Effect.gen(function* () {
          state.inFlight.add(batch.space);
          state.maxConcurrentSpaces = Math.max(state.maxConcurrentSpaces, state.inFlight.size);
          if (state.applyDelayMs > 0) yield* Effect.sleep(state.applyDelayMs);
          if (state.failNext > 0) {
            state.failNext--;
            return yield* new SinkError({ message: "sink failure (test)", cause: new Error("boom") });
          }
          switch (batch._tag) {
            case "Ops": {
              const key = `${batch.space}|${batch.did}`;
              const view = new Map(state.views.get(key));
              for (const change of batch.changes) {
                const path = `${change.collection}/${change.rkey}`;
                if (change.cid) view.set(path, change.cid.toString());
                else view.delete(path);
              }
              state.views.set(key, view);
              state.batches.push({ _tag: "Ops", space: batch.space, did: batch.did, rev: batch.rev, paths: batch.changes.map((c) => `${c.collection}/${c.rkey}`) });
              return;
            }
            case "Reset": {
              const records = yield* Stream.runCollect(batch.records).pipe(
                Effect.mapError((e) => new SinkError({ message: e.message, cause: e })),
              );
              state.views.set(`${batch.space}|${batch.did}`, new Map(records.map((r) => [`${r.collection}/${r.rkey}`, r.cid.toString()])));
              state.batches.push({ _tag: "Reset", space: batch.space, did: batch.did, rev: batch.rev, paths: records.map((r) => `${r.collection}/${r.rkey}`) });
              return;
            }
            case "RepoRemoved":
              state.views.delete(`${batch.space}|${batch.did}`);
              state.batches.push({ _tag: "RepoRemoved", space: batch.space, did: batch.did });
              return;
            case "SpaceDeleted":
              for (const key of [...state.views.keys()]) if (key.startsWith(`${batch.space}|`)) state.views.delete(key);
              state.batches.push({ _tag: "SpaceDeleted", space: batch.space });
              return;
          }
        }).pipe(
          Effect.onInterrupt(() => Effect.sync(() => void state.interrupted++)),
          Effect.ensuring(Effect.sync(() => void state.inFlight.delete(batch.space))),
        ),
    }),
  );

  return { state, layer };
};
```

- [ ] **Step 6: Append `makeHarness` / `runWithHarness` to `test/harness.ts`**

Add the imports to the top of `test/harness.ts`:

```ts
import { Credentials } from "../Credentials";
import { Identity } from "../Identity";
import { SpaceClient } from "../SpaceClient";
import { SyncSink } from "../SyncSink";
import { SyncStore } from "../SyncStore";
import { server } from "../../test/msw";
import { FakeNetwork } from "./fakeNetwork";
import { type RecordingSinkState, makeRecordingSink } from "./recordingSink";
```

(Change the existing `import { type FakeNetwork, PLC_URL } from "./fakeNetwork";` to `import { FakeNetwork, PLC_URL } from "./fakeNetwork";`, and remove the separate `FakeNetwork` import above if it duplicates.)

Append:

```ts
export type HarnessServices = SyncStore | SyncSink | SpaceClient | Credentials | Identity | DelegationSource;

export interface Harness {
  readonly net: FakeNetwork;
  readonly sink: RecordingSinkState;
  readonly layer: Layer.Layer<HarnessServices>;
}

export const makeHarness = async (overrides: Partial<SpaceSyncOptions> = {}): Promise<Harness> => {
  const net = new FakeNetwork();
  server.use(...net.handlers);
  const sink = makeRecordingSink();
  const layer = Layer.mergeAll(
    SpaceClient.layer.pipe(
      Layer.provideMerge(Credentials.layer),
      Layer.provideMerge(Identity.layer),
      Layer.provideMerge(fakeDelegation(net)),
    ),
    SyncStore.memory,
    sink.layer,
  ).pipe(Layer.provide(testConfig(overrides)));
  return { net, sink: sink.state, layer };
};

/** Builds a fresh harness, then runs `body` with its services provided. */
export const runWithHarness = <A, E>(
  body: (h: Harness) => Effect.Effect<A, E, HarnessServices>,
  overrides: Partial<SpaceSyncOptions> = {},
) =>
  Effect.promise(() => makeHarness(overrides)).pipe(
    Effect.flatMap((h) => body(h).pipe(Effect.provide(h.layer), Effect.provide(testConfig(overrides)))),
  );
```

(Config is provided to the body as well, so code that reads `SpaceSyncConfig` directly sees the test values.)

- [ ] **Step 7: Typecheck and run everything so far**

Run: `pnpm --filter internal exec tsc --noEmit -p . && pnpm --filter internal exec vitest run src/spaceSync`
Expected: no type errors in `src/spaceSync`; all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add typescript/internal/src/spaceSync
git commit -m "spaceSync: add SyncStore/SyncSink ports, memory store and test harness

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Repo sync (incremental, recovery, commit)

**Files:**
- Create: `typescript/internal/src/spaceSync/repoSync.ts`
- Test: `typescript/internal/src/spaceSync/repoSync.test.ts`

**Interfaces:**
- Consumes: `SpaceClient`, `Identity`, `SyncStore`, `SyncSink`, `ListedRepo`, `RepoBatch`, `SyncEvent`.
- Produces: `syncRepo(space: string, listed: ListedRepo) → Effect<Option<SyncEvent>, SinkError | StoreError | CredentialError | RepoSyncError, SpaceClient | Identity | SyncStore | SyncSink>`. It returns `Option.none()` when an incremental sync found nothing new.

- [ ] **Step 1: Write the failing test**

```ts
// @vitest-environment node
import { it } from "@effect/vitest";
import { Effect, Option } from "effect";
import { describe, expect } from "vitest";
import { syncRepo } from "./repoSync";
import { SyncStore } from "./SyncStore";
import { runWithHarness } from "./test/harness";

const listed = (space: { repoRevOf(d: string): string; spaceRevOf(d: string): string }, did: string) => ({
  did,
  repoRev: space.repoRevOf(did),
  spaceRev: space.spaceRevOf(did),
});

describe("syncRepo", () => {
  it.live("recovers a repo with no local state and stores its rev", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        const event = yield* syncRepo(space.ref, listed(space, alice.did));
        expect(Option.getOrThrow(event)._tag).toBe("Reset");
        expect(sink.view(space.ref, alice.did)).toEqual(space.expectedView(alice.did));
        const stored = yield* Effect.flatMap(SyncStore, (s) => s.getRepo(space.ref, alice.did));
        expect(Option.getOrThrow(stored).rev).toBe(space.repoRevOf(alice.did));
      }),
    ),
  );

  it.live("syncs new writes incrementally", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* syncRepo(space.ref, listed(space, alice.did));
        yield* Effect.promise(() => space.write(alice, "com.example.post", "2", { text: "b" }));
        const event = yield* syncRepo(space.ref, listed(space, alice.did));
        expect(Option.getOrThrow(event)._tag).toBe("Ops");
        expect(space.getRepoCalls).toBe(1);
        expect(sink.view(space.ref, alice.did)).toEqual(space.expectedView(alice.did));
      }),
    ),
  );

  it.live("collapses multiple ops on one path into the final change", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "keep", { text: "k" }));
        yield* syncRepo(space.ref, listed(space, alice.did));
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "v1" }));
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", null));
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "v3" }));
        yield* syncRepo(space.ref, listed(space, alice.did));
        expect(sink.batches.at(-1)?.paths).toEqual(["com.example.post/1"]);
        expect(sink.view(space.ref, alice.did)).toEqual(space.expectedView(alice.did));
      }),
    ),
  );

  it.live("falls back to recovery when the oplog no longer covers `since`", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* syncRepo(space.ref, listed(space, alice.did));
        yield* Effect.promise(() => space.write(alice, "com.example.post", "2", { text: "b" }));
        space.truncateOplog(alice.did);
        const event = yield* syncRepo(space.ref, listed(space, alice.did));
        expect(Option.getOrThrow(event)._tag).toBe("Reset");
        expect(sink.view(space.ref, alice.did)).toEqual(space.expectedView(alice.did));
      }),
    ),
  );

  it.live("falls back to recovery when the oplog commit fails verification", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* syncRepo(space.ref, listed(space, alice.did));
        yield* Effect.promise(() => space.write(alice, "com.example.post", "2", { text: "b" }));
        space.corruptOpsCommits.add(alice.did);
        const event = yield* syncRepo(space.ref, listed(space, alice.did));
        expect(Option.getOrThrow(event)._tag).toBe("Reset");
      }),
    ),
  );

  it.live("falls back to recovery when the local set hash has diverged", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* syncRepo(space.ref, listed(space, alice.did));
        const store = yield* SyncStore;
        const local = Option.getOrThrow(yield* store.getRepo(space.ref, alice.did));
        const tampered = local.ltHash.slice();
        tampered[0] ^= 0xff;
        yield* store.putRepo({ ...local, ltHash: tampered });
        yield* Effect.promise(() => space.write(alice, "com.example.post", "2", { text: "b" }));
        const event = yield* syncRepo(space.ref, listed(space, alice.did));
        expect(Option.getOrThrow(event)._tag).toBe("Reset");
      }),
    ),
  );

  it.live("does not advance the store when the sink fails", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        sink.failNext = 1;
        const error = yield* Effect.flip(syncRepo(space.ref, listed(space, alice.did)));
        expect(error._tag).toBe("SinkError");
        const stored = yield* Effect.flatMap(SyncStore, (s) => s.getRepo(space.ref, alice.did));
        expect(Option.isNone(stored)).toBe(true);
      }),
    ),
  );

  it.live("fails with RepoSyncError when the host is behind the listed revision", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        const error = yield* Effect.flip(
          syncRepo(space.ref, { ...listed(space, alice.did), repoRev: "zzzzzzzzzzzzz" }),
        );
        expect(error._tag).toBe("RepoSyncError");
      }),
    ),
  );
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `pnpm --filter internal exec vitest run src/spaceSync/repoSync.test.ts`
Expected: FAIL. Cannot resolve `./repoSync`.

- [ ] **Step 3: Write `repoSync.ts`**

```ts
import { type Cid, type LexMap, parseCid } from "@atproto/lex";
import { RepoCommit, type SignedCommit, verifyCommit, verifyRepoCar } from "@atproto/space";
import type { NsidString, RecordKeyString } from "@atproto/syntax";
import { Effect, Option, Predicate, Stream } from "effect";
import { RepoSyncError, RepoVerificationError, errorMessage } from "./errors";
import { Identity } from "./Identity";
import { SpaceClient } from "./SpaceClient";
import { SyncSink } from "./SyncSink";
import { SyncStore } from "./SyncStore";
import type { Change, ListedRepo, RepoBatch, RepoState, SyncEvent } from "./types";

/**
 * Deliver a batch, then persist the repo state. apply may be interrupted (the
 * sink rolls back); once it returns, putRepo always runs so sink and store stay
 * in step. A crash between the two replays the batch (at-least-once).
 */
const commitBatch = (batch: RepoBatch, state: RepoState) =>
  Effect.uninterruptibleMask((restore) =>
    Effect.gen(function* () {
      const sink = yield* SyncSink;
      const store = yield* SyncStore;
      if (!(batch._tag === "Ops" && batch.changes.length === 0)) yield* restore(sink.apply(batch));
      yield* store.putRepo(state);
    }),
  );

const parseCidOr = (space: string, did: string, value: string) =>
  Effect.try({
    try: () => parseCid(value),
    catch: () => new RepoVerificationError({ space, did, message: `invalid cid ${value}` }),
  });

const incremental = Effect.fnUntraced(function* (space: string, listed: ListedRepo, local: RepoState) {
  const client = yield* SpaceClient;
  const identity = yield* Identity;
  const did = listed.did;
  const state = RepoCommit.fromState(local.ltHash);
  // Later ops on a path supersede earlier ones, so the batch holds one change per path.
  const changes = new Map<string, Change>();
  let cursor: string | undefined;
  let commit: SignedCommit | undefined;
  do {
    const page = yield* client.listRepoOps(space, did, local.rev, cursor);
    for (const op of page.ops) {
      const cid: Cid | null = op.cid === null ? null : yield* parseCidOr(space, did, op.cid);
      const prev: Cid | null = op.prev === null ? null : yield* parseCidOr(space, did, op.prev);
      yield* Effect.try({
        try: () => state.applyOp({ collection: op.collection as NsidString, rkey: op.rkey as RecordKeyString, cid, prev }),
        catch: (error) => new RepoVerificationError({ space, did, message: `cannot apply op: ${errorMessage(error)}` }),
      });
      const path = `${op.collection}/${op.rkey}`;
      changes.delete(path);
      changes.set(path, {
        uri: `${space}/${did}/${path}`,
        collection: op.collection,
        rkey: op.rkey,
        cid,
        value: cid && Predicate.isObject(op.value) ? (op.value as LexMap) : undefined,
      });
    }
    commit = page.commit ?? commit;
    cursor = page.cursor;
  } while (cursor);

  if (!commit) return yield* new RepoVerificationError({ space, did, message: "oplog did not end with a commit" });
  const finalCommit = commit;
  const { signingKey } = yield* identity.resolve(did).pipe(
    Effect.mapError((e) => new RepoVerificationError({ space, did, message: e.message })),
  );
  const valid = yield* Effect.promise(() =>
    verifyCommit(finalCommit, { space, author: did, rev: finalCommit.rev }, signingKey).catch(() => false),
  );
  if (!valid) return yield* new RepoVerificationError({ space, did, message: "commit signature or MAC is invalid" });
  if (!state.matches(finalCommit)) return yield* new RepoVerificationError({ space, did, message: "set hash does not match commit" });
  if (finalCommit.rev < listed.repoRev) {
    return yield* new RepoVerificationError({ space, did, message: "repo host is behind the listed revision" });
  }
  const batch = { _tag: "Ops" as const, space, did, rev: finalCommit.rev, changes: [...changes.values()] };
  yield* commitBatch(batch, { space, did, rev: finalCommit.rev, ltHash: state.setHash.state() });
  return batch.changes.length === 0 ? Option.none<SyncEvent>() : Option.some<SyncEvent>(batch);
});

const recover = Effect.fnUntraced(function* (space: string, listed: ListedRepo) {
  const client = yield* SpaceClient;
  const identity = yield* Identity;
  const did = listed.did;
  const syncError = (message: string, cause?: unknown) => new RepoSyncError({ space, did, message, cause });
  const { signingKey } = yield* identity.resolve(did).pipe(Effect.mapError((e) => syncError(e.message, e)));

  return yield* Effect.scoped(
    Effect.gen(function* () {
      const car = yield* client.getRepo(space, did).pipe(
        Effect.catchTag("XrpcError", (e) => Effect.fail(syncError(`getRepo failed: ${e.message}`, e))),
      );
      // Verifies the commit and that the index matches its hash; records are checked as they stream.
      const verified = yield* Effect.acquireRelease(
        Effect.tryPromise({
          try: () => verifyRepoCar(car, { space, author: did, didKey: signingKey }),
          catch: (error) => syncError(`repo CAR failed verification: ${errorMessage(error)}`, error),
        }),
        (repo) => Effect.promise(() => repo[Symbol.asyncDispose]()),
      );
      const rev = verified.commit.rev;
      if (rev < listed.repoRev) return yield* syncError(`repo host is behind (${rev} < ${listed.repoRev})`);
      const records = Stream.fromAsyncIterable(
        verified.records,
        (error) => new RepoVerificationError({ space, did, message: errorMessage(error) }),
      );
      yield* commitBatch({ _tag: "Reset", space, did, rev, records }, { space, did, rev, ltHash: verified.repo.setHash.state() });
      return Option.some<SyncEvent>({ _tag: "Reset", space, did, rev });
    }),
  );
});

/**
 * Bring one repo up to `listed.repoRev`: incrementally via listRepoOps when local
 * state exists, otherwise (or on any verification failure) via a full getRepo.
 */
export const syncRepo = Effect.fn("syncRepo")(function* (space: string, listed: ListedRepo) {
  const store = yield* SyncStore;
  const local = yield* store.getRepo(space, listed.did);
  if (Option.isSome(local)) {
    const result = yield* incremental(space, listed, local.value).pipe(
      Effect.map(Option.some),
      Effect.catchTag(["RepoVerificationError", "XrpcError"], (error) =>
        Effect.logWarning("incremental sync failed; recovering", error.message).pipe(
          Effect.annotateLogs({ space, did: listed.did }),
          Effect.as(Option.none<Option.Option<SyncEvent>>()),
        ),
      ),
    );
    if (Option.isSome(result)) return result.value;
  }
  return yield* recover(space, listed);
});
```

- [ ] **Step 4: Run the test**

Run: `pnpm --filter internal exec vitest run src/spaceSync/repoSync.test.ts`
Expected: 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add typescript/internal/src/spaceSync/repoSync.ts typescript/internal/src/spaceSync/repoSync.test.ts
git commit -m "spaceSync: add incremental repo sync with CAR recovery

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Space pass (listRepos walk, fan-out, pruning, checkpoint, backoff)

**Files:**
- Create: `typescript/internal/src/spaceSync/spacePass.ts`
- Test: `typescript/internal/src/spaceSync/spacePass.test.ts`

**Interfaces:**
- Consumes: `syncRepo`, `SpaceClient`, `SyncStore`, `SyncSink`, `SpaceSyncConfig`.
- Produces:
  - `type PassKind = "CatchUp" | "Maintenance" | "Full"`
  - `runSpacePass(space, kind) → Effect<ReadonlyArray<SyncEvent>, CredentialError | XrpcError | StoreError | SinkError, SpaceClient | Identity | SyncStore | SyncSink>`. It is a no-op returning `[]` when the space isn't in the store.
  - `recordPassFailure(space, error: unknown) → Effect<void, never, SyncStore>`
  - `backoffMillis(config, failures) → number`

- [ ] **Step 1: Write the failing test**

```ts
// @vitest-environment node
import { it } from "@effect/vitest";
import { Effect, Option } from "effect";
import { describe, expect } from "vitest";
import { recordPassFailure, runSpacePass } from "./spacePass";
import { SyncStore } from "./SyncStore";
import { type FakeSpace } from "./test/fakeNetwork";
import { SERVICE_DID, runWithHarness } from "./test/harness";

const track = (space: FakeSpace) =>
  Effect.flatMap(SyncStore, (s) => s.putSpace({ space: space.ref, authority: space.authority.did, nextDueAt: 0, failures: 0 }));
const stateOf = (space: FakeSpace) =>
  Effect.flatMap(SyncStore, (s) => s.getSpace(space.ref)).pipe(Effect.map(Option.getOrThrow));

describe("runSpacePass", () => {
  it.live("full pass syncs every writer, registers, and checkpoints at the newest spaceRev", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const [alice, bob] = yield* Effect.promise(() => Promise.all([net.createAccount("alice"), net.createAccount("bob")]));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* Effect.promise(() => space.write(bob, "com.example.post", "1", { text: "b" }));
        yield* track(space);
        const events = yield* runSpacePass(space.ref, "Full");
        expect(events.map((e) => e._tag)).toEqual(["Reset", "Reset"]);
        expect(space.registrations).toEqual([SERVICE_DID]);
        const state = yield* stateOf(space);
        expect(state.spaceRev).toBe(space.spaceRevOf(bob.did));
        expect(state.failures).toBe(0);
        expect(state.lastFullPassAt).toBeDefined();
        expect(sink.view(space.ref, bob.did)).toEqual(space.expectedView(bob.did));
      }),
    ),
  );

  it.live("catch-up pass only touches writers after the checkpoint", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const [alice, bob] = yield* Effect.promise(() => Promise.all([net.createAccount("alice"), net.createAccount("bob")]));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* Effect.promise(() => space.write(bob, "com.example.post", "1", { text: "b" }));
        yield* track(space);
        yield* runSpacePass(space.ref, "Full");
        yield* Effect.promise(() => space.write(bob, "com.example.post", "2", { text: "b2" }));
        const opsBefore = space.listRepoOpsCalls;
        const events = yield* runSpacePass(space.ref, "CatchUp");
        expect(events).toEqual([expect.objectContaining({ _tag: "Ops", did: bob.did })]);
        expect(space.listRepoOpsCalls - opsBefore).toBe(1);
      }),
    ),
  );

  it.live("full pass prunes writers no longer listed", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const [alice, bob] = yield* Effect.promise(() => Promise.all([net.createAccount("alice"), net.createAccount("bob")]));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* Effect.promise(() => space.write(bob, "com.example.post", "1", { text: "b" }));
        yield* track(space);
        yield* runSpacePass(space.ref, "Full");
        space.delisted.add(bob.did);
        const events = yield* runSpacePass(space.ref, "Full");
        expect(events).toContainEqual({ _tag: "RepoRemoved", space: space.ref, did: bob.did });
        expect(yield* Effect.flatMap(SyncStore, (s) => s.listRepoDids(space.ref))).toEqual([alice.did]);
        expect(sink.view(space.ref, bob.did).size).toBe(0);
      }),
    ),
  );

  it.live("checkpoints before the first failed repo and records a failure", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const [alice, bob, carol] = yield* Effect.promise(() =>
          Promise.all([net.createAccount("alice"), net.createAccount("bob"), net.createAccount("carol")]),
        );
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* Effect.promise(() => space.write(bob, "com.example.post", "1", { text: "b" }));
        yield* Effect.promise(() => space.write(carol, "com.example.post", "1", { text: "c" }));
        space.failingRepos.add(bob.did);
        yield* track(space);
        const events = yield* runSpacePass(space.ref, "Full");
        expect(events.map((e) => "did" in e && e.did)).toEqual([alice.did, carol.did]);
        const failed = yield* stateOf(space);
        expect(failed.spaceRev).toBe(space.spaceRevOf(alice.did));
        expect(failed.failures).toBe(1);
        space.failingRepos.clear();
        yield* runSpacePass(space.ref, "CatchUp");
        const healed = yield* stateOf(space);
        expect(healed.spaceRev).toBe(space.spaceRevOf(carol.did));
        expect(healed.failures).toBe(0);
      }),
    ),
  );

  it.live("dedupes a writer that reappears in the listing", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const [alice, bob] = yield* Effect.promise(() => Promise.all([net.createAccount("alice"), net.createAccount("bob")]));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* Effect.promise(() => space.write(bob, "com.example.post", "1", { text: "b" }));
        // One entry per page; alice writes again after the first page, so she
        // reappears later in the same walk with a newer repoRev.
        space.listReposPageSize = 1;
        let wrote = false;
        space.onListRepos = async () => {
          if (wrote) return;
          wrote = true;
          await space.write(alice, "com.example.post", "2", { text: "a2" });
        };
        yield* track(space);
        const events = yield* runSpacePass(space.ref, "Full");
        expect(events.filter((e) => "did" in e && e.did === alice.did)).toHaveLength(1);
        expect(space.getRepoCalls).toBe(2);
        expect(sink.view(space.ref, alice.did)).toEqual(space.expectedView(alice.did));
      }),
    ),
  );

  it.live("rejects an unordered listRepos response without advancing the checkpoint", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const [alice, bob] = yield* Effect.promise(() => Promise.all([net.createAccount("alice"), net.createAccount("bob")]));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* Effect.promise(() => space.write(bob, "com.example.post", "1", { text: "b" }));
        space.unorderedListRepos = true;
        yield* track(space);
        const error = yield* Effect.flip(runSpacePass(space.ref, "Full"));
        expect(error._tag).toBe("XrpcError");
        expect((yield* stateOf(space)).spaceRev).toBeUndefined();
      }),
    ),
  );

  it.live("renews the registration when it is inside the renewal lead", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        space.registrationLifetimeMs = 30 * 60 * 1000; // inside the default 1h lead
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* track(space);
        yield* runSpacePass(space.ref, "Full");
        yield* runSpacePass(space.ref, "CatchUp");
        expect(space.registrations).toHaveLength(2);
      }),
    ),
  );

  it.live("recordPassFailure backs off exponentially up to the cap", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* track(space);
        const before = Date.now();
        for (let i = 0; i < 5; i++) yield* recordPassFailure(space.ref, new Error("nope"));
        const state = yield* stateOf(space);
        expect(state.failures).toBe(5);
        expect(state.lastError).toBe("nope");
        // test config: base 50ms, cap 200ms
        expect(state.nextDueAt - before).toBeLessThanOrEqual(200 + 50);
      }),
    ),
  );
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `pnpm --filter internal exec vitest run src/spaceSync/spacePass.test.ts`
Expected: FAIL. Cannot resolve `./spacePass`.

- [ ] **Step 3: Write `spacePass.ts`**

```ts
import { Clock, Duration, Effect, Option } from "effect";
import { type SpaceSyncOptions, SpaceSyncConfig } from "./config";
import { XrpcError, errorMessage } from "./errors";
import { syncRepo } from "./repoSync";
import { SpaceClient } from "./SpaceClient";
import { SyncSink } from "./SyncSink";
import { SyncStore } from "./SyncStore";
import type { ListedRepo, SyncEvent } from "./types";

export type PassKind = "CatchUp" | "Maintenance" | "Full";

export const backoffMillis = (config: SpaceSyncOptions, failures: number): number =>
  Math.min(
    Duration.toMillis(config.backoffBase) * 2 ** Math.max(0, failures - 1),
    Duration.toMillis(config.backoffCap),
  );

const LIST_REPOS = "com.atproto.space.listRepos";

/**
 * One reconcile pass over a space. Callers (SpaceSyncer) guarantee passes for a
 * space never overlap.
 */
export const runSpacePass = Effect.fn("runSpacePass")(function* (space: string, kind: PassKind) {
  const store = yield* SyncStore;
  const client = yield* SpaceClient;
  const sink = yield* SyncSink;
  const config = yield* SpaceSyncConfig;

  const current = yield* store.getSpace(space);
  if (Option.isNone(current)) return [] as ReadonlyArray<SyncEvent>;
  let state = current.value;
  const now = yield* Clock.currentTimeMillis;
  const renewLeadMs = Duration.toMillis(config.registrationRenewLead);
  const fullIntervalMs = Duration.toMillis(config.fullPassInterval);

  if (config.serviceDid && (state.registrationExpiresAt === undefined || state.registrationExpiresAt - now <= renewLeadMs)) {
    const { expiresAt } = yield* client.registerNotify(space, config.serviceDid);
    state = { ...state, registrationExpiresAt: expiresAt };
    yield* store.putSpace(state);
  }

  const full =
    kind === "Full" || state.spaceRev === undefined || (state.lastFullPassAt ?? 0) + fullIntervalMs <= now;

  // Walk the writer set. Entries must be strictly ascending by spaceRev and each
  // non-empty page's cursor must equal its last entry; a writer that reappears
  // keeps only its newest listing.
  const listed = new Map<string, ListedRepo>();
  let cursor = full ? undefined : state.spaceRev;
  while (true) {
    const page = yield* client.listRepos(space, cursor);
    if (page.repos.length === 0) break;
    for (const repo of page.repos) {
      if (cursor !== undefined && repo.spaceRev <= cursor) {
        return yield* new XrpcError({ method: LIST_REPOS, status: 200, error: "InvalidResponse", message: "listRepos entries are not in ascending spaceRev order" });
      }
      listed.delete(repo.did);
      listed.set(repo.did, { did: repo.did, repoRev: repo.repoRev, spaceRev: repo.spaceRev });
      cursor = repo.spaceRev;
    }
    if (page.cursor !== cursor) {
      return yield* new XrpcError({ method: LIST_REPOS, status: 200, error: "InvalidResponse", message: "listRepos cursor does not match its last entry" });
    }
  }

  const pending = [...listed.values()].sort((a, b) => (a.spaceRev < b.spaceRev ? -1 : 1));
  const results = yield* Effect.forEach(
    pending,
    (repo) =>
      Effect.gen(function* () {
        const local = yield* store.getRepo(space, repo.did);
        if (Option.isSome(local) && local.value.rev >= repo.repoRev) return { repo, ok: true as const, event: Option.none<SyncEvent>() };
        const event = yield* syncRepo(space, repo);
        return { repo, ok: true as const, event };
      }).pipe(
        // Per-repo failures don't fail the pass; the checkpoint below makes sure they're retried.
        Effect.catchTag(["RepoSyncError", "SinkError"], (error) =>
          Effect.logWarning("repo sync failed", error.message).pipe(
            Effect.annotateLogs({ space, did: repo.did }),
            Effect.as({ repo, ok: false as const, event: Option.none<SyncEvent>(), error: error.message }),
          ),
        ),
      ),
    { concurrency: config.repoConcurrency },
  );

  const events: SyncEvent[] = results.flatMap((r) => (Option.isSome(r.event) ? [r.event.value] : []));
  const firstFailure = results.findIndex((r) => !r.ok);

  if (full) {
    for (const did of yield* store.listRepoDids(space)) {
      if (listed.has(did)) continue;
      yield* Effect.uninterruptible(
        sink.apply({ _tag: "RepoRemoved", space, did }).pipe(Effect.andThen(store.removeRepo(space, did))),
      );
      events.push({ _tag: "RepoRemoved", space, did });
    }
  }

  // Never checkpoint past a failed repo: resume from the last entry before it.
  const checkpoint =
    firstFailure === -1 ? (cursor ?? state.spaceRev) : firstFailure === 0 ? state.spaceRev : pending[firstFailure - 1].spaceRev;
  const failed = firstFailure !== -1;
  const failures = failed ? state.failures + 1 : 0;
  const lastFullPassAt = full ? now : state.lastFullPassAt;
  const candidates = [(lastFullPassAt ?? now) + fullIntervalMs];
  if (state.registrationExpiresAt !== undefined) candidates.push(state.registrationExpiresAt - renewLeadMs);
  if (failed) candidates.push(now + backoffMillis(config, failures));
  const failedResult = failed ? results[firstFailure] : undefined;

  yield* store.putSpace({
    ...state,
    spaceRev: checkpoint,
    lastFullPassAt,
    failures,
    lastError: failedResult && "error" in failedResult ? `${failedResult.repo.did}: ${failedResult.error}` : undefined,
    nextDueAt: Math.min(...candidates),
  });
  return events as ReadonlyArray<SyncEvent>;
});

/** Record a failed pass on the space and schedule a backed-off retry. Never fails. */
export const recordPassFailure = (space: string, error: unknown) =>
  Effect.gen(function* () {
    const store = yield* SyncStore;
    const config = yield* SpaceSyncConfig;
    yield* Effect.logWarning("space pass failed", errorMessage(error)).pipe(Effect.annotateLogs({ space }));
    const current = yield* store.getSpace(space);
    if (Option.isNone(current)) return;
    const now = yield* Clock.currentTimeMillis;
    const failures = current.value.failures + 1;
    yield* store.putSpace({
      ...current.value,
      failures,
      lastError: errorMessage(error),
      nextDueAt: now + backoffMillis(config, failures),
    });
  }).pipe(Effect.catch((e) => Effect.logError("could not record pass failure", errorMessage(e))));
```

- [ ] **Step 4: Run the test**

Run: `pnpm --filter internal exec vitest run src/spaceSync/spacePass.test.ts`
Expected: 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add typescript/internal/src/spaceSync/spacePass.ts typescript/internal/src/spaceSync/spacePass.test.ts
git commit -m "spaceSync: add space reconcile pass with pruning and safe checkpoints

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: SpaceSyncer (on-demand fibers, scheduler, events)

**Files:**
- Create: `typescript/internal/src/spaceSync/SpaceSyncer.ts`
- Modify: `typescript/internal/src/spaceSync/test/harness.ts` (add `runWithSyncer`)
- Test: `typescript/internal/src/spaceSync/SpaceSyncer.test.ts`

**Interfaces:**
- Consumes: `runSpacePass`, `recordPassFailure`, `PassKind`, all services.
- Produces: `class SpaceSyncer` with:
  - `watch(space) → Effect<void, InvalidSpaceRefError | StoreError>`
  - `unwatch(space) → Effect<void, StoreError>`
  - `notifyWrite(input: NotifyWriteInput) → Effect<void, StoreError>`
  - `notifySpaceDeleted(space) → Effect<void, StoreError | SinkError>`
  - `getBlob(space, did, cid) → Effect<Uint8Array, SpaceCallError>`
  - `events: Stream<SyncEvent>`
  - `activeSpaces: Effect<number>`. Counts running space fibers.
  - `awaitIdle(space) → Effect<void>`. Resolves once the space has no running fiber and nothing pending.
  - `static layer: Layer<SpaceSyncer, never, SpaceClient | Identity | Credentials | SyncStore | SyncSink>`

- [ ] **Step 1: Add `runWithSyncer` to `test/harness.ts`**

```ts
import { SpaceSyncer } from "../SpaceSyncer";

/** Like runWithHarness, with SpaceSyncer (and its scheduler) running for the body. */
export const runWithSyncer = <A, E>(
  body: (h: Harness) => Effect.Effect<A, E, HarnessServices | SpaceSyncer>,
  overrides: Partial<SpaceSyncOptions> = {},
) =>
  Effect.promise(() => makeHarness(overrides)).pipe(
    Effect.flatMap((h) =>
      body(h).pipe(
        Effect.provide(SpaceSyncer.layer.pipe(Layer.provideMerge(h.layer), Layer.provide(testConfig(overrides)))),
        Effect.provide(testConfig(overrides)),
      ),
    ),
  );
```

- [ ] **Step 2: Write the failing test**

```ts
// @vitest-environment node
import { it } from "@effect/vitest";
import { Effect, Fiber, Option, Schema, Stream } from "effect";
import { describe, expect } from "vitest";
import { SpaceSyncer } from "./SpaceSyncer";
import { SyncStore } from "./SyncStore";
import { runWithSyncer } from "./test/harness";
import { NotifyWriteInput, lexJson } from "./wire";

const decodeNotify = (body: unknown) => Schema.decodeUnknownEffect(NotifyWriteInput)(lexJson(body)).pipe(Effect.orDie);

describe("SpaceSyncer", () => {
  it.live("watch performs an initial full sync and then goes idle", () =>
    runWithSyncer(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        const syncer = yield* SpaceSyncer;
        yield* syncer.watch(space.ref);
        yield* syncer.awaitIdle(space.ref);
        expect(sink.view(space.ref, alice.did)).toEqual(space.expectedView(alice.did));
        expect(yield* syncer.activeSpaces).toBe(0);
      }),
    ),
  );

  it.live("notifyWrite triggers an incremental catch-up; stale notifications are dropped", () =>
    runWithSyncer(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        const syncer = yield* SpaceSyncer;
        yield* syncer.watch(space.ref);
        yield* syncer.awaitIdle(space.ref);
        const stale = yield* decodeNotify(space.notifyWriteBody(alice.did));
        const listsBefore = space.listReposCalls;
        yield* syncer.notifyWrite(stale);
        yield* syncer.awaitIdle(space.ref);
        expect(space.listReposCalls).toBe(listsBefore);

        yield* Effect.promise(() => space.write(alice, "com.example.post", "2", { text: "b" }));
        yield* syncer.notifyWrite(yield* decodeNotify(space.notifyWriteBody(alice.did)));
        yield* syncer.awaitIdle(space.ref);
        expect(sink.batches.at(-1)?._tag).toBe("Ops");
        expect(sink.view(space.ref, alice.did)).toEqual(space.expectedView(alice.did));
      }),
    ),
  );

  it.live("coalesces a burst of notifications into one extra pass", () =>
    runWithSyncer(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "0", { text: "0" }));
        const syncer = yield* SpaceSyncer;
        yield* syncer.watch(space.ref);
        yield* syncer.awaitIdle(space.ref);
        sink.applyDelayMs = 50;
        const listsBefore = space.listReposCalls;
        for (let i = 1; i <= 50; i++) {
          yield* Effect.promise(() => space.write(alice, "com.example.post", String(i), { text: String(i) }));
          yield* syncer.notifyWrite(yield* decodeNotify(space.notifyWriteBody(alice.did)));
        }
        yield* syncer.awaitIdle(space.ref);
        // One pass for the first notification, at most one more for everything queued behind it.
        expect(space.listReposCalls - listsBefore).toBeLessThanOrEqual(2 * 2);
        expect(sink.view(space.ref, alice.did)).toEqual(space.expectedView(alice.did));
      }),
    ),
  );

  it.live("notifySpaceDeleted drops the space from the sink and the store", () =>
    runWithSyncer(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        const syncer = yield* SpaceSyncer;
        yield* syncer.watch(space.ref);
        yield* syncer.awaitIdle(space.ref);
        yield* syncer.notifySpaceDeleted(space.ref);
        expect(sink.batches.at(-1)?._tag).toBe("SpaceDeleted");
        expect(Option.isNone(yield* Effect.flatMap(SyncStore, (s) => s.getSpace(space.ref)))).toBe(true);
      }),
    ),
  );

  it.live("a SpaceDeleted credential error during a pass deletes the space", () =>
    runWithSyncer(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        space.deleted = true;
        const syncer = yield* SpaceSyncer;
        yield* syncer.watch(space.ref);
        yield* syncer.awaitIdle(space.ref);
        expect(sink.batches.map((b) => b._tag)).toEqual(["SpaceDeleted"]);
        expect(Option.isNone(yield* Effect.flatMap(SyncStore, (s) => s.getSpace(space.ref)))).toBe(true);
      }),
    ),
  );

  it.live("a failed pass backs off and the scheduler retries it", () =>
    runWithSyncer(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        space.deniedUsers.add(alice.did);
        const syncer = yield* SpaceSyncer;
        yield* syncer.watch(space.ref);
        yield* syncer.awaitIdle(space.ref);
        const failed = Option.getOrThrow(yield* Effect.flatMap(SyncStore, (s) => s.getSpace(space.ref)));
        expect(failed.failures).toBe(1);
        space.deniedUsers.clear();
        yield* Effect.sleep("400 millis");
        yield* syncer.awaitIdle(space.ref);
        expect(sink.view(space.ref, alice.did)).toEqual(space.expectedView(alice.did));
      }),
    ),
  );

  it.live("never runs more than maxActiveSpaces passes at once", () =>
    runWithSyncer(
      ({ net, sink }) =>
        Effect.gen(function* () {
          const alice = yield* Effect.promise(() => net.createAccount("alice"));
          sink.applyDelayMs = 20;
          const spaces = [];
          for (let i = 0; i < 20; i++) {
            const space = net.createSpace(alice, `s${i}`);
            yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
            spaces.push(space);
          }
          const syncer = yield* SpaceSyncer;
          yield* Effect.forEach(spaces, (s) => syncer.watch(s.ref), { discard: true });
          yield* Effect.forEach(spaces, (s) => syncer.awaitIdle(s.ref), { discard: true });
          expect(sink.maxConcurrentSpaces).toBeLessThanOrEqual(3);
          expect(sink.batches.filter((b) => b._tag === "Reset")).toHaveLength(20);
          expect(yield* syncer.activeSpaces).toBe(0);
        }),
      { maxActiveSpaces: 3 },
    ),
  );

  it.live("unwatch interrupts an in-flight sink apply and leaves no state", () =>
    runWithSyncer(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        sink.applyDelayMs = 500;
        const syncer = yield* SpaceSyncer;
        yield* syncer.watch(space.ref);
        yield* Effect.sleep("150 millis");
        yield* syncer.unwatch(space.ref);
        expect(sink.interrupted).toBe(1);
        expect(sink.batches).toHaveLength(0);
        const store = yield* SyncStore;
        expect(Option.isNone(yield* store.getSpace(space.ref))).toBe(true);
        expect(yield* store.listRepoDids(space.ref)).toEqual([]);
        expect(yield* syncer.activeSpaces).toBe(0);
      }),
    ),
  );

  it.live("publishes committed batches on the events stream", () =>
    runWithSyncer(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        const syncer = yield* SpaceSyncer;
        const collector = yield* syncer.events.pipe(Stream.take(1), Stream.runCollect, Effect.forkScoped);
        yield* Effect.sleep("20 millis");
        yield* syncer.watch(space.ref);
        const events = yield* Fiber.join(collector);
        expect(events[0]).toEqual({ _tag: "Reset", space: space.ref, did: alice.did, rev: space.repoRevOf(alice.did) });
      }),
    ).pipe(Effect.scoped),
  );

  it.live("the scheduler renews registrations before they expire", () =>
    runWithSyncer(
      ({ net }) =>
        Effect.gen(function* () {
          const alice = yield* Effect.promise(() => net.createAccount("alice"));
          const space = net.createSpace(alice);
          space.registrationLifetimeMs = 150;
          yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
          const syncer = yield* SpaceSyncer;
          yield* syncer.watch(space.ref);
          yield* Effect.sleep("500 millis");
          expect(space.registrations.length).toBeGreaterThanOrEqual(2);
        }),
      { registrationRenewLead: "100 millis" as never },
    ),
  );
});
```

(In the last test, pass `Duration.millis(100)` instead of the cast if `Duration` is imported. The cast just keeps the snippet short.)

- [ ] **Step 3: Run it to see it fail**

Run: `pnpm --filter internal exec vitest run src/spaceSync/SpaceSyncer.test.ts`
Expected: FAIL. Cannot resolve `./SpaceSyncer`.

- [ ] **Step 4: Write `SpaceSyncer.ts`**

```ts
import { Clock, Context, Duration, Effect, FiberMap, Layer, Option, PubSub, Schedule, Semaphore, Stream } from "effect";
import { SpaceSyncConfig } from "./config";
import type { Credentials } from "./Credentials";
import type { InvalidSpaceRefError, SinkError, StoreError } from "./errors";
import type { Identity } from "./Identity";
import { type SpaceCallError, SpaceClient } from "./SpaceClient";
import { type PassKind, recordPassFailure, runSpacePass } from "./spacePass";
import { SyncSink } from "./SyncSink";
import { SyncStore } from "./SyncStore";
import type { SyncEvent } from "./types";
import { type NotifyWriteInput, parseSpaceRef } from "./wire";

const STRENGTH: Record<PassKind, number> = { CatchUp: 0, Maintenance: 1, Full: 2 };

export class SpaceSyncer extends Context.Service<
  SpaceSyncer,
  {
    readonly watch: (space: string) => Effect.Effect<void, InvalidSpaceRefError | StoreError>;
    readonly unwatch: (space: string) => Effect.Effect<void, StoreError>;
    readonly notifyWrite: (input: NotifyWriteInput) => Effect.Effect<void, StoreError>;
    readonly notifySpaceDeleted: (space: string) => Effect.Effect<void, StoreError | SinkError>;
    readonly getBlob: (space: string, did: string, cid: string) => Effect.Effect<Uint8Array, SpaceCallError>;
    /** Committed batches (Reset carries no records), for fan-out such as SSE. */
    readonly events: Stream.Stream<SyncEvent>;
    readonly activeSpaces: Effect.Effect<number>;
    readonly awaitIdle: (space: string) => Effect.Effect<void>;
  }
>()("internal/spaceSync/SpaceSyncer") {
  static readonly layer = Layer.effect(
    SpaceSyncer,
    Effect.gen(function* () {
      const config = yield* SpaceSyncConfig;
      const store = yield* SyncStore;
      const sink = yield* SyncSink;
      const client = yield* SpaceClient;
      const services = yield* Effect.context<SpaceClient | Identity | Credentials | SyncStore | SyncSink>();
      const permits = yield* Semaphore.make(config.maxActiveSpaces);
      const fibers = yield* FiberMap.make<string>();
      const events = yield* PubSub.unbounded<SyncEvent>();

      // Mailbox: at most one pending trigger per space (the strongest). `running`
      // maps a space to the token of the fiber draining it; the token stops a
      // finishing fiber from clearing the entry of its replacement.
      const pending = new Map<string, PassKind>();
      const running = new Map<string, number>();
      let nextToken = 0;

      const deleteSpaceData = (space: string) =>
        Effect.gen(function* () {
          pending.delete(space);
          yield* sink.apply({ _tag: "SpaceDeleted", space });
          yield* store.removeSpace(space);
          yield* PubSub.publish(events, { _tag: "SpaceDeleted", space });
        }).pipe(Effect.uninterruptible);

      const drain = (space: string, token: number): Effect.Effect<void> =>
        Effect.gen(function* () {
          while (true) {
            // Take the next trigger or release `running` in the same synchronous
            // step, so an enqueue can never observe a fiber that has stopped draining.
            const kind = yield* Effect.sync(() => {
              const next = pending.get(space);
              if (next === undefined) {
                if (running.get(space) === token) running.delete(space);
                return undefined;
              }
              pending.delete(space);
              return next;
            });
            if (kind === undefined) return;
            yield* runSpacePass(space, kind).pipe(
              Effect.flatMap((evts) => PubSub.publishAll(events, evts)),
              Effect.catchTag("CredentialError", (error) =>
                error.reason === "SpaceDeleted" ? deleteSpaceData(space) : recordPassFailure(space, error),
              ),
              Effect.catch((error) => recordPassFailure(space, error)),
              Effect.provideContext(services),
              Effect.annotateLogs({ space }),
            );
          }
        }).pipe(
          permits.withPermits(1),
          Effect.ensuring(Effect.sync(() => void (running.get(space) === token && running.delete(space)))),
        );

      const enqueue = (space: string, kind: PassKind) =>
        Effect.gen(function* () {
          const token = yield* Effect.sync(() => {
            const prev = pending.get(space);
            if (prev === undefined || STRENGTH[kind] > STRENGTH[prev]) pending.set(space, kind);
            if (running.has(space)) return undefined;
            const t = ++nextToken;
            running.set(space, t);
            return t;
          });
          if (token !== undefined) yield* FiberMap.run(fibers, space, { onlyIfMissing: false })(drain(space, token));
        });

      const stop = (space: string) =>
        Effect.gen(function* () {
          pending.delete(space);
          yield* FiberMap.remove(fibers, space);
          running.delete(space);
        });

      // Single scheduler: idle spaces live only in the store. Enqueued spaces get
      // a lease (nextDueAt = now + backoffCap) so later ticks skip them while they
      // wait for a permit; the pass writes the real nextDueAt.
      const tick = Effect.gen(function* () {
        const now = yield* Clock.currentTimeMillis;
        const due = yield* store.dueSpaces(now, config.schedulerPageSize);
        const fullIntervalMs = Duration.toMillis(config.fullPassInterval);
        for (const state of due) {
          if (running.has(state.space)) continue;
          yield* store.putSpace({ ...state, nextDueAt: now + Duration.toMillis(config.backoffCap) });
          yield* enqueue(state.space, (state.lastFullPassAt ?? 0) + fullIntervalMs <= now ? "Full" : "Maintenance");
        }
      }).pipe(Effect.catch((error) => Effect.logWarning("scheduler tick failed", error)));
      yield* tick.pipe(Effect.repeat(Schedule.spaced(config.schedulerInterval)), Effect.forkScoped);

      return SpaceSyncer.of({
        watch: Effect.fn("SpaceSyncer.watch")(function* (space: string) {
          const { authority } = yield* parseSpaceRef(space);
          if (Option.isNone(yield* store.getSpace(space))) {
            const now = yield* Clock.currentTimeMillis;
            yield* store.putSpace({ space, authority, nextDueAt: now + Duration.toMillis(config.backoffCap), failures: 0 });
          }
          yield* enqueue(space, "Full");
        }),
        unwatch: Effect.fn("SpaceSyncer.unwatch")(function* (space: string) {
          yield* stop(space);
          yield* store.removeSpace(space);
        }),
        notifyWrite: Effect.fn("SpaceSyncer.notifyWrite")(function* (input: NotifyWriteInput) {
          const state = yield* store.getSpace(input.space);
          if (Option.isNone(state)) return;
          // Never trusted as a checkpoint: it only decides whether a catch-up pass is worth running.
          if (input.spaceRev && state.value.spaceRev && input.spaceRev <= state.value.spaceRev) return;
          yield* enqueue(input.space, "CatchUp");
        }),
        notifySpaceDeleted: Effect.fn("SpaceSyncer.notifySpaceDeleted")(function* (space: string) {
          if (Option.isNone(yield* store.getSpace(space))) return;
          yield* stop(space);
          yield* deleteSpaceData(space);
        }),
        getBlob: (space, did, cid) => client.getBlob(space, did, cid),
        events: Stream.fromPubSub(events),
        activeSpaces: FiberMap.size(fibers),
        awaitIdle: (space) =>
          Effect.sync(() => running.has(space) || pending.has(space)).pipe(
            Effect.repeat({ schedule: Schedule.spaced("5 millis"), while: (busy: boolean) => busy }),
            Effect.asVoid,
          ),
      });
    }),
  );
}
```

- [ ] **Step 5: Run the test**

Run: `pnpm --filter internal exec vitest run src/spaceSync/SpaceSyncer.test.ts`
Expected: 10 tests PASS. If "unwatch interrupts…" fails because `FiberMap.remove` doesn't wait for the fiber to finish, follow `FiberMap.remove` with `Fiber.await` on the fiber taken from `FiberMap.get` before removal. If "coalesces a burst…" sees more passes, check that `enqueue` doesn't fork while `running` holds the space.

- [ ] **Step 6: Commit**

```bash
git add typescript/internal/src/spaceSync/SpaceSyncer.ts typescript/internal/src/spaceSync/SpaceSyncer.test.ts typescript/internal/src/spaceSync/test/harness.ts
git commit -m "spaceSync: add SpaceSyncer with on-demand space fibers and scheduler

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Notification ingress verification

**Files:**
- Create: `typescript/internal/src/spaceSync/notification.ts`
- Test: `typescript/internal/src/spaceSync/notification.test.ts`

**Interfaces:**
- Consumes: `Identity`, `parseSpaceRef`, `NotificationAuthError`.
- Produces: `type NotificationLxm = "com.atproto.space.notifyWrite" | "com.atproto.space.notifySpaceDeleted"`, `verifyNotification(authorization: string | undefined, opts: { lxm; space; serviceDid }) → Effect<void, NotificationAuthError, Identity>`.

- [ ] **Step 1: Write the failing test**

```ts
// @vitest-environment node
import { it } from "@effect/vitest";
import { Secp256k1Keypair } from "@atproto/crypto";
import { Effect, Exit } from "effect";
import { describe, expect } from "vitest";
import { verifyNotification } from "./notification";
import { SERVICE_DID, runWithHarness } from "./test/harness";

const LXM = "com.atproto.space.notifyWrite" as const;

describe("verifyNotification", () => {
  it.live("accepts service auth from the space authority and rejects everything else", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const [alice, mallory] = yield* Effect.promise(() => Promise.all([net.createAccount("alice"), net.createAccount("mallory")]));
        const space = net.createSpace(alice);
        const opts = { lxm: LXM, space: space.ref, serviceDid: SERVICE_DID };
        const check = (token: string | undefined) => Effect.exit(verifyNotification(token && `Bearer ${token}`, opts));
        const auth = (issuer = alice, extra: Partial<Parameters<typeof net.serviceAuth>[1]> = {}) =>
          Effect.promise(() => net.serviceAuth(issuer, { aud: SERVICE_DID, lxm: LXM, ...extra }));
        const otherKey = yield* Effect.promise(() => Secp256k1Keypair.create());

        expect(Exit.isSuccess(yield* check(yield* auth()))).toBe(true);
        expect(Exit.isFailure(yield* check(undefined))).toBe(true);
        expect(Exit.isFailure(yield* check(yield* auth(mallory)))).toBe(true);
        expect(Exit.isFailure(yield* check(yield* auth(alice, { aud: "did:web:other.test" })))).toBe(true);
        expect(Exit.isFailure(yield* check(yield* auth(alice, { lxm: "com.atproto.space.notifySpaceDeleted" })))).toBe(true);
        expect(Exit.isFailure(yield* check(yield* auth(alice, { expSec: -10 })))).toBe(true);
        expect(Exit.isFailure(yield* check(yield* auth(alice, { signer: otherKey })))).toBe(true);
      }),
    ),
  );
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `pnpm --filter internal exec vitest run src/spaceSync/notification.test.ts`
Expected: FAIL. Cannot resolve `./notification`.

- [ ] **Step 3: Write `notification.ts`**

```ts
import { verifySignature } from "@atproto/crypto";
import { fromBase64 } from "@atproto/lex";
import { Clock, Effect, Schema } from "effect";
import { NotificationAuthError } from "./errors";
import { Identity } from "./Identity";
import { parseSpaceRef } from "./wire";

export type NotificationLxm = "com.atproto.space.notifyWrite" | "com.atproto.space.notifySpaceDeleted";

const ServiceAuthPayload = Schema.Struct({
  iss: Schema.String,
  aud: Schema.String,
  exp: Schema.Number,
  lxm: Schema.optional(Schema.String),
});

const decodePart = (part: string) =>
  Effect.try({
    try: () => JSON.parse(new TextDecoder().decode(fromBase64(part, "base64url"))) as unknown,
    catch: () => new NotificationAuthError({ message: "malformed jwt" }),
  });

/**
 * Verify the service-auth JWT on an inbound notifyWrite / notifySpaceDeleted:
 * issued by the space authority, addressed to us, for this method, unexpired,
 * and signed by the authority's #atproto key.
 */
export const verifyNotification = Effect.fn("verifyNotification")(function* (
  authorization: string | undefined,
  opts: { readonly lxm: NotificationLxm; readonly space: string; readonly serviceDid: string },
) {
  const fail = (message: string) => new NotificationAuthError({ message });
  const token = authorization?.match(/^Bearer (.+)$/)?.[1];
  if (!token) return yield* fail("missing bearer token");
  const parts = token.split(".");
  if (parts.length !== 3) return yield* fail("malformed jwt");
  const [head, body, sig] = parts;
  yield* decodePart(head);
  const payload = yield* decodePart(body).pipe(
    Effect.flatMap(Schema.decodeUnknownEffect(ServiceAuthPayload)),
    Effect.mapError(() => fail("malformed jwt payload")),
  );
  const { authority } = yield* parseSpaceRef(opts.space).pipe(Effect.mapError(() => fail("invalid space")));
  if (payload.iss.split("#")[0] !== authority) return yield* fail("issuer is not the space authority");
  if (payload.aud !== opts.serviceDid) return yield* fail("wrong audience");
  if (payload.lxm !== opts.lxm) return yield* fail("wrong lxm");
  const now = yield* Clock.currentTimeMillis;
  if (payload.exp * 1000 <= now) return yield* fail("token expired");
  const identity = yield* Identity;
  const { signingKey } = yield* identity.resolve(authority).pipe(Effect.mapError((e) => fail(e.message)));
  const valid = yield* Effect.promise(() =>
    verifySignature(signingKey, new TextEncoder().encode(`${head}.${body}`), fromBase64(sig, "base64url")).catch(() => false),
  );
  if (!valid) return yield* fail("bad signature");
});
```

- [ ] **Step 4: Run the test**

Run: `pnpm --filter internal exec vitest run src/spaceSync/notification.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add typescript/internal/src/spaceSync/notification.ts typescript/internal/src/spaceSync/notification.test.ts
git commit -m "spaceSync: verify service auth on inbound space notifications

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Promise facade and public exports

**Files:**
- Create: `typescript/internal/src/spaceSync/promise.ts`
- Modify: `typescript/internal/src/spaceSync/index.ts`
- Test: `typescript/internal/src/spaceSync/promise.test.ts`

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `interface PromiseSyncStore` (the `SyncStore` methods with `Promise` results; `Option` becomes `| undefined`)
  - `type PromiseRepoBatch` (the `RepoBatch` variants, but `Reset.records: AsyncIterable<VerifiedRecord>`)
  - `interface PromiseSyncSink { apply(batch: PromiseRepoBatch, signal: AbortSignal): Promise<void> }`
  - `interface PromiseDelegationSource { issue(space): Promise<string>; attestation?(space, aud): Promise<string> }`
  - `createSpaceSyncer(options: Partial<SpaceSyncOptions> & { serviceDid; store; sink; delegation }) → PromiseSpaceSyncer` with `watch`, `unwatch`, `notifyWrite(body: unknown, authorization: string | undefined)` and `notifySpaceDeleted(body, authorization)` (both decode the body and run `verifyNotification`), `getBlob`, `awaitIdle`, `events(): AsyncIterable<SyncEvent>`, `dispose()`
  - `index.ts` exports the public surface listed in the spec.

- [ ] **Step 1: Write the failing test**

```ts
// @vitest-environment node
import type { VerifiedRecord } from "@atproto/space";
import { describe, expect, it } from "vitest";
import { server } from "../test/msw";
import { type PromiseRepoBatch, type PromiseSyncStore, createSpaceSyncer } from "./promise";
import { FakeNetwork, PLC_URL } from "./test/fakeNetwork";
import { SERVICE_DID } from "./test/harness";
import type { RepoState, SpaceState } from "./types";

const memoryStore = (): PromiseSyncStore => {
  const spaces = new Map<string, SpaceState>();
  const repos = new Map<string, RepoState>();
  return {
    getSpace: async (s) => spaces.get(s),
    putSpace: async (st) => void spaces.set(st.space, st),
    removeSpace: async (s) => {
      spaces.delete(s);
      for (const k of [...repos.keys()]) if (k.startsWith(`${s}|`)) repos.delete(k);
    },
    dueSpaces: async (now, limit) => [...spaces.values()].filter((s) => s.nextDueAt <= now).slice(0, limit),
    getRepo: async (s, d) => repos.get(`${s}|${d}`),
    listRepoDids: async (s) => [...repos.values()].filter((r) => r.space === s).map((r) => r.did),
    putRepo: async (r) => void repos.set(`${r.space}|${r.did}`, r),
    removeRepo: async (s, d) => void repos.delete(`${s}|${d}`),
  };
};

describe("createSpaceSyncer", () => {
  it("syncs through Promise ports and verifies notifications", async () => {
    const net = new FakeNetwork();
    server.use(...net.handlers);
    const alice = await net.createAccount("alice");
    const space = net.createSpace(alice);
    await space.write(alice, "com.example.post", "1", { text: "a" });

    const applied: Array<{ tag: string; records?: string[] }> = [];
    const syncer = createSpaceSyncer({
      serviceDid: SERVICE_DID,
      plcUrl: PLC_URL,
      store: memoryStore(),
      delegation: { issue: (s) => net.delegationTokenFor(s) },
      sink: {
        apply: async (batch: PromiseRepoBatch) => {
          if (batch._tag === "Reset") {
            const records: VerifiedRecord[] = [];
            for await (const r of batch.records) records.push(r);
            applied.push({ tag: "Reset", records: records.map((r) => r.rkey) });
          } else {
            applied.push({ tag: batch._tag });
          }
        },
      },
    });
    try {
      await syncer.watch(space.ref);
      await syncer.awaitIdle(space.ref);
      expect(applied).toEqual([{ tag: "Reset", records: ["1"] }]);

      await space.write(alice, "com.example.post", "2", { text: "b" });
      const lxm = "com.atproto.space.notifyWrite";
      const goodAuth = `Bearer ${await net.serviceAuth(alice, { aud: SERVICE_DID, lxm })}`;
      await expect(syncer.notifyWrite(space.notifyWriteBody(alice.did), "Bearer nope")).rejects.toThrow();
      await syncer.notifyWrite(space.notifyWriteBody(alice.did), goodAuth);
      await syncer.awaitIdle(space.ref);
      expect(applied.at(-1)).toEqual({ tag: "Ops" });
    } finally {
      await syncer.dispose();
    }
  });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `pnpm --filter internal exec vitest run src/spaceSync/promise.test.ts`
Expected: FAIL. Cannot resolve `./promise`.

- [ ] **Step 3: Write `promise.ts`**

```ts
import type { VerifiedRecord } from "@atproto/space";
import { Effect, Layer, ManagedRuntime, Option, Schema, Stream } from "effect";
import { type SpaceSyncOptions, spaceSyncConfigLayer } from "./config";
import { Credentials, DelegationSource } from "./Credentials";
import { CredentialError, SinkError, StoreError, errorMessage } from "./errors";
import { Identity } from "./Identity";
import { verifyNotification } from "./notification";
import { SpaceClient } from "./SpaceClient";
import { SpaceSyncer } from "./SpaceSyncer";
import { SyncSink } from "./SyncSink";
import { SyncStore } from "./SyncStore";
import type { RepoBatch, RepoState, SpaceState, SyncEvent } from "./types";
import { NotifySpaceDeletedInput, NotifyWriteInput, lexJson } from "./wire";

export interface PromiseSyncStore {
  getSpace(space: string): Promise<SpaceState | undefined>;
  putSpace(state: SpaceState): Promise<void>;
  removeSpace(space: string): Promise<void>;
  dueSpaces(now: number, limit: number): Promise<ReadonlyArray<SpaceState>>;
  getRepo(space: string, did: string): Promise<RepoState | undefined>;
  listRepoDids(space: string): Promise<ReadonlyArray<string>>;
  putRepo(state: RepoState): Promise<void>;
  removeRepo(space: string, did: string): Promise<void>;
}

export type PromiseRepoBatch =
  | Exclude<RepoBatch, { _tag: "Reset" }>
  | (Omit<Extract<RepoBatch, { _tag: "Reset" }>, "records"> & { readonly records: AsyncIterable<VerifiedRecord> });

export interface PromiseSyncSink {
  /** Must drain Reset.records, be idempotent, and roll back if `signal` aborts. */
  apply(batch: PromiseRepoBatch, signal: AbortSignal): Promise<void>;
}

export interface PromiseDelegationSource {
  issue(space: string): Promise<string>;
  attestation?(space: string, aud: string): Promise<string>;
}

export interface CreateSpaceSyncerOptions extends Partial<SpaceSyncOptions> {
  readonly serviceDid: string;
  readonly store: PromiseSyncStore;
  readonly sink: PromiseSyncSink;
  readonly delegation: PromiseDelegationSource;
}

const storeLayer = (store: PromiseSyncStore) => {
  const wrap = <A>(f: () => Promise<A>) =>
    Effect.tryPromise({ try: f, catch: (cause) => new StoreError({ message: errorMessage(cause), cause }) });
  return Layer.succeed(
    SyncStore,
    SyncStore.of({
      getSpace: (space) => wrap(() => store.getSpace(space)).pipe(Effect.map(Option.fromNullishOr)),
      putSpace: (state) => wrap(() => store.putSpace(state)),
      removeSpace: (space) => wrap(() => store.removeSpace(space)),
      dueSpaces: (now, limit) => wrap(() => store.dueSpaces(now, limit)),
      getRepo: (space, did) => wrap(() => store.getRepo(space, did)).pipe(Effect.map(Option.fromNullishOr)),
      listRepoDids: (space) => wrap(() => store.listRepoDids(space)),
      putRepo: (state) => wrap(() => store.putRepo(state)),
      removeRepo: (space, did) => wrap(() => store.removeRepo(space, did)),
    }),
  );
};

const sinkLayer = (sink: PromiseSyncSink) =>
  Layer.succeed(
    SyncSink,
    SyncSink.of({
      apply: (batch) =>
        Effect.tryPromise({
          try: (signal) =>
            sink.apply(batch._tag === "Reset" ? { ...batch, records: Stream.toAsyncIterable(batch.records) } : batch, signal),
          catch: (cause) => new SinkError({ message: errorMessage(cause), cause }),
        }),
    }),
  );

const delegationLayer = (delegation: PromiseDelegationSource) =>
  Layer.succeed(
    DelegationSource,
    DelegationSource.of({
      issue: (space) =>
        Effect.tryPromise({
          try: () => delegation.issue(space),
          catch: (e) => new CredentialError({ space, reason: "NoDelegation", message: errorMessage(e) }),
        }),
      ...(delegation.attestation
        ? {
            attestation: (space: string, aud: string) =>
              Effect.tryPromise({
                try: () => delegation.attestation!(space, aud),
                catch: (e) => new CredentialError({ space, reason: "NotAuthorized", message: errorMessage(e) }),
              }),
          }
        : {}),
    }),
  );

/** Promise API over SpaceSyncer for hosts that don't use Effect. Call dispose() on shutdown. */
export const createSpaceSyncer = (options: CreateSpaceSyncerOptions) => {
  const { store, sink, delegation, ...config } = options;
  const configLayer = spaceSyncConfigLayer(config);
  const layer = SpaceSyncer.layer.pipe(
    Layer.provideMerge(SpaceClient.layer),
    Layer.provideMerge(Credentials.layer),
    Layer.provideMerge(Identity.layer),
    Layer.provideMerge(Layer.mergeAll(storeLayer(store), sinkLayer(sink), delegationLayer(delegation))),
    Layer.provideMerge(configLayer),
  );
  const runtime = ManagedRuntime.make(layer);
  const run = <A, E>(effect: Effect.Effect<A, E, SpaceSyncer | Identity>) => runtime.runPromise(effect);

  return {
    watch: (space: string) => run(Effect.flatMap(SpaceSyncer, (s) => s.watch(space))),
    unwatch: (space: string) => run(Effect.flatMap(SpaceSyncer, (s) => s.unwatch(space))),
    /** Pass the raw JSON body and Authorization header of an inbound notifyWrite. */
    notifyWrite: (body: unknown, authorization: string | undefined) =>
      run(
        Effect.gen(function* () {
          const input = yield* Schema.decodeUnknownEffect(NotifyWriteInput)(lexJson(body));
          yield* verifyNotification(authorization, { lxm: "com.atproto.space.notifyWrite", space: input.space, serviceDid: options.serviceDid });
          yield* Effect.flatMap(SpaceSyncer, (s) => s.notifyWrite(input));
        }),
      ),
    notifySpaceDeleted: (body: unknown, authorization: string | undefined) =>
      run(
        Effect.gen(function* () {
          const { space } = yield* Schema.decodeUnknownEffect(NotifySpaceDeletedInput)(body);
          yield* verifyNotification(authorization, { lxm: "com.atproto.space.notifySpaceDeleted", space, serviceDid: options.serviceDid });
          yield* Effect.flatMap(SpaceSyncer, (s) => s.notifySpaceDeleted(space));
        }),
      ),
    getBlob: (space: string, did: string, cid: string) => run(Effect.flatMap(SpaceSyncer, (s) => s.getBlob(space, did, cid))),
    awaitIdle: (space: string) => run(Effect.flatMap(SpaceSyncer, (s) => s.awaitIdle(space))),
    events: (): AsyncIterable<SyncEvent> => ({
      [Symbol.asyncIterator]: () =>
        Stream.toAsyncIterable(Stream.unwrap(Effect.map(SpaceSyncer, (s) => s.events)).pipe(Stream.provide(runtime)))[Symbol.asyncIterator](),
    }),
    dispose: () => runtime.dispose(),
  };
};

export type PromiseSpaceSyncer = ReturnType<typeof createSpaceSyncer>;
```

If `Stream.provide(runtime)` doesn't typecheck in v4, implement `events` as follows instead. Get the `SpaceSyncer` once with `runtime.runPromise(SpaceSyncer.asEffect())` (or `run(Effect.map(SpaceSyncer, (s) => s))`), then return `Stream.toAsyncIterable(syncer.events)` from an async generator that awaits that promise first.

- [ ] **Step 4: Replace `index.ts` with the full public surface**

```ts
export * from "./config";
export { Credentials, DelegationSource, type SpaceCredential } from "./Credentials";
export * from "./errors";
export { Identity, type ResolvedIdentity } from "./Identity";
export { type NotificationLxm, verifyNotification } from "./notification";
export {
  type CreateSpaceSyncerOptions,
  type PromiseDelegationSource,
  type PromiseRepoBatch,
  type PromiseSpaceSyncer,
  type PromiseSyncSink,
  type PromiseSyncStore,
  createSpaceSyncer,
} from "./promise";
export { SpaceClient, type SpaceCallError } from "./SpaceClient";
export { SpaceSyncer } from "./SpaceSyncer";
export { SyncSink } from "./SyncSink";
export { SyncStore } from "./SyncStore";
export * from "./types";
export { NotifySpaceDeletedInput, NotifyWriteInput, parseSpaceRef } from "./wire";
```

- [ ] **Step 5: Run the full suite, typecheck, lint, format**

Run: `pnpm --filter internal exec vitest run src/spaceSync && pnpm --filter internal exec tsc --noEmit -p . && moon run internal:lint && moon run internal:format`
Expected: all spaceSync tests PASS. No type errors. Lint is clean (fix any oxlint findings in `src/spaceSync`). Formatting is applied.

- [ ] **Step 6: Run the whole internal package's tests (regression check)**

Run: `moon run internal:test`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add typescript/internal/src/spaceSync
git commit -m "spaceSync: add Promise facade and public exports

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

## Spec coverage map

| Spec item | Task |
|---|---|
| Identity fallbacks / malformed entries | 2 |
| Delegation → credential exchange, key binding, cache by `exp`, invalidate, typed reasons | 4 |
| Signed XRPC, audience per call, retries, re-mint on JwtExpired/CredentialRevoked, SpaceDeleted mapping | 5 |
| SyncStore / SyncSink ports, memory store | 6 |
| Incremental sync, commit verify, hash match, rev check, recovery via CAR, at-least-once commit | 7 |
| listRepos walk validation, dedupe, concurrency, pruning, checkpoint-before-failure, registration renewal, backoff | 8 |
| On-demand fibers, coalescing, global permits, scheduler with lease, delete/unwatch, events, idle cleanup | 9 |
| verifyNotification | 10 |
| Promise facade, public surface | 11 |

# Space sync consumer (`internal/spaceSync`) — design

Status: approved in conversation 2026-10-08, pending written-spec review.

## Goal

A TypeScript library implementing the **syncer** (consumer) side of the atproto
spaces sync protocol from
[proposal 0016](https://github.com/bluesky-social/proposals/blob/main/0016-permissioned-data/README.md):
obtain space credentials, enumerate a space's writer set, keep a verified local
copy of each writer's space repo up to date (incremental `listRepoOps` with
full-state `getRepo` recovery), and react to write / space-deleted
notifications.

### Context and constraints

- **Consumer:** a long-running Node server-side syncer that holds users' OAuth
  sessions, receives `notifyWrite` / `notifySpaceDeleted` over HTTP, and
  persists state durably.
- **Scale:** tens of thousands of tracked spaces per process. Memory and timers
  must scale with *active* spaces, not tracked ones.
- **Location:** `typescript/internal/src/spaceSync/`, exported as the
  `internal/spaceSync` subpath of the `internal` workspace package.
- **Concurrency:** Effect v4 (`effect@^4`), following
  [Effect's LLMS.md](https://github.com/Effect-TS/effect/blob/main/LLMS.md):
  `Context.Service` services + `Layer`s, `Effect.fn` / `Effect.fnUntraced`,
  `Schema.TaggedError`, `Schema` for decoding untrusted data, `Predicate` for
  guards.
- **Crypto/verification is not reimplemented.** `@atproto/space` (spaces alpha
  `0.0.0-spaces-alpha-20261001173819`, already on main) provides `RepoCommit`,
  `verifyCommit`, `verifyRepoCar`, `createSpaceSigHeaders`, `parseSpaceToken`.
  This library is orchestration on top of it.
- **Reference:** `../bulletin/lib/sync/engine.ts` implements the same flow with
  hand-rolled promise queues, generation counters and `setTimeout` timers. This
  library replaces those with fibers, semaphores, interruption and a durable
  schedule, and removes all app-specific (bulletin record) logic.

### Success criteria

- A host can `watch` a space and receive a verified, at-least-once stream of
  repo batches that converges to the authoritative state of every writer repo.
- Notifications, scheduler ticks and watches for the same space never run
  concurrent passes; distinct spaces run in parallel up to `maxActiveSpaces`.
- Idle tracked spaces hold no fiber, queue, timer or credential in memory.
- All protocol verification failures fall back to recovery; nothing unverified
  reaches the sink.

### Out of scope (v1)

- An HTTP server. The host mounts routes and calls the syncer (a
  `verifyNotification` helper is provided).
- Automatic blob mirroring. `getBlob` is exposed for on-demand use.
- `#account` / `#identity` firehose handling.
- The proposal's "light healing" (`listRecords` with `excludeValues` + diff +
  `getRecord`). Recovery always resets the repo via `getRepo`, because diffing
  needs the host's record index.
- Client attestation *generation*. A host may supply an attestation hook; the
  library only forwards it.

## Architecture

```
            host HTTP routes                      host config
   notifyWrite / notifySpaceDeleted       DelegationSource  SyncStore  SyncSink
                 │                                   │         │          │
                 ▼                                   ▼         ▼          ▼
 ┌──────────────────────────── SpaceSyncer ────────────────────────────────┐
 │ watch / unwatch / notifyWrite / notifySpaceDeleted / getBlob / events   │
 │                                                                        │
 │  Scheduler fiber ──dueSpaces──► enqueue(space, trigger)                 │
 │                                       │                                │
 │                    FiberMap<space, SpaceFiber>  (on demand)            │
 │                    Semaphore(maxActiveSpaces)                          │
 │                                       │                                │
 │                 SpacePass ── RepoSync (concurrency N per pass)          │
 │                     │             │                                    │
 │               Credentials     SpaceClient ── Identity                  │
 └────────────────────────────────────────────────────────────────────────┘
```

### Units

| Unit | Kind | Responsibility |
|---|---|---|
| `Identity` | service, default layer | Resolve a DID to its PDS endpoint, `#atproto` key, and space host endpoint + space key (`#atproto_space_host` / `#atproto_space` with fallbacks to `#atproto_pds` / `#atproto`; a present-but-malformed entry is an error, not a fallback). Default layer uses `@atproto/identity`'s `IdResolver`. |
| `DelegationSource` | service, **host-provided** | `issue(space) → Effect<string, CredentialError>`: returns a delegation token from `com.atproto.space.getDelegationToken` using whichever OAuth session the host chooses (e.g. authority first, then other members). Optional `attestation(space, aud)` hook returning a client-attestation JWT. |
| `Credentials` | service | Bounded `Cache` keyed by space, TTL tied to the credential's `exp` (refresh 30 s before). On miss: fresh `P256Keypair`, `DelegationSource.issue`, `getSpaceCredential` to the authority's space host with the `atproto-space` HTTP signature over `("authorization")`. Exposes `get(space)` and `invalidate(space)`. |
| `SpaceClient` | service | Signed XRPC calls: `listRepos`, `registerNotify`, `listRepoOps`, `getRepo` (streaming body), `getBlob`. Uses `@atproto/lex-client` `Client` with the generated `api` lexicon bindings and a signing `fetch` that sets `Authorization: Atproto-Space <cred>`, `Atproto-Space-Audience` (repo DID for repo calls, authority DID for host calls) and the signature headers. Each call is `Effect.tryPromise` with the fiber's `AbortSignal`. Maps error bodies to `XrpcError`. |
| `RepoSync` | internal module | Sync one repo (incremental, else recovery), deliver one batch to the sink, then persist repo state. |
| `SpacePass` | internal module | One reconcile pass over a space (credential, registration, `listRepos` walk, repo fan-out, pruning, checkpoint). |
| `SpaceSyncer` | service, public | Trigger intake, on-demand per-space fibers, scheduler, event `PubSub`. |
| `SyncStore` | service, **host-provided** | Durable per-space and per-repo state. In-memory layer shipped for tests/ephemeral use. |
| `SyncSink` | service, **host-provided** | Applies verified batches to the host's index. |

A Promise facade `createSpaceSyncer(options)` builds a `ManagedRuntime` from the
layers and returns `{ watch, unwatch, notifyWrite, notifySpaceDeleted, getBlob,
events (AsyncIterable), dispose }` for non-Effect hosts. Ports may be given as
Effect layers or as Promise-returning objects (adapted with `Effect.tryPromise`).

## Ports

```ts
interface SpaceState {
  space: SpaceRef            // at://{authority}/space/{type}/{skey}
  authority: Did
  spaceRev?: string          // last fully processed listRepos checkpoint
  registrationExpiresAt?: DateTime.Utc
  nextDueAt: DateTime.Utc
  lastFullPassAt?: DateTime.Utc
  failures: number
  lastError?: string
}

interface RepoState {
  space: SpaceRef
  did: Did
  rev: string                // TID of the last verified commit
  ltHash: Uint8Array         // 2048-byte LtHash state
}

SyncStore {
  getSpace(space): Effect<Option<SpaceState>, StoreError>
  putSpace(state): Effect<void, StoreError>
  removeSpace(space): Effect<void, StoreError>          // also drops repo states
  dueSpaces(now, limit): Effect<SpaceState[], StoreError> // nextDueAt <= now, oldest first
  getRepo(space, did): Effect<Option<RepoState>, StoreError>
  listRepoDids(space): Effect<Did[], StoreError>
  putRepo(state): Effect<void, StoreError>
  removeRepo(space, did): Effect<void, StoreError>
}

SyncSink {
  apply(batch: RepoBatch): Effect<void, SinkError>
}

type RepoBatch =
  | { _tag: "Ops"; space; did; rev; changes: Change[] }
  | { _tag: "Reset"; space; did; rev; records: Stream<VerifiedRecord, RepoVerificationError> }
  | { _tag: "RepoRemoved"; space; did }
  | { _tag: "SpaceDeleted"; space }

type Change = {
  uri: string                // full space record URI
  collection: Nsid
  rkey: string
  cid: Cid | null            // null = delete
  value?: LexMap             // present for create/update when inlined
}
```

`Reset` means "replace this repo's contents with exactly these records". The
sink **must drain** `records`; draining is what verifies each block against the
index, and a verification failure surfaces as a stream error that fails
`apply`.

Delivery is **at-least-once**: repo state is persisted only after `apply`
succeeds. A crash between the two replays the batch from the previous `rev`;
sinks key on `uri` + `cid` so replays are idempotent.

## Data flow

### Triggers

Every input becomes `enqueue(space, trigger)`. If the space has no fiber in the
`FiberMap`, one is forked; the fiber first acquires a permit from
`Semaphore(maxActiveSpaces)`.

| Trigger | Source | Effect |
|---|---|---|
| `Full` | `watch(space)`; scheduler when `lastFullPassAt` is older than `fullPassInterval` (default 6 h) | Insert `SpaceState` if new; pass walks `listRepos` from the start and prunes. |
| `Maintenance` | scheduler (`nextDueAt <= now`) | Catch-up pass; renews registration if due. |
| `CatchUp` | `notifyWrite` | Dropped up front if `spaceRev <= stored.spaceRev` or the space is not tracked; otherwise a catch-up pass from the stored checkpoint. The notification itself is never used as a checkpoint, so `prevSpaceRev` gaps and out-of-order delivery self-heal. |
| `Delete` | `notifySpaceDeleted`, or `SpaceDeleted` from any call | Interrupt the fiber, `sink.apply(SpaceDeleted)`, `removeSpace`. |
| `Unwatch` | `unwatch(space)` | Interrupt the fiber, `removeSpace`; no sink event. |

The **scheduler** is a single fiber that, every `schedulerInterval` (default
30 s), pages through `dueSpaces(now, pageSize)` and enqueues `Maintenance` or
`Full` for each.

### Space fiber

The fiber drains its `Queue`, folds all pending triggers into the strongest
(`Full > Maintenance > CatchUp`), runs one `SpacePass`, and repeats until the
queue is empty. It then exits, releasing its permit and its `FiberMap` entry.
Idle spaces therefore cost only their store rows.

### Space pass

1. `Credentials.get(space)`.
2. If `registrationExpiresAt` is missing or within `registrationRenewLead`
   (default 1 h), call `registerNotify` on the space host with the configured
   `serviceDid`; store the new expiry.
3. Walk `listRepos` (limit 1000) from `spaceRev`, or from the start for `Full`.
   Pages are validated: entries strictly increasing by `spaceRev`, and each
   non-empty page's `cursor` equals its last entry's `spaceRev`. Walking stops
   at the first empty page.
4. For each listed writer whose `repoRev` is newer than the stored `rev` (or
   that has no stored state), run `RepoSync`. Repos are deduplicated within the
   pass (a writer may appear on several pages; its latest `repoRev` wins) and
   synced with `Effect.forEach(..., { concurrency: repoConcurrency })`
   (default 8).
5. `Full` only: every DID in `listRepoDids(space)` absent from the listing gets
   `sink.apply(RepoRemoved)` then `removeRepo`.
6. Persist `spaceRev` = the last listed `spaceRev` **before the first failed
   repo** (all of it if none failed), reset `failures` on success, set
   `lastFullPassAt` for `Full`, and set `nextDueAt` to the earliest of
   registration renewal and the next full pass.

### Repo sync

**Incremental** (stored state exists):

1. Page `listRepoOps(space, repo, since = rev)` until no cursor, applying every
   op to `RepoCommit.fromState(ltHash)` and collecting `Change`s (later ops on
   the same path supersede earlier ones in the batch).
2. The final page must carry a commit; decode it with `Schema`.
3. Require `verifyCommit(commit, { space, author: did, rev: commit.rev },
   didKey)`, `state.matches(commit)`, and `commit.rev >= listedRepoRev`.
4. `uninterruptible(sink.apply(Ops) *> putRepo({ rev: commit.rev, ltHash }))`.

**Recovery**: used when there is no stored state, `since` is unknown
(`listRepoOps` error), there is no final commit, or any step 3 check fails.

1. Stream `getRepo(space, repo)` into `verifyRepoCar(body, { space, author,
   didKey })`. This verifies the commit and that the index matches its hash.
2. Require `commit.rev >= listedRepoRev`.
3. `uninterruptible(sink.apply(Reset{ records }) *> putRepo({ rev, ltHash:
   repo.setHash.state() }))`. The CAR reader is disposed via `Scope` whatever
   happens.

`didKey` comes from `Identity` (the repo author's `#atproto` key).

## Errors and retries

All errors are `Schema.TaggedError`:

- `CredentialError { reason: "SpaceDeleted" | "SpaceNotFound" | "NotAuthorized" | "NoDelegation" | "Transport", message }`
- `XrpcError { method, status, error?, message? }`
- `RepoVerificationError { space, did, message }` (internal; triggers recovery)
- `RepoSyncError { space, did, cause }` (recovery itself failed)
- `SinkError`, `StoreError` (host ports)
- `NotificationAuthError` (`verifyNotification`)

| Situation | Behavior |
|---|---|
| Transport error / HTTP 5xx on an XRPC call | `Schedule.exponential("500 millis")` with jitter, 3 attempts, per call. |
| `JwtExpired` / `CredentialRevoked` from a host | `Credentials.invalidate(space)`, re-mint, retry the call once. |
| `SpaceDeleted` anywhere | Handled as the `Delete` trigger. |
| `NotAuthorized` / `SpaceNotFound` / `NoDelegation` | Per the proposal this says nothing about the space: keep data, record `lastError`, back off. Unwatching is the host's decision. |
| A single repo fails (`RepoSyncError`, `SinkError`, `XrpcError`) | The pass continues with other repos; the checkpoint rule in step 6 ensures the failed repo is retried. |
| Whole pass fails | `failures++`, `lastError`, `nextDueAt = now + min(base * 2^failures, cap)` (defaults 30 s, 1 h). |

**Interruption:** `unwatch` and space deletion interrupt the space fiber.
HTTP calls (via `AbortSignal`) and CAR streaming are interruptible. The
`sink.apply` + `putRepo` commit is `uninterruptible`, so interruption waits for
an in-flight commit instead of leaving sink and store out of step.

## Notification ingress helper

`verifyNotification(authorization, { lxm, space, serviceDid })` verifies the
service-auth JWT on an inbound `notifyWrite` / `notifySpaceDeleted`: `iss` (DID
part) equals the space authority, `aud` equals `serviceDid`, `lxm` matches, not
expired, and the signature verifies against the authority's `#atproto` key
from `Identity`. Request bodies are decoded with `Schema` (bytes from
`{$bytes}`).

## Configuration

```ts
interface SpaceSyncerConfig {
  serviceDid: string            // our service identifier for registerNotify / aud checks
  maxActiveSpaces: number       // default 64
  repoConcurrency: number       // default 8
  schedulerInterval: Duration   // default 30 s
  fullPassInterval: Duration    // default 6 h
  registrationRenewLead: Duration // default 1 h
  backoff: { base: Duration; cap: Duration } // default 30 s / 1 h
  credentialCacheCapacity: number // default 1024
}
```

## Observability

Public operations use `Effect.fn("SpaceSyncer.<op>")` for spans. Internal hot
paths use `Effect.fnUntraced`. Logs are annotated with `space` and `did`. Time
uses `Clock` / `DateTime` so `TestClock` controls it.

## Public surface (`internal/spaceSync`)

- Services and layers: `SpaceSyncer` (+ `SpaceSyncer.layer`), `SyncStore`,
  `SyncSink`, `DelegationSource`, `Identity` (+ default layer),
  `SpaceSyncerConfig`.
- `SyncStore.memory` layer.
- `createSpaceSyncer` Promise facade.
- `verifyNotification`.
- Types: `RepoBatch`, `Change`, `SpaceState`, `RepoState`, the error classes.
- `SpaceSyncer.events: Stream<RepoBatch>`, a `PubSub`-backed fan-out of
  successfully applied batches (e.g. for SSE), published after the store
  commits.

## Files

```
typescript/internal/src/spaceSync/
  index.ts           public exports
  errors.ts          tagged errors
  schema.ts          Schema decoders for XRPC outputs / notifications / commits
  config.ts          SpaceSyncerConfig (Context.Reference with defaults)
  Identity.ts        Identity service + default layer
  Credentials.ts     DelegationSource port + Credentials service
  SpaceClient.ts     signed XRPC client
  SyncStore.ts       port + memory layer
  SyncSink.ts        port
  repoSync.ts        incremental + recovery
  spacePass.ts       listRepos walk, fan-out, pruning, checkpoint
  SpaceSyncer.ts     triggers, FiberMap, scheduler, events
  notification.ts    verifyNotification
  promise.ts         createSpaceSyncer facade
  test/fakeSpaceHost.ts   in-memory space+repo host exposed as msw handlers
  *.test.ts
```

`typescript/internal/package.json` gains the `./spaceSync` export plus
dependencies `effect`, `@atproto/crypto`, `@atproto/lex-client`,
`@atproto/identity` (already present), `@atproto/space` (already present), and
dev dependency `@effect/vitest`.

## Testing

- `@effect/vitest` `it.effect`, with `// @vitest-environment node` per file (the
  package defaults to jsdom).
- **Network mocking uses msw.** `test/fakeSpaceHost.ts` holds in-memory
  writers, space metadata and an oplog, and exposes msw `http` handlers for
  `getSpaceCredential`, `listRepos`, `registerNotify`, `listRepoOps`, `getRepo`
  and `getBlob`, plus DID-document resolution (PLC / `did:web`). Tests register
  them with `server.use(...fake.handlers)` on the shared server from
  `src/test/msw.ts` (unhandled requests error). The handlers use the real
  `@atproto/space` code (`createSpaceToken`, `verifySpaceSignature`,
  `RepoCommit`, `serializeRepo`), so tests exercise real crypto and CAR
  verification. Failure knobs: drop oplog, corrupt commit, return
  `SpaceDeleted`, expire or revoke credentials, delay responses.
- `TestClock` drives the scheduler, backoff, registration renewal and
  credential expiry.
- Cases:
  1. `watch` performs the initial full sync (recovery path) and emits `Reset`.
  2. A subsequent write and `notifyWrite` produce an incremental `Ops` batch.
  3. Recovery when the hash doesn't match, the oplog is truncated, or the commit is invalid.
  4. A stale or out-of-order `notifyWrite` is dropped; a `spaceRev` gap is caught up.
  5. A full pass prunes a removed writer (`RepoRemoved`).
  6. Space deletion via notification and via a `getSpaceCredential` `SpaceDeleted` error.
  7. Credential refresh on expiry; re-mint and retry on `CredentialRevoked`.
  8. A partial pass failure leaves the checkpoint before the failed repo.
  9. A `SinkError` does not advance the store; the next pass replays.
  10. `unwatch` interrupts an in-flight pass but not a committing batch.
  11. `maxActiveSpaces` is respected under a burst of 100 spaces.
  12. Idle spaces leave no `FiberMap` entry.
  13. The scheduler picks up due spaces and renews registration.
  14. `verifyNotification` accept and reject cases.

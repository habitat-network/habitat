# Chalk on Durable Objects

## Problem

Chalk today is a single Node process holding all sync state in module globals
(`typescript/apps/chalk/src/server/functions.server.ts`):

- one `DocSync` websocket to sap's `/channel`,
- an in-memory `Map<spaceUri, Y.Doc>`,
- an in-process `DocPubSub` fanning merged snapshots to `subscribeDoc` streams,
- two `DebounceQueue`s of `setTimeout` timers, and
- a local sqlite `DocStore`.

Every one of these is correct only within one process. The app cannot run more
than one instance: sap's outbox is single-consumer (`pkg/sap/outbox` — "only one
consumer should drain it at a time"), so two `DocSync` connections starve each
other, and two processes hold divergent `Y.Doc`s for the same space. Pending
debounced edits are lost on restart. `functions.server.ts` carries a
`globalThis.__chalkSingletons` pin and an `import.meta.hot.dispose` hook purely
to stop Vite's SSR dev server from creating a second `DocSync` inside one
process.

## Goal

Move chalk to Cloudflare Workers, with Durable Objects owning both the sap
subscription and per-document state, so the app scales past one process and
pending work survives eviction.

## Architecture

Six components.

### Worker (entry)

TanStack Start's Cloudflare target. Serves routes, SSR, and server functions.
Holds no state; `env` bindings replace every module global. The
`createSingletons()` / `globalThis.__chalkSingletons` / `import.meta.hot.dispose`
block in `functions.server.ts` is deleted — it exists only to work around Vite
re-evaluating a module within one Node process, which is not a condition that
occurs here.

### `SapChannel` DO (singleton, `idFromName("default")`)

Owns the outbound WebSocket to sap's `/channel`. Its whole job:

1. parse the space-record URI with `parseSpaceRecordUri` (moved as-is),
2. filter to collection `network.habitat.docs.crdt`,
3. look up the doc in D1 for its `ownerDid`, skipping unknown docs exactly as
   today's `store.docsByUris` miss does,
4. `await stub.applyRemote({ spaceUri, ownerDid }, cid)` on `DOC.idFromName(spaceUri)`,
5. ack sap.

This is `docSync.ts`'s `run` / `connectOnce` / `handleRawMessage` moved intact,
including the reconnect loop.

Ack-after-success means a failed merge is redelivered by sap, which is safe:
Yjs updates are idempotent, a fact `docSync.ts` already relies on for its own
republish round trip. Messages deliberately ignored (wrong collection, unknown
doc, missing blob ref) are acked immediately so one uninteresting message cannot
wedge the outbox.

**Lifecycle.** Workers have no module-load moment to call `docSync.start()`
from. An open outbound WebSocket keeps the DO resident, but once it closes the
DO can be evicted with nothing left to wake it. Two mechanisms:

- a Workers Cron trigger (every minute) calls `ensureConnected()`, which no-ops
  when the socket is live — this is what guarantees recovery from eviction;
- on close, an alarm `RECONNECT_DELAY_MS` out reconnects from `alarm()` — this
  only makes reconnects fast rather than up-to-a-minute.

### `DocRoom` DO (one per space URI, `idFromName(spaceUri)`)

Owns one document: the `Y.Doc` in memory, its CRDT snapshot in DO SQLite, the
set of connected subscriber streams, the member-edit debounce, and the
owner-republish debounce.

```ts
class DocRoom extends DurableObject<Env> {
  // ctor: blockConcurrencyWhile(() => migrate + load snapshot and identity from DO SQLite)
  applyEdit(id: DocIdentity, memberDid: string, update: Uint8Array): Promise<void>
  applyRemote(id: DocIdentity, cid: string): Promise<void>
  fetch(req): Response   // GET /subscribe -> streaming body
}

interface DocIdentity { spaceUri: string; ownerDid?: string }
```

A DO cannot read back the name it was addressed by, and both paths need the
space URI — `applyRemote` to call `getBlob`, the flush path to call
`putRecord`. So every caller passes a `DocIdentity`, which the room persists to
its own storage on first sight and thereafter uses for alarm-driven work, when
no caller is present to supply it. Callers always have it: `sendEdit`'s `docId`
*is* the space URI (see `createDoc`), and `SapChannel` has both halves from its
D1 lookup. This also means `sendEdit` still works for a doc this deployment's
D1 has never seen — a shared doc — which is a property today's code has and is
worth preserving. `ownerDid` is optional for exactly that case: the member-edit
flush writes to the *member's own* repo and never needs it, while the
owner-republish flush is skipped when it is unknown — mirroring
`republishCanonical`'s existing `if (!doc) return;`. Once any path supplies it
(a `SapChannel` delivery, or `createDoc`), it is persisted and republish
begins.

`applyEdit` and `applyRemote` converge on one private `mergeUpdate`, which keeps
today's comment explaining why the seed is the persisted merged state rather
than a bare `new Y.Doc()` — incoming bytes are an incremental diff, so merging
into an empty doc would persist a fragment over the real content. That hazard is
unchanged. `applyRemote` performs the `getBlob` dereference that
`handleOutboxMessage` does today.

Absorbs `DocSync.mergeUpdate` / `applyEdit` / `republishCanonical`, all of
`pubsub.ts`, and the per-doc half of `docStore.ts`.

**Subscription.** `stub.fetch("/subscribe")` returns a `Response` whose body is
a `ReadableStream`; the `subscribeDoc` server fn reads it and yields each chunk,
so `useYDoc` is unchanged. The DO holds the live writers — this is `DocPubSub`
without the hand-rolled async-iterator queue, since a stream already provides
backpressure. Emitting the initial snapshot inside the DO also closes a race
that exists today, where the snapshot read and the `pubsub.subscribe`
registration are two separate steps with a window between them.

**Alarms.** A DO has exactly one alarm timestamp, but `DocRoom` needs two
independent debounces — the per-member edit flush and the owner-republish
flush — each with an idle and a max-wait deadline, so up to *2N+2* pending
deadlines. `setTimeout` is not usable: it does not survive eviction, and a DO
with no open connections will be evicted. So `DebounceQueue` is rewritten
against DO storage rather than ported:

- a `pending_flush(kind, member_did, first_push_at, idle_deadline)` table in DO
  SQLite, plus the queued update bytes for member flushes;
- every push recomputes `min(idle_deadline, first_push_at + maxWait)` across all
  rows and calls `ctx.storage.setAlarm` with it;
- `alarm()` runs every due row, then re-arms to the next minimum.

The 2s idle / 10s max-wait policy constants carry over unchanged. Because
pending updates are now in storage rather than in a timer closure, pending edits
survive eviction — a behavior improvement over today, where they are lost on
process restart.

### D1 + drizzle

The `docs` index table only: `space_uri` (PK), `doc_id` (unique), `owner_did`,
`title`, `updated_at` — serving `docsForOwner` and `docsByUris`. Written by
`createDoc` and by `DocRoom` when a republish changes the title.

`src/db/schema.ts` replaces the `todos` template stub with this table;
`src/db/index.ts` switches from `drizzle-orm/better-sqlite3` to
`drizzle-orm/d1`, taking the binding from `env` rather than
`process.env.DATABASE_URL`. A separate schema module defines the DO-side tables
(`doc_crdt`, `pending_flush`) against `drizzle-orm/durable-sqlite`, so both
tiers share one query idiom. D1 uses drizzle-kit migrations; the DO side runs
drizzle's migration helper inside `blockConcurrencyWhile`.

### `SapClient`

Nearly unchanged — it is already only `fetch`. The one change: URLs and secrets
arrive as constructor arguments from `env` instead of being read from
`process.env` globals.

### Client

`useYDoc` is untouched. `subscribeDoc` and `sendEdit` keep their signatures;
only their handler bodies change.

## Deleted

- `pubsub.ts` — the DO's own connection set replaces it.
- `DocSync`'s `private queue: Promise<void>` serialization chain — a DO instance
  is single-threaded, so per-doc ordering is a platform guarantee rather than
  something to hand-roll.

## Server functions

All four keep their signatures.

- `createDoc` — sap calls unchanged; `store.upsertDoc` becomes a drizzle D1
  insert. Also gets the room's stub once so it exists with its owner recorded.
- `listDocs` — one drizzle `select().where(eq(docs.ownerDid, did))`.
- `sendEdit` — `env.DOC.get(idFromName(data.docId)).applyEdit({ spaceUri: data.docId }, did, data.update)`.
  This collapses today's two steps (`docSync.applyEdit` for immediate fanout
  plus `memberEditQueue.push` for repo writeback) into one RPC, because both
  halves now live in the same object.
- `subscribeDoc` — `stub.fetch("/subscribe")`, then `for await` over the body,
  yielding each chunk.

## Configuration and dev loop

New `wrangler.jsonc`: DO bindings with `new_sqlite_classes` migrations, the D1
binding, the cron trigger, vars for the sap URLs, and `CHALK_SESSION_SECRET` as
a secret. `vite.config.ts` adds `@cloudflare/vite-plugin` and points
`tanstackStart` at the Cloudflare target. Most of `moon.yml`'s env block becomes
`wrangler.jsonc` vars; `CHALK_DB` goes away.

`vite dev` becomes wrangler-backed dev on workerd instead of Node:

- The port stays 5177, so the existing `Caddyfile` entry and `CHALK_BASE_URL`
  keep working, and `moon chalk:dev`'s deps on `pear:dev` / `sap:dev` /
  `root:caddy` are unchanged.
- `NODE_OPTIONS: --use-system-ca` drops out. It exists because Caddy's `tls
  internal` cert is not publicly trusted, and workerd would not honor the system
  CA store anyway. This works out because sap traffic targets
  `http://127.0.0.1:2581` (plain HTTP, not via Caddy), which workerd reaches
  fine. Verify this early: any path that does reach a `*.local.habitat.network`
  HTTPS URL from inside the Worker will fail in a way that does not reproduce in
  production.
- D1 and DO state are emulated locally by miniflare, so dev needs no cloud
  resources.

## Testing

Vitest stays; the pool becomes `@cloudflare/vitest-pool-workers` for anything
touching a DO, since `ctx.storage` and alarms cannot be exercised in plain Node.

| Existing file | Fate |
|---|---|
| `sapClient.test.ts` | survives roughly as-is |
| `session.test.ts` | survives roughly as-is |
| `functions.server.test.ts` | rewritten against bindings |
| `docSync.test.ts` | splits into a `SapChannel` routing test and a `DocRoom` merge test; its `parseSpaceRecordUri` cases move intact |
| `docStore.test.ts` | splits along the D1 / DO-storage line |
| `pubsub.test.ts` | deleted — module ceases to exist |
| `debounceQueue.test.ts` | deleted — behavior re-tested through `DocRoom`'s alarm handler, using vitest-pool-workers' alarm time control |

## Out of scope

Recorded so they are not lost:

1. **sap reachability and authentication.** Today `CHALK_SAP_INTERNAL_URL` is
   `http://127.0.0.1:2581`; chalk and sap are colocated, and sap's
   `/proxy/<nsid>` authenticates purely on a `Habitat-Did` header with no
   credential, which is safe only because it is loopback-only. On Workers
   nothing is colocated, so every `SapClient` call and the `SapChannel`
   websocket become public-internet calls. **This design is not deployable to
   real Workers until that is solved** (shared secret, or a Cloudflare Tunnel to
   sap). Explicitly deferred by decision; not an oversight.
2. Migrating existing `.chalk/chalk.db` data.
3. Switching the client to a direct DO WebSocket (with hibernation, and possibly
   the standard y-protocols sync). Deferred deliberately: with streamed server
   fns, each connected client keeps both a Worker invocation and the doc DO
   resident for the life of the stream, and hibernation is WebSocket-only. This
   remains available later because it changes nothing inside `DocRoom`.

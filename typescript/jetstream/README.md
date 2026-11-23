# @habitat-network/jetstream

Follow every space an org can read as **one** event stream with **one** cursor,
and write records that are known to be indexed when the call returns.

It is a client of pear's `network.habitat.space.subscribe` (Habitat's Jetstream
for spaces), built on [Effect](https://effect.website). Its API follows the
`@bsky/jetstream` spaces mode sketched in
[Syncing spaces](https://atproto.com/blog/space-sync), so either side can be
swapped for the official package once it ships.

```ts
import { Jetstream, LexIndexer } from "@habitat-network/jetstream";
import { network } from "api";

// The org's OAuth session is the credential: the stream carries every space
// the org may read that the session's space: scopes cover.
const js = new Jetstream({
  service: "https://pear.example",
  mode: "spaces",
  fetch: (input, init) => orgSession.fetchHandler(input, init),
});

const indexer = new LexIndexer({ mode: "spaces" })
  .commit(network.habitat.drop.file, {
    put: async (e) =>
      db.upsertFile({ uri: e.uri, space: e.space, name: e.record.name }),
    del: async (e) => db.deleteFile(e.uri),
  })
  .spaceDeleted(async (e) => db.deleteSpace(e.space));

const runner = js.runner(indexer, {
  cursor: { get: () => db.getCursor(), set: (seq) => db.setCursor(seq) },
});
void runner.replay(); // from the saved cursor, else 0, then live

// Writes return once the runner has indexed them: no optimistic update.
await Effect.runPromise(
  runner.putRecord(
    {
      space,
      repo: memberDid,
      collection: "network.habitat.drop.file",
      rkey: "self",
      record,
    },
    { fetch: memberSession.fetchHandler.bind(memberSession) },
  ),
);
```

## Running an Effect Sink

`js.runner()` also takes any Effect `Sink` over the stream's events, in place
of a `LexIndexer`. The sink is run over each group of events (one event unless
`batch` says otherwise), so it can write a group in one transaction. Name the
collections to ask for with `collections`.

```ts
const runner = js.runner(
  Sink.forEachArray((events: ReadonlyArray<SpaceEvent>) => writeAll(events)),
  {
    collections: ["network.habitat.drop.file"],
    batch: { size: 50, window: "20 millis" },
  },
);
```

A failing sink is run again over the same events, with backoff, before the
cursor moves, so it must be idempotent. `LexIndexer#sink()` is an indexer as a
Sink.

## Semantics

- **Handlers** may return an Effect, a promise or nothing. They run one event
  at a time; the cursor advances only after an event's handlers all succeed,
  and a failing handler is retried with backoff rather than skipped. Delivery
  is at-least-once, so key handlers on `e.uri`.
- **Reconnects** (a dropped socket, or the server's `ExpiredToken` close)
  resume from the seq after the last event emitted, with backoff. A refused
  request (4xx other than 401/408/429) or a malformed frame ends the stream.
- **Writes** (`putRecord`, `createRecord`, `deleteRecord`) call
  `com.atproto.space.*` with the writer's fetch handler, then wait for the
  event they produced. A put is matched on its record URI **and** CID (a CID
  only hashes content, so the URI is what tells equal records apart); a
  delete on its URI. The runner remembers the latest indexed CID per URI, so
  an event that arrives before the write's own response still counts.
  `WriteError` means nothing was written; `WriteNotObserved` (after the
  timeout, default 15 s) means the write succeeded but isn't indexed yet:
  the stream is behind, or a later write to the same record replaced it in
  the compacted log first.
- **Deletes** look the record up first: deleting a record that doesn't exist
  produces no event, so it returns at once.
- **Transport**: by default the WebSocket is opened as an HTTP upgrade
  through `fetch`, the way Cloudflare Workers make outbound WebSockets, so
  OAuth/DPoP headers ride on it like any other call. Elsewhere, pass
  `webSocketTransport(url => new WebSocket(url, { headers }))`.

## API reference

Everything is exported from the package root (`src/index.ts`). Streams and
writes are [Effect](https://effect.website) values; `replay()`/`stop()` are the
promise-based entry points for apps that don't run their own Effect runtime.

### `new Jetstream(options)`

The client of `network.habitat.space.subscribe`.

| Option      | Meaning                                                                                                              |
| ----------- | -------------------------------------------------------------------------------------------------------------------- |
| `service`   | The space host to follow: the org's Habitat (pear) URL.                                                              |
| `mode`      | Only `"spaces"`; kept so call sites match `@bsky/jetstream`.                                                          |
| `fetch`     | The credential: typically an OAuth session's `fetchHandler`. Receives paths for XRPC calls and absolute URLs for the stream. Defaults to unauthenticated `fetch` against `service`. |
| `transport` | How to open the WebSocket. Defaults to `fetchTransport(fetch)`.                                                       |
| `spaces`, `dids` | Server-side filters, applied to every subscription.                                                              |
| `reconnect` | `{ base, cap }` backoff, default 500 ms doubling to 30 s.                                                             |

- `subscribe({ cursor?, collections? })` returns `Stream<SpaceEvent, StreamError | FrameDecodeError>`:
  events from `cursor` (inclusive; `0` replays, omitted starts live), then live,
  with transparent reconnects. Only errors a reconnect can't fix end it.
- `subscribeUrl(cursor, collections)` builds the `wss://` URL.
- `runner(target, options?)` creates a [`Runner`](#runner).

### `LexIndexer`

Routes events to per-collection handlers, like `@bsky/jetstream`'s.

- `.commit(collection, { put?, del? })`: `collection` is an NSID, a
  `"<prefix>.*"` pattern, or a generated `@atproto/lex` namespace. With a lex
  namespace, `$safeParse` validates and types records; invalid records are
  logged and skipped.
- `.spaceDeleted(handler)`: a space was deleted; drop everything held for it.
- `.collections()`: the NSIDs to request server-side (all, if no commit
  handlers are registered).
- `.sink()` / `.handle(event)`: the indexer as an Effect `Sink` / for one event.

Handlers may return an `Effect`, a `Promise` or nothing.

### `Runner`

`js.runner(indexerOrSink, { cursor, writeTimeout, recentLimit, collections, batch })`

- **Running**: `replay({ cursor? })` starts it in the background (rejects if
  already started) and returns a promise that settles when it stops; `stop()`,
  `running()`. `run(options)` is the same as an `Effect`. `apply(event)` /
  `applyAll(events)` feed events by hand (tests, custom pipelines).
- **Cursor**: `cursor()` reads it. A `CursorStore` is `{ get, set }` (Effect,
  promise or sync). Without one the cursor is kept in memory. The stored value
  is the seq to resume from (one past the last indexed event).
- **Writes** (all return `Effect<WriteResult, WriteError | WriteNotObserved>`,
  with `WriteOptions { fetch?, timeout? }`):
  `putRecord`, `createRecord` (rkey optional), `deleteRecord`.
  `awaitIndexed(uri, cid | undefined, timeout?)` is the building block: wait
  until the record is indexed at that CID (`undefined` means deleted).

### Events

`SpaceEvent = PutEvent | DelEvent | SpaceDeletedEvent`, discriminated on `kind`.
A `PutEvent` (`create`/`update`) carries `cid` and the decoded `record`; a
`DelEvent` carries only the common fields (`seq`, `space`, `did`, `uri`,
`collection`, `rkey`, `rev`, `time`). `decodeFrame(text)` parses one raw frame.

### Transports

- `fetchTransport(fetch)` (default): HTTP `Upgrade: websocket` through `fetch`,
  as Cloudflare Workers open outbound sockets.
- `webSocketTransport(open)`: for runtimes with a constructor-style WebSocket;
  credentials must be added by `open` itself.

### Errors (tagged, `Schema.TaggedError`)

`ConnectionError` (retried), `StreamError` (server error frame or non-retryable
HTTP refusal; ends the stream), `FrameDecodeError`, `HandlerError` (retried),
`CursorError`, `WriteError` (nothing written), `WriteNotObserved` (written, not
yet indexed).

## Architecture

```
 pear: network.habitat.space.subscribe          com.atproto.space.{put,create,delete}Record
        │ xrpc.v1.json frames                            ▲
        ▼                                                │ writer's fetch
  Transport ──text frames──► decodeFrame ──► Jetstream.subscribe ──► Runner ──► Sink / LexIndexer ──► your handlers
  (fetch upgrade               (typed Frame)    (cursor tracking,      │  advance cursor after
   or WebSocket)                                 reconnect/backoff)    │  handlers succeed
                                                                       └─► recent map + waiters ──► write promises resolve
```

### Layers

1. **Transport** (`transport.ts`): turns one WebSocket connection into an Effect
   `Stream<string, ConnectionError>` of text frames. A queue is filled by the
   socket's `message` events, and failed by `close`/`error`. A finalizer closes
   the socket when the stream is interrupted. `fetchTransport` sends the upgrade
   through the authenticated fetch handler, so OAuth/DPoP headers ride on it
   like any other request and a refused upgrade surfaces as a `ConnectionError`
   with its HTTP status.
2. **Frame decoding** (`events.ts`): parses `xrpc.v1.json` frames into a tagged
   `Frame` (`Event`, `Info`, `Error`, `Unknown`). Record bodies are re-parsed
   with `lexParse` so `$link`, `$bytes` and blob refs come out as typed values.
   Unknown payload kinds and operations are skipped, so the server can add
   event types without breaking old clients.
3. **Jetstream** (`Jetstream.ts`): owns the *connection-level* cursor. Each
   connection attempt starts at `next` (the seq after the last event
   *emitted*), and `next` survives reconnects, so consumers see one unbroken
   at-least-once sequence. `ExpiredToken` is converted to a retryable
   `ConnectionError` (the fetch handler refreshes the token on the next
   attempt). Refusals with 4xx other than 401/408/429 become a fatal
   `StreamError`. Reconnect delay is exponential, capped, and reset after any
   event arrives. `Info` frames are logged, not emitted.
4. **Runner** (`Runner.ts`): owns the *durable* cursor and the write path.

### Runner: indexing and the cursor

The runner subscribes from the saved cursor (else `0`), optionally groups
events (`Stream.groupedWithin(size, window)`), and for each group runs the sink
in `applyAll`:

1. Run the sink over the group; on failure, **retry the same group forever with
   exponential backoff** (200 ms, capped at 1 min) rather than skip it.
2. Only then `saveCursor(last.seq + 1)`.
3. Only then record the events as indexed and wake waiting writes.

So the persisted cursor never passes an event a handler hasn't finished, giving
at-least-once delivery to handlers. Two cursors exist deliberately: the
connection-level one in `Jetstream` (for reconnects within a process) and the
persisted one in `Runner` (for restarts).

### Runner: write-and-wait

`putRecord` and friends call the writer's XRPC endpoint with *their* fetch
handler (the member's session, resolving against their own host), then block
until the stream has delivered and the sinks have indexed the resulting event:

- Events are matched on **URI and CID** (a CID only hashes content, so the URI
  distinguishes equal records in different places); a delete matches on URI
  alone.
- `indexed(event)` runs synchronously after the cursor is saved. It updates a
  bounded, insertion-ordered **recent map** (`uri → latest cid`, default 10,000
  entries, oldest evicted) and resolves matching `waiters`.
- `awaitIndexed` first checks the recent map, which handles the race where the
  event arrives *before* the write's HTTP response. Otherwise it registers a
  `Deferred` waiter and times out (15 s default) with `WriteNotObserved`.
- Because the server log is compacted, a later write to the same record can
  supersede yours before you observe it. That surfaces as `WriteNotObserved`
  too, though the record is written.
- `deleteRecord` first `getRecord`s the target: deleting something absent
  produces no event, so it returns immediately in that case.

### Key design decisions

- **Effect-native, promise-friendly.** Handlers and cursor stores accept
  Effects, promises or plain values (`toEffect`), so apps pick their style.
- **Mirror `@bsky/jetstream`'s spaces API** (`new Jetstream({ service, mode })`,
  `LexIndexer.commit`, `runner(...).replay`) so either implementation can be
  swapped in.
- **One cursor for the whole org**, however many spaces and repos the credential
  covers; the server does the authorization and filtering.
- **Never skip a failing event**: correctness of the index over liveness. Make
  sinks idempotent (key on `uri`).
- **Credential as a fetch function**, so token refresh and DPoP stay in the
  OAuth layer and the SDK never handles tokens.
- **No hibernation.** The SDK holds an outbound client socket, which platforms
  such as Cloudflare Durable Objects can't hibernate; hosts that care about
  duration billing should connect on demand and resume from the cursor.

Tests: `src/Runner.test.ts` (`vitest run`).

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

## Catching up over HTTP (replay archive)

A fresh runner, or one that was off for long, can catch up through
`network.habitat.space.getSnapshot` instead of one long WebSocket replay. It
mirrors Bluesky Jetstream's replay archive (`planSnapshot`): `afterSeq` /
`beforeSeq` bound the range, the first page pins `tipSeq`, and
`plannedThroughSeq` is the next page's `afterSeq`. Our log is one compacted
table rather than sealed segment files, so pages carry the events inline.

```ts
void runner.replay({ snapshot: true }); // HTTP pages to the tip, then the socket from tip + 1
// or by hand:
js.snapshot({ after: 0 }).pipe(Stream.runForEach((page) => ...));
```

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

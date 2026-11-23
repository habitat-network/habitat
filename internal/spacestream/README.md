# spacestream

Serves **`network.habitat.space.subscribe`**: one ordered sequence of every
record write and space deletion this host holds, across every space a
subscriber may read. Apps follow permissioned data from a single WebSocket with
a single cursor, instead of each syncing every space and repo themselves.

It is Habitat's counterpart to [Jetstream](../../external/jetstream) and is
modeled on it (pull-based streams, an inclusive cursor, `xrpc.v1.json`
framing), adapted for permissioned data: every event is **authorized for its
subscriber as it is delivered**.

The client side is [`@habitat-network/jetstream`](../../typescript/jetstream).
The wire contract is the lexicon
[`network.habitat.space.subscribe`](../../lexicons/network/habitat/space/subscribe.json).

## Public API

### Wire protocol

`GET /xrpc/network.habitat.space.subscribe` upgrades to a WebSocket. The stream
is server-push only: a data frame from the client is a protocol violation and
closes the connection with a policy-violation code.

Query parameters (all optional; filters are ANDed and each matches everything
when omitted):

| Parameter     | Meaning                                                                                                    | Cap     |
| ------------- | ---------------------------------------------------------------------------------------------------------- | ------- |
| `cursor`      | Resume position, **inclusive** (`seq >= cursor`). `0` replays from the start. Omitted starts at the live tip. |         |
| `spaces`      | Space URIs to receive events for. Spaces the caller can't read are ignored, not rejected.                   | 100     |
| `dids`        | Record authors to receive `#commit` events for.                                                             | 10,000  |
| `collections` | Collection NSIDs, or `<prefix>.*` patterns (e.g. `app.bsky.*`), for `#commit` events.                      | 100     |

A malformed or over-cap parameter is rejected **before** the upgrade with
HTTP 400.

Authentication is an `Authorization` header (never a cookie, which is why any
`Origin` is allowed). Three credentials are accepted:

| Credential       | Receives                                                                                               |
| ---------------- | ------------------------------------------------------------------------------------------------------ |
| OAuth token      | Spaces the user holds the **reader role** on _and_ the token's `space:` scopes cover. `read_self` limits a space to the caller's own records. |
| Service auth     | Every space the user holds the reader role on (no scope limit).                                         |
| Space credential | That one space only. A `spaces` filter that excludes it is a 400.                                       |

Each text frame is `xrpc.v1.json`:

```jsonc
// a message frame; payload.$type is network.habitat.space.subscribe#<kind>
{ "$type": "message", "payload": { "$type": "network.habitat.space.subscribe#commit", /* ... */ } }
// terminal; the connection closes after it
{ "$type": "error", "error": "ExpiredToken", "message": "..." }
```

Payload kinds:

- **`#commit`**: a record's latest write. Fields: `seq`, `space`, `did`
  (author), `uri`, `time`, `rev`, `operation` (`create` | `update` |
  `delete`), `collection`, `rkey`, and, except for deletes, `record` and
  `cid`.
- **`#spaceDeleted`**: `seq`, `space`, `time`. Consumers drop everything they
  hold for the space. Sent only for spaces the subscriber names in `spaces`,
  the space a space credential is for, or spaces it owns, because a deleted
  space has no roles left to check.
- **`#info`**: advisory, carries no `seq` and doesn't advance the cursor.
  Currently only `FutureCursor`: the requested cursor was past the next seq, so
  the stream started at the live tip.

Event times are RFC 3339 UTC with exactly six fractional digits, like
Jetstream.

### Delivery semantics

- **At-least-once, inclusive cursor.** Resume by passing the seq _after_ the
  last event you processed. Consumers must be idempotent, keyed on `uri`.
- **Compacted log.** Each record appears once, at the seq of its latest write,
  so replaying from `0` converges on current state but skips intermediate
  versions. Deleted records appear as `delete` commits.
- **Seqs increase but are not contiguous.**
- **Expiry.** When the credential the stream was opened with expires, the
  server sends an `ExpiredToken` error frame and closes. Reconnect with a fresh
  credential and the last seq.
- **Revocation.** Access is rechecked page by page, so a user who loses the
  reader role stops receiving events from that space within a page.

### Go package API

| Symbol | Purpose |
| ------ | ------- |
| `ParseQuery(url.Values) (Query, error)` | Parses and validates query parameters (dedupes lists, then applies the caps; `ErrInvalidQuery`). |
| `Subscription{Query, Reader, ExpiresAt}` | An authenticated request. `Reader` is a `UserReader{DID, Scope}` or a `SpaceCredentialReader{Space}`. |
| `Prepare(Subscription)` | Pre-upgrade check. Narrows a space credential's filter to its space; returns `ErrCredentialMismatch` if the filter excludes it. |
| `NewServer(source, roles, broadcaster, opts...)` | Builds a `Server`. Options: `WithPageSize` (default 100), `WithPollInterval` (default 2s). |
| `(*Server).ServeWebSocket(w, r, sub)` | Upgrades and runs the stream until either end closes. |
| `(*Server).Run(ctx, conn, sub)` | Transport-independent stream loop over a `Conn` (one method: `WriteFrame`). Used directly in tests. |
| `NewBroadcaster()` | Wake-up channel; implements `spaces.Notifier`. |

Dependencies are small interfaces: `Source` (`SequenceEvents`, `ListEvents`;
satisfied by `spaces.Store`) and `RoleChecker` (`CheckUserHasSpaceRole`;
satisfied by the permissions store).

The HTTP handler lives in
[`internal/pearserver/space_subscribe.go`](../pearserver/space_subscribe.go):
it parses the query, validates the credential, maps it to a `Reader` (turning
an OAuth token's `scp` claim into a `spacescope.Limit`), calls `Prepare`, and
hands off to `ServeWebSocket`.

## Architecture

```
 writers ──► spaces.Store ──(Notifier)──► Broadcaster ─┐ wake
 (putRecord,   │  rows get seq = 0                      │
  deleteSpace, │                                        ▼
  remotesync)  │                          per-subscriber stream (Server.Run)
               │   SequenceEvents()  ◄──── 1. number pending rows, get head
               │   ListEvents(cursor) ◄─── 2. read a page from own cursor
               │                           3. authorize each event (accessCache)
               └───────────────────────►   4. write frames to the WebSocket
```

### The event log lives in the spaces store

There is no separate log or queue. `internal/spaces` keeps a `seq` column on
each record row (`space_records`, `space_remote_records`) and on deleted
spaces. A write sets `seq = 0` ("pending") and **never touches the counter**, so
writers don't contend on the log.

`Store.SequenceEvents` numbers pending rows in bounded batches of 500, inside a
transaction that locks the single counter row. Because the lock is held to
commit, a seq only becomes visible once every lower one has committed, which is
what makes seqs appear in order. Pending rows are taken `FOR UPDATE SKIP
LOCKED`, so the sequencer never waits on an in-flight write: a locked row is
about to be rewritten and left pending, so a later batch numbers it. Order
within a batch: local records by `rev`, then records fetched from external
writers by `rev`, then space deletions.

Since each row carries only the seq of its latest write, the log is
**compacted by construction**: rewriting a record gives it a new seq and drops
the old one.

`Store.ListEvents(filter, cursor, limit)` reads local records, remote records
and deletions each in `seq` order with the filter pushed into SQL, then merges
them and keeps the first `limit`. Fewer than `limit` results means the head
was reached.

### Pull-based streams, no per-stream buffer

Like Jetstream, each stream reads the log from its **own cursor**. A slow
reader just falls behind; nothing queues in memory. `Server.run` loops:

1. Take `broadcaster.Wait()` **before** reading, so a write landing during the
   read still wakes the next wait (no lost wake-up).
2. `SequenceEvents` to number anything pending (every stream does this, which
   is cheap when nothing is pending: it takes no locks).
3. `ListEvents` a page (default 100) from the cursor.
4. Authorize and write each event; advance the cursor past every event in the
   page, delivered or not.
5. A full page means more to read: loop immediately. Otherwise wait on the wake
   channel, the 2s poll ticker, or context end.

The **Broadcaster** is a "close and replace a channel" wake-up: `wake()` closes
the current channel and installs a fresh one, so every waiter is released with
no per-stream state. It carries no payload. It is a `spaces.Notifier`, so pear
registers it alongside the webhook notifier, and the store calls it on
`NotifyWrite`, `NotifySpaceDeleted` and `NotifyWriterRemoved`. The **poll
ticker** covers writes made by _other pear instances_, whose broadcasts this
process never hears.

### Authorization at delivery

Permissioned data means the set of readable spaces can change while a stream is
open, so access is evaluated per event, not once at connect. `accessCache`
(one per page, so revocation takes effect within a page) decides:

- **Space credential**: `event.space == credential.space`.
- **User**: first the token scope (`ReadNone` drops the event). A
  `#spaceDeleted` is allowed to the space's owner or when the space is named in
  the `spaces` filter. A `#commit` under `read_self` must be authored by the
  caller. Otherwise the reader role is checked with `CheckUserHasSpaceRole`,
  memoized per space for the page.

Events the reader may not see are skipped silently, and the cursor still moves
past them.

### External writers

Members whose repos live on their own PDS only report writes through
`notifyWrite`. That handler queues the writer on `internal/remotesync`, which
(as the space authority, with a self-minted space credential) pulls the ops
with `listRepoOps`, verifies them against the writer's MAC-authenticated commit
(LtHash), stores them in `space_remote_records`, and notifies the broadcaster.
They are then sequenced and streamed as ordinary `#commit` events.

### WebSocket transport (`websocket.go`)

`ServeWebSocket` runs `Server.Run` over a gorilla WebSocket with:

- a **reader goroutine** that only services pongs and the close handshake (a
  data frame closes with a policy violation);
- a **pinger goroutine**: ping every 30s, drop the subscriber if no pong within
  60s;
- a **10s write deadline** per frame, so a subscriber that stops reading is
  dropped rather than pinning the stream;
- context cancellation _causes_ (`errClientClosed`, `errExpired`) to tell a
  hang-up from a server shutdown from credential expiry. On expiry `Run`
  writes the `ExpiredToken` frame on a fresh short context, since the stream's
  own context has already ended.

### Key design decisions

- **The store is the log.** No second source of truth, so streams survive
  restarts and work across multiple pear instances (via the poll tick).
- **Sequencing is separate from writing**, so writes never serialize on a global
  counter, while consumers still see a gap-free-in-visibility order.
- **Pull, not push**: no buffers, no backpressure policy; a slow consumer only
  affects itself.
- **Compaction over history**: a replay is cheap and bounded by current state,
  at the cost of not seeing intermediate versions.
- **Authorize on delivery**, with scope limits from the OAuth token layered on
  top of the role check, so an app only sees what both the user _and_ its
  grants allow.
- **Future cursors don't fail**: they start at the live tip with a
  `FutureCursor` info frame, matching Jetstream.

## Testing

`stream_test.go`, `query_test.go` and `websocket_test.go` cover replay,
filtering, authorization changes mid-stream, expiry and the transport. Run with
`go test ./internal/spacestream/...`.

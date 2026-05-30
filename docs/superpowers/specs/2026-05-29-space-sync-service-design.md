# Org Space Sync Service

## Overview

Habitat needs a sync layer for organization-approved apps to consume all
authorized space data without asking each user to authorize the app
individually. The sync layer follows atproto's split between a canonical
server sync API and client-side indexing services, but adapts the
authorization model for Habitat organizations.

Pear is the authoritative sync source for an org. It exposes a small sync
API for live updates and backfill. `cmd/spacetap` is the canonical syncing
client implementation: it consumes Pear's sync API, maintains durable
cursors and local state, and offers Tap-like delivery modes for apps that do
not want to implement sync themselves.

This spec intentionally uses the existing OAuth token model. An admin can
grant an app org-level scopes; those OAuth access tokens are the app's sync
credential. There is no separate org credential format for v1.

## Goals

- Let admin-approved apps sync all spaces of the granted space types in an
  org.
- Provide a canonical Pear API for real-time updates and backfill.
- Provide `cmd/spacetap` as both reference implementation and off-the-shelf
  service for apps.
- Keep authorization simple: OAuth org scopes gate space types; runtime
  filters only narrow the data delivered.
- Make stream consumption recoverable with durable cursors, oplog replay,
  and full resync fallback.

## Non-goals

- Per-user OAuth authorization for org-approved apps.
- Cross-PDS space fan-out. Current Habitat spaces are served from Pear's org
  database.
- Cryptographic CAR/MST verification for space records in v1. The stream is
  authoritative from Pear, not self-certifying like public atproto repo
  sync.
- A full Relay equivalent. Pear streams a single organization's authorized
  space data.

## Authorization

### OAuth scope shape

Sync uses normal Habitat OAuth access tokens. PR 364's org-scope behavior is
the baseline, with one semantic adjustment: the value after `org:` names a
space type, not a record collection.

Examples:

```text
org:network.habitat.calendar?action=sync
org:network.habitat.docs?action=sync
org:*
```

Only org admins can grant `org:*` or `org:<space-type>` scopes. Pear
enforces that at OAuth authorization time. Sync endpoints enforce granted
scopes at request time.

Scope matching:

- `org:*` permits all space types in the org.
- `org:<space-type>` permits only spaces whose `space.Type` equals that
  NSID.
- If action parameters are present, `action=sync` permits sync. A scope with
  no action parameter is treated as allowing all actions for that space type,
  matching the current scope matching behavior.

### Collection filters are not authorization

Collection filters are runtime filters on stream/backfill requests. They
reduce bandwidth and downstream storage, but do not grant access. A token
with `org:network.habitat.calendar?action=sync` may request only
`network.habitat.calendar.event` records, but the authorization decision is
still based on the calendar space type.

Pear must never emit data for a space type not covered by the token, even if
the requested collection filter matches records in that space.

### Caller identity

The OAuth token subject is the admin DID that granted the scope. Pear should
also retain the OAuth client ID from the token/session metadata for logging,
rate limiting, connected-app display, and future revocation UX.

`cmd/spacetap` can run with either:

- an OAuth refresh token flow for long-running services, or
- a static access token for local development and short-lived jobs.

## Pear Sync API

Pear exposes a canonical sync surface under `network.habitat.sync.*`.
Endpoint names can change during lexicon implementation, but the protocol
shape should remain stable.

### `network.habitat.sync.subscribeSpaces`

WebSocket stream for live org space events.

Parameters:

| Field | Type | Description |
| --- | --- | --- |
| `cursor` | integer, optional | Last processed org stream sequence. Pear resumes after this sequence when possible. |
| `spaceTypes` | string array, optional | Requested space-type filter. Every value must be covered by the OAuth token. Omitted means all granted space types. |
| `collections` | string array, optional | Runtime record collection filter. Supports exact NSIDs in v1. |

Authentication:

- `Authorization: Bearer <oauth-access-token>`
- Required scope: `org:<space-type>` or `org:*` for every requested space
  type. If no `spaceTypes` filter is supplied, Pear derives the allowed set
  from the granted scopes.

Encoding:

- JSON WebSocket frames for v1.
- CBOR can be added later if event volume requires it.

Cursor behavior:

- Every live event has a monotonically increasing org-local `seq`.
- `cursor` resumes by `seq`, not by per-space or per-repo `rev`.
- If Pear no longer has stream history for the requested `cursor`, it closes
  with a typed error or emits an error frame requiring HTTP backfill.

### `network.habitat.sync.listSpaces`

Lists spaces visible to the token, optionally filtered by space type.

Output includes:

- `space`
- `type`
- `skey`
- `createdAt`
- `deleted`
- current `spaceRev`
- current `memberRev`
- current config or permission revs if those are tracked separately

This is the entry point for initial discovery by `cmd/spacetap`.

### `network.habitat.sync.getSpaceState`

Returns current synchronization state for one space.

Output:

```json
{
  "space": "ats://did:plc:org/network.habitat.calendar/default",
  "type": "network.habitat.calendar",
  "spaceRev": "3l...",
  "memberRev": "3l...",
  "repos": [
    { "did": "did:plc:alice", "rev": "3l..." }
  ]
}
```

`repos` contains one row per member DID with records in that space, plus any
known empty repo state if Pear tracks it.

### `network.habitat.sync.listRecords`

Backfill endpoint for live records in one space.

Parameters:

- `space` required
- `repo` optional DID filter
- `collection` optional collection filter
- `cursor` optional pagination cursor
- `limit` optional, capped by Pear

Output records include:

- `space`
- `repo`
- `collection`
- `rkey`
- `rev`
- `value`
- `updatedAt`

This endpoint is used for initial sync and full resync after oplog retention
misses.

### `network.habitat.sync.getRecordOplog`

Incremental record replay for one space and optionally one repo.

Parameters:

- `space` required
- `repo` optional
- `since` optional TID/rev, exclusive
- `limit` optional, capped by Pear

Each op includes:

- `space`
- `repo`
- `rev`
- `idx`
- `action`: `create`, `update`, or `delete`
- `collection`
- `rkey`
- `value` for create/update
- `prev` optional previous content identifier if available

If `since` is older than the retained oplog window, Pear returns a typed
`CursorTooOld` error. The client must resync the affected space or repo with
`listRecords`.

### `network.habitat.sync.getMemberOplog`

Incremental member and access replay for one space.

Parameters:

- `space` required
- `since` optional TID/rev, exclusive
- `limit` optional, capped by Pear

Each op includes:

- `space`
- `rev`
- `idx`
- `action`: `add`, `remove`, or `update`
- `did`
- `access` for add/update

This mirrors `internal/spaces` membership semantics where read/write access
is represented in FGA but must be visible to sync consumers.

### Optional state endpoints

Pear may add narrower endpoints as implementation requires:

- `getSpaceConfigOplog` for app config and managing-app changes.
- `getPermissionOplog` for org-level permission changes.
- `getIdentityState` for current member identity metadata.

For v1, these can also be represented as event types on the live stream and
folded into `getSpaceState` for backfill.

## Event Model

Live events are small JSON objects. The stream is notification plus payload:
record events include the record body for normal operation, but clients must
still be able to repair from HTTP backfill.

Common fields:

| Field | Type | Description |
| --- | --- | --- |
| `seq` | integer | Org-local stream sequence, monotonic. |
| `time` | datetime | Pear emission time. |
| `type` | string | Event discriminator. |
| `space` | string, optional | Space URI when event is space-scoped. |
| `spaceType` | string, optional | Space type for authorization/debugging. |

### `space_record`

```json
{
  "seq": 123,
  "time": "2026-05-30T12:00:00.000Z",
  "type": "space_record",
  "space": "ats://did:plc:org/network.habitat.calendar/default",
  "spaceType": "network.habitat.calendar",
  "repo": "did:plc:alice",
  "rev": "3l...",
  "idx": 0,
  "action": "create",
  "collection": "network.habitat.calendar.event",
  "rkey": "3l...",
  "record": {}
}
```

Identity for dedupe: `(space, repo, rev, idx)`.

### `space_member`

```json
{
  "seq": 124,
  "time": "2026-05-30T12:00:01.000Z",
  "type": "space_member",
  "space": "ats://did:plc:org/network.habitat.calendar/default",
  "spaceType": "network.habitat.calendar",
  "rev": "3l...",
  "idx": 0,
  "action": "add",
  "did": "did:plc:bob",
  "access": "read"
}
```

Identity for dedupe: `(space, rev, idx)`.

### `space`

Emitted when a space is created, deleted, or its sync-visible config changes.

Fields:

- `action`: `create`, `delete`, or `update`
- `rev`
- `space`
- `spaceType`
- optional `config`

### `identity`

Emitted when Pear knows an org member's identity metadata may have changed.
This is best effort, like atproto identity events. Consumers should resolve or
refresh identity state independently when they need current metadata.

Fields:

- `did`
- optional `handle`
- optional `active`
- optional `status`

### `permission`

Emitted when org-level permissions or app authorization changes may affect
what a syncing app can observe.

For v1, permission events are advisory. If a token loses authority, the next
API call or stream reconnect will fail authorization. `cmd/spacetap` should
react by refreshing tokens and revalidating its configured space types.

## Server-side Storage

Pear needs append-only logs in addition to live state. The existing
`internal/spaces` implementation already has `space` and `spaceRecord`; this
spec adds durable oplogs and stream sequence state.

### `spaceRecordOp`

```text
spaceRecordOp(
  space      SpaceURI      PK
  rev        TID           PK
  idx        int           PK
  action     string
  owner      DID
  collection NSID
  rkey       RecordKey
  value      jsonb?
  createdAt  time
)
```

Each write to `spaceRecord` also appends one or more `spaceRecordOp` rows in
the same transaction. Batch writes share a `rev` and use `idx` for ordering.

### `spaceMemberOp`

```text
spaceMemberOp(
  space      SpaceURI      PK
  rev        TID           PK
  idx        int           PK
  action     string
  did        DID
  access     string?
  createdAt  time
)
```

Each successful FGA membership/access mutation appends a row.

### `spaceStreamEvent`

```text
spaceStreamEvent(
  seq        integer       PK
  space      SpaceURI?
  spaceType  NSID?
  type       string
  payload    jsonb
  createdAt  time
)
```

This table powers WebSocket resumption. It can be compacted independently of
the per-space oplogs. If stream history is compacted, clients use oplog
backfill from their per-resource cursors.

### Retention

V1 can keep oplogs unbounded while the feature is young. The API still needs
`CursorTooOld` semantics from the start so clients implement resync correctly
before retention policies are introduced.

## `cmd/spacetap`

`cmd/spacetap` is the canonical syncing client and an off-the-shelf service
for apps. It should feel like Indigo Tap, adapted to org spaces instead of
public repo firehose consumption.

### Responsibilities

1. Authenticate to Pear with an OAuth token that has org sync scopes.
2. Discover in-scope spaces with `listSpaces`.
3. Backfill each space with `getSpaceState`, `listRecords`, and member state.
4. Subscribe to `subscribeSpaces` for live events.
5. Persist stream cursor and per-space/per-repo cursors.
6. Detect gaps or stale cursors and run resync workers.
7. Deliver normalized events to downstream app consumers.

### Local storage

Support SQLite by default and Postgres for production.

Core tables:

- `space_state`: `org`, `space`, `spaceType`, `state`, `spaceRev`,
  `memberRev`, `lastSeq`, timestamps.
- `repo_state`: `space`, `repo`, `rev`, `state`, timestamps.
- `record_state`: `space`, `repo`, `collection`, `rkey`, `rev`, `record`.
- `member_state`: `space`, `did`, `access`, `rev`.
- `outbox`: durable delivery queue with event JSON, ack state, attempts.

State values should mirror Tap where possible:

- `active`
- `backfilling`
- `resyncing`
- `desynchronized`
- `error`

### Delivery modes

`spacetap` supports three app-facing delivery modes:

- WebSocket with acks: default, at-least-once delivery. Consumers ack event
  IDs after durable processing.
- Webhook: POST events to a configured URL; 2xx response acks the event.
- Fire-and-forget: send events without ack tracking, useful for local
  development.

Event format should stay close to Tap but include space context:

```json
{
  "id": 12345,
  "type": "record",
  "record": {
    "live": true,
    "space": "ats://did:plc:org/network.habitat.calendar/default",
    "spaceType": "network.habitat.calendar",
    "did": "did:plc:alice",
    "rev": "3l...",
    "collection": "network.habitat.calendar.event",
    "rkey": "3l...",
    "action": "create",
    "record": {}
  }
}
```

Additional event payloads:

- `member`
- `space`
- `identity`
- `permission`

### Configuration

Environment variables or CLI flags:

| Name | Description |
| --- | --- |
| `SPACETAP_PEAR_URL` | Pear base URL. |
| `SPACETAP_DATABASE_URL` | SQLite/Postgres database URL. |
| `SPACETAP_BIND` | HTTP bind address for app-facing API. |
| `SPACETAP_SPACE_TYPES` | Comma-separated requested space types. Empty means all token-granted types. |
| `SPACETAP_COLLECTION_FILTERS` | Comma-separated record collections to deliver. |
| `SPACETAP_ACCESS_TOKEN` | Static OAuth access token for development. |
| `SPACETAP_REFRESH_TOKEN` | OAuth refresh token for long-running sync. |
| `SPACETAP_CLIENT_ID` | OAuth client ID for refresh flow. |
| `SPACETAP_WEBHOOK_URL` | Enables webhook delivery. |
| `SPACETAP_DISABLE_ACKS` | Enables fire-and-forget delivery. |
| `SPACETAP_RESYNC_PARALLELISM` | Number of concurrent resync workers. |
| `SPACETAP_OUTBOX_PARALLELISM` | Number of delivery workers. |
| `SPACETAP_LOG_LEVEL` | Log verbosity. |

### App-facing HTTP API

Minimum API:

- `GET /health`
- `WS /channel`
- `GET /stats`
- `GET /info/space/:space`
- `POST /spaces/resync`

Optional admin APIs can mirror Tap's repo add/remove controls later. V1 does
not need dynamic space addition because Pear is the source of truth and
`spacetap` discovers spaces from the granted org scopes.

## Sync Algorithms

### Initial sync

1. Validate configured `spaceTypes` against token scopes by calling Pear.
2. Call `listSpaces`.
3. For each space, call `getSpaceState`.
4. Backfill members and records.
5. Mark each repo and space `active`.
6. Start `subscribeSpaces` from the newest available stream cursor.

If live events arrive while a space is still backfilling, `spacetap` queues
them by space and applies them after backfill reaches the relevant rev.

### Live event handling

For each stream event:

1. Persist raw event and stream `seq`.
2. Check the event is still in configured `spaceTypes` and collection filters.
3. Apply the event idempotently to local state.
4. Enqueue the app-facing event in the outbox.
5. Advance the per-resource cursor after the local transaction commits.

### Stream reconnect

On reconnect, `spacetap` passes the last processed `seq`.

- If Pear resumes successfully, processing continues.
- If Pear reports the stream cursor is too old, `spacetap` calls
  `listSpaces` and `getSpaceState`, then replays per-space oplogs from its
  stored revs.
- If any per-space oplog returns `CursorTooOld`, `spacetap` full-resyncs that
  space.

### Periodic reconciliation

`spacetap` periodically calls `listSpaces` and `getSpaceState` for tracked
spaces. This catches missed stream events, token-scope changes, and stream
implementation bugs. A five-minute default interval is reasonable for v1.

## Ordering and Delivery Guarantees

Pear guarantees:

- `seq` increases for every stream event in one org.
- Per-resource `rev` increases for writes to the same record repo or member
  list.
- Stream delivery is at least once across reconnects when the cursor remains
  within retention.

Pear does not guarantee:

- Global causal ordering across unrelated spaces.
- Infinite stream replay.
- Self-certifying event payloads.

`spacetap` guarantees to app consumers:

- At-least-once delivery in WebSocket-with-acks and webhook modes.
- Best-effort delivery in fire-and-forget mode.
- Idempotent local application of duplicate Pear events.
- Per-space/repo record ordering in delivered live events.

Consumers should dedupe by app-facing event `id` when using acks, or by the
resource identifiers included in each payload.

## Error Handling

Pear errors:

- `Unauthorized`: missing or invalid OAuth token.
- `Forbidden`: token lacks required `org:<space-type>` scope.
- `CursorTooOld`: requested stream/oplog cursor is older than retention.
- `SpaceNotFound`: requested space does not exist or is not visible.
- `RateLimited`: caller exceeded server policy.

`spacetap` behavior:

- `Unauthorized`: refresh token if possible; otherwise enter `error`.
- `Forbidden`: stop syncing affected space types and surface config/token
  error.
- `CursorTooOld`: resync affected scope.
- transient network errors: reconnect with exponential backoff.
- repeated resync failure: mark affected space/repo `error` and keep retrying
  with backoff.

## Implementation Notes

- Existing `internal/spaces.GetRepoOplog` currently reads from live records,
  which cannot represent deletes. It must be retargeted to an append-only
  oplog before it is suitable for sync.
- Existing member state lives in FGA. Sync needs an append-only member oplog
  because FGA alone cannot answer "what changed since rev X?"
- `PutRecord`, `DeleteRecord`, `AddMember`, `RemoveMember`, `CreateSpace`,
  and `DeleteSpace` should write live state, oplog rows, and stream events in
  one database transaction where possible.
- If FGA writes cannot share the SQL transaction, append the member oplog only
  after the FGA mutation succeeds. The implementation should make repeated
  operations idempotent.
- The stream event table is an implementation detail; Pear may also publish
  directly from an in-process event manager, but durable sequence replay
  requires persisted events or equivalent state.

## Testing

Pear tests:

- OAuth tokens with `org:<space-type>` can subscribe only to that space type.
- `org:*` can subscribe to multiple space types.
- collection filters narrow records but cannot bypass space-type scope checks.
- record create/update/delete all produce oplog rows and stream events.
- member add/remove/access update all produce oplog rows and stream events.
- stream resume from `cursor` returns only later events.
- old stream/oplog cursor returns `CursorTooOld`.
- `listRecords` full resync reconstructs current state after deletes.

`spacetap` tests:

- initial backfill stores spaces, members, records, and cursors.
- live events update local state and outbox.
- duplicate events are idempotent.
- stream cursor loss triggers oplog replay.
- oplog `CursorTooOld` triggers full space resync.
- WebSocket ack mode retries unacked events.
- webhook mode retries non-2xx responses.
- fire-and-forget mode does not block on acks.

## Deferred Scope

- Lexicon naming can change during implementation, but the v1 API must keep
  the conceptual split from this spec: live stream, space discovery, state,
  record backfill, record oplog, and member oplog.
- `permission` events are live-stream only in v1. Pear authorization remains
  authoritative on every request and reconnect, so a separate permission
  backfill endpoint is not required for initial implementation.
- Stream encoding is JSON in v1. CBOR is explicitly deferred until measured
  event volume or frame size makes JSON a problem.

## References

- AT Protocol sync specification: https://atproto.com/specs/sync
- Indigo Tap reference implementation: `../indigo/cmd/tap`
- Existing Habitat spaces implementation: `internal/spaces`

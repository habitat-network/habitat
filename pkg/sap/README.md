# sap — Sync Agent Process

`sap` is a library (and thin binary wrapper) that keeps a local copy of AT
Protocol repos in sync with their habitat hosts, for every `network.habitat.space`
a set of OAuth/JWT-bearer sessions can see. Synced records are delivered
through a durable, acknowledged outbox.

## How the parts fit together

```
AddSession(did) ─────▶ session.Store ─────▶ crawl.Crawler
                        (auth clients,        (listSpaces/listRepos,
                         space access)         resumable via cursor)
                                                     │
                                    Track/Check      │
                                                     ▼
                        register.Registrar ◀── syncer.Engine ──▶ outbox.Store
                        (registerNotify,        (state machine:         │
                         renews before            pending → syncing →   │
                         expiry)                   active/desynced,     ▼
                                                    listRepoOps +   consumer
                                                    LtHash verify,  (Poll/Ack/Watch)
                                                    getRepo recover
                                                    on verify failure)
```

- **`session`** owns every tracked session's auth and issues HTTP clients:
  `ClientForSession` (as the session itself, for crawling) and
  `ClientForSpace` (a space-credential client, for reading a space any tracked
  session can access). Every other package gets an authenticated client
  through it rather than touching OAuth/JWT-bearer state directly.
- **`crawl`** backfills: for each session it pages `listSpaces`, records space
  access, and for each space's `listRepos` calls `Tracker.Check` (start
  tracking, or compare the listed rev/hash against ours). Crawl progress is a
  cursor persisted per session, so a restart resumes instead of re-scanning.
- **`syncer`** is the sync engine and state machine, one row per `(space,
  repo)`. `pending`/`error` repos are synced incrementally via
  `listRepoOps` and verified against the host's signed commit hash (LtHash);
  a repo that fails verification is marked `desynced` and rebuilt from a full
  `getRepo` CAR snapshot. `Track` (new repo from crawl) and `NotifyWrite`
  (pushed notification) both funnel into the same staleness check, so a
  repo mid-sync when a new write lands is marked dirty and requeued instead
  of settling on stale data.
- **`register`** keeps `registerNotify` subscriptions alive so hosts push
  `notifyWrite`/`notifySpaceDeleted` to sap instead of relying on polling: it
  registers a space inline as crawl discovers it, and a background sweep
  renews registrations before they expire.
- **`outbox`** is the durable handoff to sap's consumer: the syncer emits
  synced records here (in the same transaction as its state advance), and the
  consumer polls, processes, and acks them. Unacked messages redeliver.
- **`credential`** and **`jwtbearer`** back `session`: `credential` mints and
  caches per-space host credentials so reads are authorized as the space
  rather than an individual member; `jwtbearer` lets sap authenticate to a
  host directly (RFC 7523 JWT-bearer grant) for sessions added without an
  OAuth flow.

## Quick start

```go
import (
    "github.com/habitat-network/habitat/pkg/sap"
    "github.com/habitat-network/habitat/pkg/sap/session"
    "go.opentelemetry.io/otel"
)

s, err := sap.New(sap.Config{
    DB:          db,
    OAuthClient: oauthApp,
    Directory:   identity.DefaultDirectory(),
    Endpoint:    "https://sap.example.com", // registered with hosts for notifyWrite
    Meter:       otel.Meter("sap"),
    Tracer:      otel.Tracer("sap"),
})
if err != nil {
    log.Fatal(err)
}

// Registers the session and kicks off its backfill crawl in the background.
if err := s.AddSession(ctx, did, sessionID, session.AuthOAuth); err != nil {
    log.Fatal(err)
}

// Runs the sync engine, crawl resumption/recrawl loop, and notify-registration
// upkeep until ctx is cancelled.
if err := s.Start(ctx); err != nil {
    log.Fatal(err)
}
```

When a host calls back with a notification, relay it into sap:

```go
s.NotifyWrite(ctx, space, repo, rev, hash)   // repo advanced; sync it
s.NotifySpaceDeleted(ctx, space)             // drop all tracking state for it
```

## Consuming the outbox

```go
for {
    msgs, err := s.Outbox().Poll(ctx, 100)
    if err != nil {
        log.Fatal(err)
    }
    for _, msg := range msgs {
        // process msg.Value (json.RawMessage) keyed by msg.URI
        if err := s.Outbox().Ack(ctx, msg.ID); err != nil {
            log.Fatal(err)
        }
    }
    if len(msgs) == 0 {
        <-s.Outbox().Watch()
    }
}
```

## The `cmd/sap` binary

Wraps a `*sap.Sap` with HTTP. The OAuth-facing endpoints (`/oauth-callback`,
`/client-metadata.json`) and the host-facing notify webhooks
(`/xrpc/network.habitat.space.notifyWrite`, `notifySpaceDeleted`, validated by
service-auth) are served on the public port; session management and the
outbox are served on a separate internal port meant to be restricted to
trusted callers:

- `POST /session/add` — start an OAuth flow for a handle (`AddSession` runs on callback)
- `POST /session/jwt` — register a JWT-bearer session for a DID directly
- `GET /session/list` — DIDs of tracked sessions
- `GET /channel` — websocket streaming the outbox (see
  [`cmd/sap/websocket.go`](../../cmd/sap/websocket.go) for the wire protocol);
  a message is redelivered until the client acks its ID
- `/proxy/<nsid>` — forwards an XRPC call to pear authenticated as the session
  named by the `Habitat-Did`/`Habitat-Session-Id` headers

## Tables

Each subpackage owns and auto-migrates its own tables:
`sap_sessions`/`sap_space_access` (session), `sap_crawls` (crawl),
`sap_repos`/`sap_repo_records` (syncer), `sap_registrations` (register),
`sap_outbox` (outbox).

## Configuration

| Field | Description |
|---|---|
| `DB` | GORM database handle (schema auto-migrated on construction) |
| `OAuthClient` | OAuth client app for resuming sessions and minting space credentials |
| `Directory` | AT Protocol DID directory for commit-signature verification; nil verifies by hash only |
| `Endpoint` | sap's public base URL registered with hosts for notifications; empty disables registration |
| `Parallelism` | Sync worker pool size (default 5) |
| `CrawlInterval` | How often every session is re-crawled (default 1h) |
| `JWTBearer` | Enables JWT-bearer sessions (`session.AuthJWTBearer`) when set |
| `Meter` / `Tracer` | OpenTelemetry instrumentation (nil = no-op) |

Metrics are prefixed `sap.crawler.*` and `sap.syncer.*`; see each package's
`New`/telemetry setup for exact names.

## Running sap

```bash
moon sap:dev     # development with hot reload
moon pear:build  # build the binary
```

Flags/env vars are documented in [`cmd/sap/flags.go`](../../cmd/sap/flags.go).

# sap — Sync Agent Process

`sap` is a library (and thin binary wrapper) that keeps a local copy of AT
Protocol repos in sync with their habitat hosts, for every `network.habitat.space`
a tracked DID can see. Synced records are delivered through a durable,
acknowledged outbox.

sap itself never establishes a session: `AddSession(did, sessionID)` just
records that a session is resumable for did, and resumes it via the
`*oauth.ClientApp` (and its `Store`) the caller configures sap with. How a
session actually came to exist — a browser OAuth flow, an RFC 7523
JWT-bearer grant, anything else — is entirely the caller's concern, done out
of band of this package: `cmd/sap` has two ways of adding a session
(`/session/add`'s browser flow, `/session/jwt`'s JWT-bearer grant), and once
either gets an access-token session, it calls `AddSession` with that
session's ID.

## How the parts fit together

```
AddSession(did,           session.Store ─────▶ crawl.Crawler
  sessionID) ──────────▶  (tracked DIDs,        (listSpaces/listRepos,
                           space access,         resumable via cursor)
                           resumes via                  │
                           *oauth.ClientApp)  Track/Check│
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

- **`session`** tracks every DID sap syncs on behalf of and the session ID
  that resumes it, and issues two kinds of HTTP client: `ClientForSession`
  authenticates as the member itself against *that member's own* host, and
  backs member-scoped calls like `listSpaces` — it does this by resuming the
  tracked session ID through `Config.OAuthClient`'s `Store`, with no
  knowledge of how that session was originally established. `ClientForSpace`
  backs every call that spans a space's members —
  `listRepos`, `listRepoOps`, `getRepo`, `registerNotify` — per the
  permissioned-data proposal, which requires space-level authorization for
  those rather than a single member's access token: it hands out a
  `credential.Manager`-backed client that resolves *the space's own host*
  (its owner's habitat instance — a space's records live in its owner's
  repo) and authenticates with a space credential, minted lazily by
  exchanging a delegation token (`getDelegationToken`, fetched from some
  accessing session's own host) for a credential at that space host
  (`getSpaceCredential`). Every other package gets its clients through
  `session` rather than touching OAuth state directly.
- **`crawl`** backfills: for each session it pages `listSpaces` (member auth),
  records space access, and for each space calls `listRepos` (space-credential
  auth) into `Tracker.Check` (start tracking, or compare the listed rev/hash
  against ours). Crawl progress is a cursor persisted per session, so a
  restart resumes instead of re-scanning.
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
- **`credential`** backs `session`'s `ClientForSpace`: it mints and caches
  per-space host credentials so reads are authorized as the space rather than
  an individual member.

## Quick start

```go
import (
    "github.com/habitat-network/habitat/pkg/sap"
    "go.opentelemetry.io/otel"
)

s, err := sap.New(sap.Config{
    DB:          db,
    OAuthClient: oauthApp, // *oauth.ClientApp; AddSession resumes sessions via its Store
    Directory:   identity.DefaultDirectory(),
    Endpoint:    "https://sap.example.com", // registered with hosts for notifyWrite
    Meter:       otel.Meter("sap"),
    Tracer:      otel.Tracer("sap"),
})
if err != nil {
    log.Fatal(err)
}

// Registers did as resumable via sessionID (already saved into oauthApp's
// Store by whatever flow obtained it) and kicks off its backfill crawl.
if err := s.AddSession(ctx, did, sessionID); err != nil {
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

Wraps a `*sap.Sap` with HTTP, and owns both ways a session gets established
before handing its session ID to `AddSession`. The OAuth-facing endpoints
(`/oauth-callback`, `/client-metadata.json`) and the host-facing notify
webhooks (`/xrpc/network.habitat.space.notifyWrite`, `notifySpaceDeleted`,
validated by service-auth) are served on the public port; session management
and the outbox are served on a separate internal port meant to be restricted
to trusted callers:

- `POST /session/add` — start a browser OAuth flow for a handle; on
  callback, `ProcessCallback` saves the session into the shared
  `*oauth.ClientApp`'s `Store` and `AddSession` is called with its session ID
- `POST /session/jwt` — mint a session for a DID directly via the RFC 7523
  JWT-bearer grant (`pkg/oauthclient.Client.SendJWTTokenRequest`, which
  itself saves the session into the same `Store`), then call `AddSession`
  with the resulting session ID — sap resumes it exactly like one from the
  browser flow, since both live in the same session store
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
| `OAuthClient` | `*oauth.ClientApp` whose `Store` `AddSession` resumes sessions from |
| `Directory` | AT Protocol DID directory for commit-signature verification; nil verifies by hash only |
| `Endpoint` | sap's public base URL registered with hosts for notifications; empty disables registration |
| `Parallelism` | Sync worker pool size (default 5) |
| `CrawlInterval` | How often every session is re-crawled (default 1h) |
| `Meter` / `Tracer` | OpenTelemetry instrumentation (nil = no-op) |

Metrics are prefixed `sap.crawler.*` and `sap.syncer.*`; see each package's
`New`/telemetry setup for exact names.

## Running sap

```bash
moon sap:dev     # development with hot reload
moon pear:build  # build the binary
```

Flags/env vars are documented in [`cmd/sap/flags.go`](../../cmd/sap/flags.go).

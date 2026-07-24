# sap — Sync Agent Process

`sap` is a standalone service that crawls managed organizations' AT Protocol
repos, discovers their spaces, and keeps a local copy of each repo's records
in sync. Synced records are exposed through a durable outbox that consumers
can poll or stream over a websocket.

## Quick start

```go
import (
    "github.com/habitat-network/habitat/internal/sap"
    "go.opentelemetry.io/otel"
)

s, err := sap.NewSap(sap.SapConfig{
    DB:          db,
    OAuthClient: oauthApp,
    Meter:       otel.Meter("sap"),
    Tracer:      otel.Tracer("sap"),
})
if err != nil {
    log.Fatal(err)
}

// Register an org. This immediately starts a crawl and opens a live
// subscription for real-time events.
if err := s.AddManagedOrg(ctx, did, sessionID); err != nil {
    log.Fatal(err)
}

// Start blocks until ctx is cancelled, running all background work.
if err := s.Start(ctx); err != nil {
    log.Fatal(err)
}
```

## How it works

When an org is added via `AddManagedOrg`, three things happen in parallel:

1. **Crawl** — enumerates every space and repo in the org, inserting
   discovered repos into the `managed_repos` table in `pending` state.
2. **Subscribe** — opens an SSE connection to `network.habitat.sync.subscribeSpaces`
   to receive real-time space events.
3. **Resync** — a worker pool picks up pending/desynced repos and backfills
   them by calling `network.habitat.space.listRepoOps`.

Once a repo is fully synced it transitions to `active` state. Live events
arriving on the subscription are applied directly to the outbox. If a repo
falls behind (the subscriber sees a rev it doesn't have), the repo is marked
`desynced` and the resyncer picks it up again.

Events that arrive while a resync is in flight are buffered in a
`buffered_events` table and replayed once the resync completes, ensuring
no events are lost.

## Consuming the outbox

The `Outbox` interface (accessible at `s.Outbox`) provides at-least-once
delivery of record-level events:

```go
// Poll returns up to N unacknowledged messages.
msgs, err := s.Outbox.Poll(ctx, 100)

// Ack marks a message as processed so it won't be redelivered.
err = s.Outbox.Ack(ctx, msgs[0].ID)

// Watch returns a channel that is signalled when new messages are available.
<-s.Outbox.Watch()
```

A typical consume loop:

```go
for {
    msgs, err := s.Outbox.Poll(ctx, 100)
    if err != nil {
        log.Fatal(err)
    }
    for _, msg := range msgs {
        // process msg.Value (json.RawMessage) keyed by msg.URI
        if err := s.Outbox.Ack(ctx, msg.ID); err != nil {
            log.Fatal(err)
        }
    }
    if len(msgs) == 0 {
        <-s.Outbox.Watch()
    }
}
```

The `cmd/sap` binary exposes this over a websocket at `/channel` — see
[`cmd/sap/websocket.go`](../../cmd/sap/websocket.go) for the wire protocol.

## Proxying authenticated requests

`GetClient` returns an `*http.Client` that authenticates as a given org
using the tracked OAuth session. Requests are resolved against the org's
Habitat host:

```go
client, err := s.GetClient(ctx, did)
if err != nil {
    log.Fatal(err)
}
resp, err := client.Get("/xrpc/com.example.someMethod?key=value")
```

The `cmd/sap` binary uses this to provide a `/proxy/` endpoint — callers
set the `Habitat-Did` header and sap forwards the request with the org's
credentials. See [`cmd/sap/server.go`](../../cmd/sap/server.go).

## Org management

```go
// List all managed org DIDs.
orgs, err := s.ListManagedOrgs(ctx)
```

## Configuration

| Field | Description |
|---|---|
| `DB` | GORM database handle (schema auto-migrated on construction) |
| `ResyncParallelism` | Number of concurrent resync workers (default 5) |
| `Directory` | AT Protocol DID directory for PDS resolution |
| `OAuthClient` | OAuth client app for authenticating org PDS requests |
| `Meter` | OpenTelemetry meter (nil = no-op) |
| `Tracer` | OpenTelemetry tracer (nil = no-op) |

## Metrics

All metrics are prefixed with `sap.` and reported via OpenTelemetry:

| Metric | Type | Description |
|---|---|---|
| `sap.crawler.active` | Gauge | Active crawl goroutines |
| `sap.crawler.duration` | Histogram | Full crawl duration (seconds) |
| `sap.crawler.completed` | Counter | Crawls completed, by status |
| `sap.subscriber.active` | Gauge | Active subscription goroutines |
| `sap.subscriber.errors` | Counter | Subscription connection errors |
| `sap.subscriber.events_processed` | Counter | Events processed, by status |
| `sap.resyncer.workers_busy` | Gauge | Busy resync workers |
| `sap.resyncer.jobs_processed` | Counter | Resync jobs processed, by status |
| `sap.resyncer.job_duration` | Histogram | Single resync job duration (seconds) |
| `sap.resyncer.dispatch_duration` | Histogram | Dispatch pass duration (seconds) |

## Running sap

```bash
moon sap:dev          # development with hot reload
moon pear:build       # build the binary
```

Environment variables are documented in the project root [CLAUDE.md](../../CLAUDE.md).

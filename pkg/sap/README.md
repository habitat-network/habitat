# sap — Permissioned spaces sync utility 

Sap implements the sync protocol from the [permissioned data proposal](https://github.com/bluesky-social/proposals/blob/main/0016-permissioned-data/README.md).
It can be used by your app to crawl the permissioned spaces a user has access to, pull all the records from those spaces' repos, and receive notifications to keep the repos up-to-date.
Your app can poll Sap for new messages which are stored in a durable outbox until acknowledged.

## Quick start

```go
import (
    "github.com/habitat-network/habitat/pkg/sap"
    "go.opentelemetry.io/otel"
)

s, err := sap.New(sap.Config{
    DB:          db, // gorm.DB
    OAuthClient: oauthApp, // indigo's *oauth.ClientApp
    Directory:   identity.DefaultDirectory(),
    Endpoint:    "https://your.app.com", // registered with space hosts to receive space notification XRPC calls
    Meter:       otel.Meter("sap"),
    Tracer:      otel.Tracer("sap"),
})
if err != nil {
    log.Fatal(err)
}

// Once a user auth flow is complete (usually the OAuth callback handler), you provide the 
// ClientSession.SessionID to sap so it can crawl the user's spaces and start syncing. 
if err := s.AddSession(ctx, did, sessionID); err != nil {
    log.Fatal(err)
}

go func() {
  // Runs the sync engine, crawl resumption/recrawl loop, and notify-registration
  // upkeep until ctx is cancelled.
  if err := s.Start(ctx); err != nil {
      log.Fatal(err)
  }
}()
```

When a host calls back with `com.atproto.space.notifyWrite` or `com.atproto.space.notifySpaceDeleted`, relay it into sap:

```go
s.NotifyWrite(ctx, space, repo, rev, hash)   // repo advanced; sync it
s.NotifySpaceDeleted(ctx, space)             // drop all tracking state for it
```

## Consuming the outbox

```go
for {
    msgs, err := s.Outbox().Poll(ctx, 100 /* limit */)
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

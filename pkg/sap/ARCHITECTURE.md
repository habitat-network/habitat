# Sap Architecture

## Mostly LLM generated notes on sap's implementation. Will verify correctness later

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

- **`session`** tracks every DID sap syncs on behalf of, the session ID that
  resumes it, and which spaces each session has been seen to access. `crawl`
  resumes a tracked session directly through `Config.OAuthClient` for
  member-scoped calls like `listSpaces`, with `session` itself having no
  knowledge of how that session was originally established. `session.Store`
  also implements `credential.Delegator` (`DelegationToken`): given a space,
  it tries each recorded accessor in turn, resuming that session through
  `OAuthClient` to ask *that session's own host* for a delegation token
  (`getDelegationToken`) — never touching a space credential itself.
- **`credential`** mints and caches per-space host credentials, one per space
  regardless of which session obtained it — a space credential authorizes the
  space, not the member who fetched it. `credential.Manager.ClientForSpace`
  backs every call that spans a space's members — `listRepos`, `listRepoOps`,
  `getRepo`, `registerNotify` — per the permissioned-data proposal, which
  requires space-level authorization for those rather than a single member's
  access token: it resolves *the space's own host* (its owner's habitat
  instance — a space's records live in its owner's repo) and exchanges a
  delegation token (from its configured `Delegator`, i.e. `session.Store`)
  for a credential at that space host (`getSpaceCredential`), caching and
  renewing it just before expiry. `sap.New` wires one `credential.Manager`
  per `Sap`, built over `session.Store` as its `Delegator`, and hands it to
  `crawl`, `register`, and `syncer` wherever a space-scoped client is needed.
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

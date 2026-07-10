# docs-server

A small TypeScript (Hono) server that owns the canonical state of collaborative
documents for the orgs sap manages.

## Architecture

- Makes authenticated pear calls **through `cmd/sap`**, which holds OAuth
  sessions for the DIDs it tracks — both the orgs it manages and the individual
  users who have logged in. Every XRPC call is sent to sap's proxy
  (`POST /proxy/<nsid>`) with the DID to authenticate as in a `Habitat-Did`
  header; sap attaches that DID's access token before forwarding to pear. The
  docs server holds no credential of its own.
- Authenticates the frontend with **server sessions**. docsv2 no longer runs its
  own pear OAuth session: it talks to the docs server directly and the docs
  server owns the login. `GET /login?handle=…` starts sap's user OAuth flow;
  after the user approves, sap redirects to `GET /session/callback`, which binds
  a cookie to the authenticated DID. Requests then carry that cookie
  (credentialed CORS scoped to the docsv2 origin). `network.habitat.docs.*`
  calls from other services may still authenticate with a **service-auth JWT**
  (pear-signed, forwarded with an `Atproto-Proxy: did:web:<docs-domain>#docs`
  header, verified here with `@atproto/crypto`) — the one exception is
  `updateDoc`, which needs the user's own credential and so requires a session.
- Resolves the **caller's org from membership**: sap's `/org/list` names the
  managed orgs, and `network.habitat.relationship.listSubjects` on each org's
  self space (`ats://<org>/network.habitat.organization/self`, relation
  `reader`) yields its members. The resulting user→org map is cached briefly
  and consulted on every request.
- Writes **per-user CRDT records**. Each editor writes their own CRDT record into
  their slice of the doc space (repo = their DID) with their own credential, so
  edits are attributed to the user who made them (pear only lets a caller write
  to their own repo). The rendered **markdown record** stays a single `self`
  record written with the **org** credential. Space creation and membership
  grants also use the org credential.
- Keeps each document's merged Yjs state in a local **sqlite** table (so it
  survives restarts) and keeps merging into it: updates pushed live from the
  frontend (`updateDoc`) and the per-user CRDT records the crawler observes from
  sap both merge with `Y.applyUpdateV2`. The frontend reads that merged state
  via `getRecord` for the CRDT collection, which the server answers from its own
  store rather than pear. Other `getRecord`/relationship/space calls from the
  frontend are proxied on to pear as the logged-in user.
- Runs a **crawler** that subscribes to sap's outbox channel over its internal
  websocket port, acking every message it receives. When it sees a doc's
  markdown record it persists the doc's space URI and title to sqlite; when it
  sees a CRDT record it merges it into the doc's state. Permissions are **not**
  indexed: `listDocs` resolves them on demand via
  `network.habitat.relationship.listObjects` (the doc spaces the caller holds
  the reader role on, filtered by space type) and intersects that with the
  crawled titles. Unacked messages are redelivered by sap on the next
  connection.

## DID

The server is `did:web:<DOCS_SERVER_DOMAIN>`. It serves its own DID document at
`/.well-known/did.json` with a `#docs` service entry pointing at itself, so pear
can resolve the forward target via standard did:web resolution.

## Environment

Set these in `dev.env` (gitignored):

| Variable                 | Description                                                                              |
| ------------------------ | ---------------------------------------------------------------------------------------- |
| `DOCS_SERVER_DOMAIN`     | Public domain / did:web host, e.g. `docs-server.local.habitat.network`.                  |
| `DOCS_SERVER_PORT`       | HTTP port (default `2590`).                                                              |
| `DOCS_SERVER_SPACE_TYPE` | Space type each doc space is created under (default `network.habitat.docs`).             |
| `DOCS_SERVER_SAP_URL`    | Base URL of sap's internal port (default `http://127.0.0.1:2581`).                       |
| `DOCS_SERVER_APP_ORIGIN` | Origin of the docsv2 frontend, allowed credentialed (cookie) CORS.                       |
| `DOCS_SERVER_DB`         | sqlite path for crawled docs, org membership and server sessions (default `.docs-server/docs-server.db`). |

The frontend (`docsv2`) needs `DOCS_SERVER_DID` (`did:web:<DOCS_SERVER_DOMAIN>`)
at build time, from which it derives the docs server's HTTP base URL
(`__DOCS_SERVER_URL__`) that its fetcher points at, and which it resolves to
find this server's `/org/login` endpoint.

## Org bootstrap

sap must hold an OAuth session for an org before it can proxy calls for it. The
docs server exposes `GET /org/login?handle=<org handle>`, which kicks off sap's
`/org/add` flow for that org and redirects the admin to Habitat to approve. The
docsv2 login page links here ("add this app"). Once approved, sap tracks the
org session, the org appears in `/org/list`, and its members can use docs.

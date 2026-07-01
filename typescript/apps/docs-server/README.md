# docs-server

A small TypeScript (Hono) server that owns the canonical state of collaborative
documents for one org.

## Architecture

- Makes authenticated pear calls **through `cmd/sap`**, which holds the org's
  OAuth session. Every XRPC call is sent to sap's proxy (`POST /proxy/<nsid>`)
  with the org DID in a `Habitat-Did` header; sap attaches the org's access
  token and `Habitat-Auth-Method: oauth` before forwarding to pear. The docs
  server holds no credential of its own — it only resolves the configured org
  handle to its DID at startup (a public lookup).
- Is the **sole writer** of the canonical doc record. It writes
  `network.habitat.docs` records into an org-owned space via
  `network.habitat.space.putRecord`.
- Receives CRDT updates from frontends as **service-auth XRPC calls**. Frontends
  call `network.habitat.doc.createDoc` / `network.habitat.doc.updateDoc` with an
  `Atproto-Proxy: did:web:<docs-domain>#docs` header; pear validates the caller,
  signs a service-auth JWT, and forwards the request here. The server fully
  verifies that JWT (`@atproto/crypto`, resolving the caller's public DID doc)
  before applying the update.
- Keeps each document's Yjs state **in memory** so updates don't refetch on every
  edit. Merges with `Y.applyUpdateV2` and writes the full re-encoded state back.
- Runs a **crawler** that subscribes to sap's outbox channel over its internal
  websocket port, acking every message it receives. When it sees a doc's
  markdown record it persists the doc to a local **sqlite** table and calls
  `network.habitat.relationship.listSubjects` (proxied through sap) to record
  which members may read it. `listDocs` is served from this store and filtered
  to the caller, so a member only ever sees docs they have permission to. Unacked
  messages are redelivered by sap on the next connection.

## DID

The server is `did:web:<DOCS_SERVER_DOMAIN>`. It serves its own DID document at
`/.well-known/did.json` with a `#docs` service entry pointing at itself, so pear
can resolve the forward target via standard did:web resolution.

## Environment

Set these in `dev.env` (gitignored):

| Variable                 | Description                                                                  |
| ------------------------ | ---------------------------------------------------------------------------- |
| `DOCS_SERVER_DOMAIN`     | Public domain / did:web host, e.g. `docs-server.local.habitat.network`.      |
| `DOCS_SERVER_ORG_HANDLE` | The org this server acts on behalf of; resolved to the org DID at startup.   |
| `DOCS_SERVER_PORT`       | HTTP port (default `2590`).                                                  |
| `DOCS_SERVER_DATA_DIR`   | Where the crawl database is persisted (default `.docs-server`).              |
| `DOCS_SERVER_SPACE_TYPE` | Space type each doc space is created under (default `network.habitat.docs`). |
| `DOCS_SERVER_SAP_URL`    | Base URL of sap's internal port (default `http://127.0.0.1:2581`).           |
| `DOCS_SERVER_CRAWL_DB`   | sqlite path for the crawl store (default `<DATA_DIR>/crawl.db`).             |

The frontend (`docsv2`) needs `DOCS_SERVER_DID` (`did:web:<DOCS_SERVER_DOMAIN>`)
at build time so it can set the `Atproto-Proxy` header.

## Setup

The org must be added to sap (`POST /org/add`) and have completed sap's OAuth
flow so sap tracks a session for it. Once sap can proxy for the org DID, the
docs server needs no separate authorization — it resolves the org handle to its
DID at startup and routes all authenticated calls through sap.

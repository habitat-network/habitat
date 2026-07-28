# docs-server

A small TypeScript (Hono) server that owns the canonical state of collaborative
documents for the orgs sap manages.

## Architecture

- Makes authenticated pear calls **through `cmd/sap`**, which holds OAuth
  sessions for the orgs it manages. Every XRPC call is sent to sap's proxy
  (`POST /proxy/<nsid>`) with the target org DID in a `Habitat-Did` header; sap
  attaches that org's access token and `Habitat-Auth-Method: oauth` before
  forwarding to pear. The docs server holds no credential and no per-org config.
- Resolves the **caller's org from membership**: sap's `/org/list` names the
  managed orgs, and `network.habitat.relationship.listSubjects` on each org's
  self space (`ats://<org>/network.habitat.organization/self`, relation
  `reader`) yields its members. The resulting user→org map is cached briefly
  and consulted on every request.
- Is the **sole writer** of the canonical doc record. It writes
  `network.habitat.docs` records into a space owned by the caller's org via
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
  markdown record it persists the doc's space URI and title to a local
  **sqlite** table. Permissions are **not** indexed: `listDocs` resolves them
  on demand via `network.habitat.relationship.listObjects` (the doc spaces the
  caller holds the reader role on, filtered by space type) and intersects that
  with the crawled titles. Unacked messages are redelivered by sap on the next
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
| `DOCS_SERVER_DB`         | sqlite path for crawled docs and org membership (default `.docs-server/docs-server.db`). |

The frontend (`docsv2`) needs `DOCS_SERVER_DID` (`did:web:<DOCS_SERVER_DOMAIN>`)
at build time so it can set the `Atproto-Proxy` header and resolve this server's
`/org/login` endpoint.

## Org bootstrap

sap must hold an OAuth session for an org before it can proxy calls for it. The
docs server exposes `GET /org/login?handle=<org handle>`, which kicks off sap's
`/org/add` flow for that org and redirects the admin to Habitat to approve. The
docsv2 login page links here ("add this app"). Once approved, sap tracks the
org session, the org appears in `/org/list`, and its members can use docs.

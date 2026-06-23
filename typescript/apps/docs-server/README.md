# docs-server

A small TypeScript (Hono) server that owns the canonical state of collaborative
documents for one org.

## Architecture

- Holds an **org credential** (OAuth, the same flow as `internal/sap/org_manager.go`):
  a confidential client (`private_key_jwt`) that completes an authorization-code
  flow once and then auto-refreshes. Every request to pear carries
  `Authorization: Bearer <token>` + `Habitat-Auth-Method: oauth`.
- Is the **sole writer** of the canonical doc record. It writes
  `network.habitat.docs` records into an org-owned space via
  `network.habitat.space.putRecord`.
- Receives CRDT updates from frontends as **service-auth XRPC calls**. Frontends
  call `network.habitat.doc.createDoc` / `network.habitat.doc.updateDoc` with an
  `Atproto-Proxy: did:web:<docs-domain>#docs` header; pear validates the caller,
  signs a service-auth JWT, and forwards the request here. The server fully
  verifies that JWT (`@atproto/crypto`, resolving the caller's DID doc through
  pear) before applying the update.
- Keeps each document's Yjs state **in memory** so updates don't refetch on every
  edit. Merges with `Y.applyUpdateV2` and writes the full re-encoded state back.

## DID

The server is `did:web:<DOCS_SERVER_DOMAIN>`. It serves its own DID document at
`/.well-known/did.json` with a `#docs` service entry pointing at itself, so pear
can resolve the forward target via standard did:web resolution.

## Environment

Set these in `dev.env` (gitignored):

| Variable | Description |
| --- | --- |
| `DOCS_SERVER_DOMAIN` | Public domain / did:web host, e.g. `docs-server.local.habitat.network`. |
| `DOCS_SERVER_ORG_HANDLE` | The org this server holds a credential for. |
| `DOCS_SERVER_PEAR_HOST` | Base URL of the org's pear, e.g. `https://pear.local.habitat.network`. |
| `DOCS_SERVER_PORT` | HTTP port (default `2590`). |
| `DOCS_SERVER_DATA_DIR` | Where the credential + signing key are persisted (default `.docs-server`). |
| `DOCS_SERVER_SPACE_SKEY` | Space key for the docs space (default `docs`). |

The frontend (`docsv2`) needs `DOCS_SERVER_DID` (`did:web:<DOCS_SERVER_DOMAIN>`)
at build time so it can set the `Atproto-Proxy` header.

## One-time bootstrap

Start the server, then visit `https://<DOCS_SERVER_DOMAIN>/oauth/login` as an org
admin to grant the org credential. The access/refresh tokens are persisted to
`DOCS_SERVER_DATA_DIR` so restarts don't require re-authorization.

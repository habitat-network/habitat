# Convex + atproto Permissioned-Spaces Sync Prototype

Status: draft
Date: 2026-09-14
Scope: prototype / spike-grade design — not production hardening

## Goal

Prototype `typescript/apps/convex-test` (currently a stock Convex + TanStack Start
scaffold) as a client of habitat's proposal-0016 permissioned-spaces sync
protocol, so that:

1. A user can log into the Convex app via their atproto identity, brokered by
   `cmd/sap`.
2. Records from a space the user can access are synced into Convex tables via
   sap's webhook event stream.
3. Mutations made in the Convex app are written back to the space via
   `network.habitat.space.putRecord`, relayed through sap.

We deliberately do **not** build a new Go backend in this pass. `cmd/sap`
already implements the proposal-0016 sync protocol (crawl, verify, outbox,
webhook delivery, OAuth session relay) and is the right integration point.
Whether a purpose-built Go server (using `pkg/sap` directly, shaped more like
Convex's function model) is worth building is a decision for later, informed
by what this prototype exposes as painful or missing in the sap-relay
approach.

## Non-goals (out of scope for this prototype)

- Multi-user session management in the app (one logged-in DID at a time).
- Conflict resolution beyond last-write-wins / a `failed` status flag.
- Full lexicon ref/union resolution in generated Convex schemas (see
  "Schema generation" below).
- Production auth hardening (the `return_to` redirect, sap's internal-port
  trust model, etc. — inherited as-is from the existing sap/chalk pattern).
- Registering `convex-test` as a Moon-managed project (can follow later if
  the prototype graduates).

## Architecture overview

```
Browser (convex-test, TanStack Start)
   │ 1. POST /session/add (via server fn) → PDS authorize URL
   │ 2. browser → PDS OAuth → sap /oauth-callback
   │ 3. sap redirects browser → /session/callback?did=...
   ▼
sap (tunneled via ngrok: internal port + public /oauth-callback)
   │                                              ▲
   │ webhook POST (outbox messages)               │ POST /proxy/network.habitat.space.putRecord
   ▼                                              │ (relayed using the DID's stored OAuth session)
Convex (cloud dev deployment)
   ├─ HTTP action  /sap-webhook        → upserts into the collection's table
   ├─ HTTP action  /session/callback   → sets signed session cookie {did}
   ├─ mutation     records.write       → optimistic table write + schedules action
   └─ action       records.pushToSap   → calls sap /proxy/.../putRecord, reconciles status
```

sap is the only thing that talks to pear. Convex never calls pear directly —
this matches the existing precedent in `typescript/apps/chalk`, which relays
all pear access through sap's internal-port proxy and never holds pear
credentials itself.

## Auth flow

Reuses the pattern already proven in `typescript/apps/chalk`
(`src/server/sapClient.ts`, `src/routes/login.tsx`,
`src/routes/session.callback.tsx`) almost verbatim, ported to
`convex-test`'s TanStack Start server functions — **not** implemented as
Convex functions, since Convex has no browser-cookie-session primitive and
this flow is already proven working code:

1. A TanStack server function (`startLogin`) POSTs to sap's internal
   `/session/add` with `{ handle, return_to: "<site-url>/session/callback" }`,
   authenticated with a shared `SAP_INTERNAL_AUTH_SECRET` (env var, never
   exposed to the browser). sap resolves the handle to a DID, starts the PDS
   OAuth flow server-side, and returns `{ redirect_url }`.
2. The browser navigates to `redirect_url` (the PDS's own authorize page).
3. After consent, the PDS redirects to sap's public `/oauth-callback`. sap
   completes the token exchange, persists the session, and 303-redirects the
   browser to the `return_to` URL from step 1, appending `?did=<did>`.
4. `convex-test`'s `/session/callback` route reads `did` from the query
   string and sets it in a signed session cookie (mirroring chalk's
   `useAppSession()`).
5. Every subsequent Convex query/mutation call from the client passes `did`
   explicitly as an argument, read server-side from the cookie via a small
   "whoami" loader. No Convex Auth provider is wired up for this prototype.

## Schema generation from lexicons

We declare upfront which collections this prototype cares about (a small
config, e.g. `convex-test/collections.config.ts`), each entry naming:
- the NSID (e.g. `network.habitat.space.note` — placeholder, actual
  collections TBD when implementing)
- the path to its lexicon JSON under `lexicons/`
- the target Convex table name

A generator script (`convex-test/scripts/gen-schema.ts`, run manually — not
part of `moon :generate`, since this is prototype-local tooling, not a
repo-wide generated artifact) reads each declared collection's `main` record
definition and produces one Convex table per collection, writing into
`convex/schema.ts`. Type mapping:

| Lexicon type | Convex validator |
|---|---|
| `string` | `v.string()` |
| `integer` | `v.number()` |
| `boolean` | `v.boolean()` |
| `bytes` | `v.bytes()` |
| `cid-link` | `v.string()` (CID as string) |
| `array` | `v.array(<item validator>)` |
| `object` (inline) | `v.object({...})`, recursively mapped |
| `$ref` / `union` / `blob` | `v.any()` — **not resolved** across lexicon files |

Refs/unions falling back to `v.any()` is a deliberate simplification to keep
the generator small; it avoids cross-file lexicon resolution for a
prototype. Every generated table also gets fixed metadata columns:

- `uri: v.string()` (indexed, unique — the atproto record URI)
- `cid: v.string()`
- `rev: v.string()`
- `did: v.string()` (repo owner)
- `syncStatus: v.union(v.literal("pending"), v.literal("confirmed"), v.literal("failed"))`

## Sync flow (sap → Convex)

`cmd/sap` is run with `--webhook-url` pointing at the Convex deployment's
`/sap-webhook` HTTP action. Each delivered outbox message
(`{id, uri, value}`):

1. Parses the collection NSID out of `uri` (`at://did/collection/rkey`).
2. Looks up the target table from the same declared-collections config used
   by the schema generator. Messages for undeclared collections are logged
   and skipped (not an error — sap may be tracking a space with more
   collections than this prototype cares about).
3. Upserts a row into that table, keyed by the `uri` index, spreading
   `value`'s fields into the row and setting `cid`/`rev`/`did` from the
   message metadata, `syncStatus: "confirmed"`.
4. Returns HTTP 200. A 200 response is sap's outbox ack signal — if the
   handler throws or times out, sap retries the same message (per
   `cmd/sap/webhook.go`'s backoff/retry behavior), so the handler must be
   idempotent (upsert-by-uri already gives us that).

## Mutation flow (Convex → pear, via sap)

1. Client calls a mutation (e.g. `records.write({ collection, rkey?, fields })`).
2. The mutation writes optimistically into the mapped table
   (`syncStatus: "pending"`) and schedules a Convex action.
3. The action POSTs to sap's internal
   `/proxy/network.habitat.space.putRecord` (space + collection + record
   value), authenticated as the internal caller, with `Habitat-Did` set to
   the acting user's DID — sap relays the call to pear using that DID's
   stored OAuth session, exactly as it already does for chalk.
4. On success, the action updates the row's `cid`/`rev` and sets
   `syncStatus: "confirmed"`. On failure, `syncStatus: "failed"`.
5. The same write will typically also arrive later via the webhook path
   (step above); the upsert-by-uri behavior makes that a no-op against the
   already-current row.

No automatic rollback of the optimistic row on failure — surfacing
`syncStatus: "failed"` in the UI is sufficient for this prototype.

## Local dev topology

Convex functions (even under `convex dev`) execute in Convex's cloud, not on
the developer's machine, so they cannot reach a `sap`/`pear` bound only to
`localhost`/Caddy. We extend the existing ngrok tunnel already used by
`pear:dev` (for PDS OAuth callbacks) to also expose sap's internal and
public ports. `SAP_INTERNAL_URL` (Convex env var) and sap's own
`--webhook-url` both point at the ngrok domain.

## Testing / validation

This is prototype code; no coverage-threshold or CI expectations apply
(`convex-test` isn't Moon-managed). Validation is manual end-to-end:

1. Log in via the app, confirm `did` lands in the session cookie.
2. Confirm sap tracks the configured space and webhook deliveries land as
   rows in the matching Convex table.
3. Write a mutation in the app, confirm it appears in pear (e.g. via
   `listRecords`) and that `syncStatus` transitions `pending` → `confirmed`.

## Open questions to resolve during implementation

- Exact collection(s) to target first (placeholder `network.habitat.space.note`
  above — pick from what's actually usable in a test space today).
- Whether sap's webhook POST carries any auth/signature we should verify in
  the Convex HTTP action (needs checking against `cmd/sap/webhook.go` at
  implementation time).

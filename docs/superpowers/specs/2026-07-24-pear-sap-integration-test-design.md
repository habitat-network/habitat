# pear ↔ sap integration test — design

**Date:** 2026-07-24
**Status:** Approved for implementation

## Goal

Add `integration/sap_test.go`: an end-to-end test that runs the real `cmd/pear`
and `cmd/sap` binaries (as Docker containers) and exercises the full sync
mechanism between them under concurrency and degraded network conditions.

The test must:

- Drive pear with a test client that authenticates via the **JWT-bearer grant**
  (pear's `--builtin_app` allow-list) to create orgs, mint identities, create
  spaces, and put records.
- Have sap sync those records, authenticating to pear **also via JWT-bearer**
  (a new per-session auth method in sap).
- Use **toxiproxy** to simulate bad network conditions on the `notifyWrite`
  push path specifically.
- Verify sap has eventually synced **every** record put into pear, observed
  through sap's **outbox websocket** (not by peeking at sap's DB).
- Exercise **high concurrency** on both pear (many concurrent writers across
  repos/spaces) and sap (parallel sync workers, notify chaos).

Out of scope: OAuth/PDS login flows, the existing testcontainers harness
(`testcontainers.go`, PDS/selenium/frontend) — this test stands alone.

## Background (current state)

- **sap** authenticates to pear via per-DID **OAuth sessions**
  (`internal/sap/session.Store` backed by `oauth.ClientApp`). The crawler needs
  `ClientForSession(ctx, did) (*http.Client, error)`; the syncer and registrar
  need `ClientForSpace(ctx, space) (*http.Client, error)`. `ClientForSpace`
  picks a candidate DID and delegates to `ClientForSession`, so **all** client
  construction funnels through `ClientForSession`.
- sap's `cmd/sap` `serve()` is **HTTP-only** (no TLS). It exposes a public port
  (`notifyWrite`, `notifySpaceDeleted`, `oauth-callback`, `client-metadata.json`)
  and an internal port (`/channel` outbox websocket, `/session/*`, `/proxy/`).
- **pear** already supports the JWT-bearer grant: `--builtin_app <clientID>`
  registers an allow-listed client. On a token request pear fetches the client's
  `client-metadata.json` (at the clientID URL, over HTTPS) for its JWKS,
  validates the signed assertion, and issues an access token whose `sub` is the
  assertion's `subject` DID (`internal/oauthserver/{oauth_server,fosite_storage,
  jwt_bearer_store}.go`). The subject DID must resolve to an org/member. The
  grant uses `GrantTypeJWTBearerCanSkipClientAuth: true`: the client is proven
  solely by the assertion's `iss` (checked against the allow-list) and its
  signature. pear's fosite `client` wrapper (`fosite_client.go`) currently
  hardcodes `IsPublic() = true` and ignores `token_endpoint_auth_method`; the
  existing jwt-bearer test signs with **RS256**.
- **`pdsclient.ClientMetadata`** already carries every confidential-client field
  (`token_endpoint_auth_method`, `token_endpoint_auth_signing_alg`,
  `dpop_bound_access_tokens`, `scope`, `response_types`, `redirect_uris`,
  `grant_types`, `jwks`); pear-as-a-PDS-*client* (`oauth_client.go`) already
  emits a spec-compliant confidential-client doc (`private_key_jwt`, `ES256`,
  `dpop_bound_access_tokens: true`, ES256 JWKS). So the document shape exists —
  sap just needs to serve one, and pear-as-*server* needs to honor it.
- **hive** mints identities as `did:web:<opaqueid>.<subdomain>` where `opaqueid`
  is a random 6-char string — so member/org DIDs live at **runtime-random
  subdomains** of the pear domain.
- sap's syncer **must** resolve author/owner DIDs to verify commit signatures
  (`internal/sap/syncer/verify.go`); resolution failure is a hard, retrying sync
  error. So sap genuinely needs **wildcard** did:web resolution against pear.

## Architecture

All processes run as Docker containers on one user-defined network. The test
process runs on the host and reaches the stack through Caddy on `:443`.

```
 go test (host)
   │  ├─ https ─► pear.local.habitat.network ─► caddy ─► pear      (drive pear, JWT-bearer)
   │  └─ ws ─────► 127.0.0.1:<pub sap internal port>               (outbox websocket)
   │
 pear ─ https(notifyWrite) ─► sap.local…/xrpc/…notifyWrite ─► caddy ─► toxiproxy ─► sap:public
 sap  ─ https(crawl/sync/token, did:web resolve) ───────────► caddy ─► pear
```

### Containers

- **dnsmasq** — wildcards `*.local.habitat.network → <caddy ip>`. Every other
  container's resolver (`--dns`) points here. This is what lets containers
  resolve the runtime-random `did:web:<opaqueid>.<org>.pear.local.habitat.network`
  hosts (Docker's embedded DNS can't wildcard, and `127.0.0.1` inside a
  container is the container itself). Config is a single `address=/…/` line.
- **caddy** — terminates TLS for the whole stack with `tls internal` (its own
  CA). Routes (mirroring the repo `Caddyfile`):
  - `*.*.pear.local.habitat.network`, `*.pear.local.habitat.network`,
    `pear.local.habitat.network` → `pear:8000` (on-demand TLS for random hosts).
  - `sap.local.habitat.network`:
    - path `/xrpc/network.habitat.space.notifyWrite` → `toxiproxy:<port>` →
      `sap:<public>` (chaos applies **only** to notifyWrite).
    - all other paths → `sap:<public>` directly (client-metadata fetch etc. stay
      clean).
  - Caddy's root CA is exported and mounted into pear and sap via
    `SSL_CERT_FILE` so their outbound HTTPS trusts caddy-issued certs.
- **toxiproxy** — one proxy in front of sap's public port; driven by the Go
  client (`github.com/Shopify/toxiproxy/v2/client`) to add latency / timeout /
  reset toxics mid-test.
- **postgres** — one container; pear and sap each get a database (concurrency-
  safe, unlike sqlite with its single-writer lock).
- **pear** — built from `cmd/pear/Dockerfile`. `HABITAT_DOMAIN=
  pear.local.habitat.network`, HTTP on `:8000`, `--builtin_app` = sap's clientID
  **and** the test client's clientID, Postgres DSN, space signing key, etc.
- **sap** — built from a **new** `cmd/sap/Dockerfile`. HTTP public/internal
  ports, `Endpoint=https://sap.local.habitat.network`, Postgres DSN, plus the
  new JWT-bearer credentials (below). Public port is fronted by toxiproxy;
  internal port is published to the host for the outbox websocket.

### Orchestration

`testcontainers-go` (already a dependency), in a fresh helper file — not the
existing `testcontainers.go`. Start order handles the dnsmasq→caddy IP
dependency: start caddy, read its network IP, start dnsmasq pointing at it, then
start pear/sap with `--dns=<dnsmasq ip>`. Container logs are dumped on failure.

## sap code changes: JWT-bearer as a per-session auth method

The auth method is chosen **per session**, not by a startup flag. sap is
configured with JWT-bearer *credentials* at startup (instance-level), but each
session records which method it uses, so sap serves OAuth and JWT-bearer
sessions simultaneously.

### `internal/sap/session`

- Add an `AuthMethod` column to the `session` row: `"oauth"` (default) |
  `"jwt-bearer"`.
- `Store.Add(ctx, did, sessionID, method)` persists the method.
- Introduce a small client-builder seam. `Store` holds:
  - the existing OAuth `getter` (builds a client from `sessionID` + stored
    `HostURL`), and
  - a new **JWT-bearer builder** (nil unless sap is configured with credentials).
  `ClientForSession` loads the row and dispatches on `AuthMethod`. `ClientForSpace`
  is unchanged (it still funnels through `ClientForSession`).
- JWT-bearer builder behaviour, per DID:
  1. Resolve the DID's DID doc, read its `habitat` service endpoint → pear base
     URL (so no per-session host wiring is needed).
  2. Mint an access token via `grant_type=urn:ietf:params:oauth:grant-type:
     jwt-bearer` against `<pear>/oauth/token`, with the signed assertion's
     `subject = did`, `iss/sub(client) = sap clientID`, `aud = <pear>/oauth/token`,
     `jti` unique, `iat` set, signed **ES256** by sap's P-256 key. Cache one
     token source per DID for refresh. (The existing `jwt_bearer_test.go` uses
     `golang.org/x/oauth2/jwt` with RSA; sap needs ES256, so it either uses that
     library with an ES256 key or signs the assertion directly with
     `atcrypto`/`go-jose` — decided during implementation.)
  3. Return an `*http.Client` whose transport resolves relative `/xrpc/...` URLs
     against the pear base URL, attaches `Authorization: Bearer <token>`, and
     sets `Habitat-Auth-Method: oauth` (so pear's `OAuthServer.CanHandle`
     accepts it; pear does not yet enforce DPoP).

### `cmd/sap`

- New startup config (flags/env, instance-level): `--jwt_signing_key` (an
  **ES256 / P-256** private key; multibase like sap's existing `--secret`, or
  PEM) and `--jwt_client_id` (sap's clientID URL,
  `https://sap.local.habitat.network/client-metadata.json`). When set, sap
  constructs the JWT-bearer builder and wires it into `session.Store`.
- Serve a **spec-compliant atproto confidential-client** `client-metadata.json`
  (per <https://atproto.com/specs/oauth>), built from `pdsclient.ClientMetadata`:
  - `client_id` = the clientID URL
  - `token_endpoint_auth_method: "private_key_jwt"`
  - `token_endpoint_auth_signing_alg: "ES256"`
  - `dpop_bound_access_tokens: true`
  - `scope: "atproto"`
  - `response_types: ["code"]`
  - `grant_types: ["authorization_code", "refresh_token",
    "urn:ietf:params:oauth:grant-type:jwt-bearer"]` (the first two make the doc
    spec-shaped; the jwt-bearer grant is the one sap actually exercises)
  - `redirect_uris: ["https://sap.local.habitat.network/oauth-callback"]`
  - `jwks`: sap's **ES256** public key
  sap's `--jwt_signing_key` is therefore an **ES256** (P-256) key, and its
  jwt-bearer assertions are signed ES256. The ES256-signed assertion is itself
  the client's `private_key_jwt` proof, so no separate `client_assertion` is
  needed (the grant keeps `CanSkipClientAuth`).
- `/session/add` gains an `auth_method` field. The OAuth callback records
  `oauth`; a JWT-bearer session is added with an empty `sessionID` and
  `auth_method=jwt-bearer`.

No changes to crawler, syncer, registrar, or outbox — they consume the
unchanged `Clients` interfaces.

## pear code changes: confidential-client support

Goal: an approved jwt-bearer client (sap) presents a spec-compliant
**confidential-client** metadata document, and pear honors it — validating and
reflecting the confidential-client properties rather than treating every client
as public. This advances the atproto-OAuth-server compliance effort (confidential
clients were the outstanding item).

- **`fosite_client.go`**: derive client properties from the metadata instead of
  hardcoding them:
  - `IsPublic()` returns `false` when `token_endpoint_auth_method ==
    "private_key_jwt"` (true otherwise, preserving public-client behavior).
  - Expose the token-endpoint auth method and signing alg to fosite (implement
    the `fosite.OpenIDConnectClient` / JWT-profile client interfaces fosite
    consults for private_key_jwt), backed by the metadata's
    `token_endpoint_auth_signing_alg` and `jwks`.
- **`fosite_storage.go`**: the jwt-bearer key path (`GetPublicKeys`) already
  reads `metadata.Jwks`; confirm it selects the **ES256** key. Optionally
  validate that an allow-listed client's metadata conforms to the confidential-
  client spec (has `private_key_jwt`, a JWKS, `dpop_bound_access_tokens: true`)
  before honoring its grant, rejecting non-conformant docs.
- **Keep `GrantTypeJWTBearerCanSkipClientAuth: true`**: the ES256-signed grant
  assertion already proves the client (its `iss` is the clientID and it is
  signed by the client's key = private_key_jwt-equivalent proof), so no separate
  `client_assertion` is required. The exact fosite interaction (does marking the
  client confidential make fosite demand a separate `client_assertion` on the
  jwt-bearer grant?) is pinned down by tests in `internal/oauthserver` before
  relying on it — if fosite forces separate client auth for confidential
  clients, either scope `IsPublic()=false` to non-grant flows or have sap also
  send a `client_assertion`.
- All changes are driven by `internal/oauthserver` unit tests (extend
  `jwt_bearer_test.go`): an approved **ES256** confidential client obtains a
  token via the jwt-bearer grant; a public client still works; a non-conformant
  approved client is rejected (if validation is added).

## Test flow (`integration/sap_test.go`)

1. **Bootstrap infra**: bring up postgres, caddy, dnsmasq, toxiproxy, pear, sap;
   wait for pear `/health` and sap health. Register the toxiproxy proxy (no
   toxics yet).
2. **Test JWT-bearer client**: a host-side helper that serves its own
   spec-compliant confidential-client `client-metadata.json` (ES256 JWKS,
   `private_key_jwt`, `dpop_bound_access_tokens: true`) over a caddy-fronted
   domain, listed in pear's `--builtin_app`, and mints per-subject ES256 tokens.
   Used as an org admin/member to call pear's XRPC. (Its metadata host is served
   through caddy so pear can fetch the JWKS over trusted HTTPS.)
3. **Bootstrap data over pear XRPC** (via JWT-bearer): create an org; mint N
   member identities (the repo owners); create M spaces owned across those
   identities.
4. **Register sap sessions**: `POST /session/add` for each owner DID with
   `auth_method=jwt-bearer`. sap crawls, discovers spaces/repos, registers for
   notifications.
5. **Connect the outbox websocket** to sap's published internal port; ack each
   message as it arrives; record delivered URIs.
6. **Concurrent workload**: many goroutines `putRecord` across the repos/spaces —
   interleaving fresh creates and updates to existing records — while toxiproxy
   degrades the notifyWrite path (latency, then timeouts/resets, then healthy
   again). Covers: multiple records under different repos, updates to existing
   records, spaces created before vs. after sap connected.
7. **Assert**: every created record URI eventually appears on the outbox exactly
   once (via `require.Eventually` over the acked set); the final delivered set
   equals the created set. Optionally assert repos settle with verified hashes.

## Bugs expected / to fix as found

Wiring the real path (unlike the in-process `internal/sap/sap_test.go`, which
relays notifications by hand) is expected to surface real bugs. Known suspects:

- `internal/notify/notifier.go` sends `Hash: ""` on `notifyWrite`, while sap's
  verification path expects the host's signed commit hash — verify whether the
  incremental sync still converges (it should re-verify via `listRepoOps` /
  `getRepo`), and fix if the empty hash breaks convergence.
- Concurrency races in sap's crawl/sync/outbox under real parallelism.
- JWT-bearer edge cases (token refresh, aud/host resolution).

Each fix lands as a focused commit in sap or pear with a clear message.

## Testing / verification

- The integration test itself is the primary artifact; it is opt-in (Docker +
  build-tag/`moon` task, like the existing integration module) and not part of
  `moon ci` unit runs.
- Any sap/pear bug fix gets unit coverage in the owning package where practical
  (per `go-tdd` / `go-tests` conventions).
- New sap session code (`AuthMethod` dispatch, JWT-bearer builder) gets unit
  tests in `internal/sap/session` using an interface fake for the token mint /
  directory, mirroring existing session tests.

## Open risks

- **Docker networking on macOS**: the dnsmasq→caddy wildcard path is the load-
  bearing, least-standard piece. Mitigation: start-order handling and a health
  gate that fails fast with container logs.
- **Caddy CA trust in containers**: relies on exporting caddy's root and setting
  `SSL_CERT_FILE`. If a container ignores it, outbound HTTPS fails — surfaced
  immediately by the health gate.

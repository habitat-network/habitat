# Pear setup extraction and server test harness

Date: 2026-08-21

## Problem

`cmd/pear/main.go` is 663 lines that interleave three unrelated jobs: reading
CLI flags, constructing roughly 25 components, and registering roughly 80
routes. Nothing outside `main` can build a Pear instance, so every server test
hand-assembles its own subset of the world.

That hand-assembly is the real cost. `internal/relationship/server_test.go` and
`internal/spaces/server/server_test.go` each build a bespoke store graph and
authenticate through `authntest.NewSuccessValidatorWithOrg`. That stub's
`Request(options ...utils.Opt[authn.EndpointOptions])` **discards its options**
and returns a validator that always succeeds. Every `authn.WithSpace(...)` and
`authn.WithMethods(...)` constraint a handler declares is therefore inert under
test: space-role enforcement and scope enforcement are not covered anywhere in
the server test suite.

The goal is to make a real, fully wired Pear instance cheap enough to stand up
in a unit test, so server tests exercise real authentication and real
authorization instead of mocking past them.

## Design

### 1. `internal/pearsetup`

A new package that owns assembly. It does not import `urfave/cli`; flag parsing
stays in `cmd/pear`.

**`config.go`** — `Config`, plain values only:

| Field | Notes |
|---|---|
| `Domain`, `HiveDomain`, `Port`, `HTTPSCerts`, `Debug` | `HiveDomain` defaults to `Domain`, `Port` to `8000` |
| `DB` | DSN string, as `internal/db` accepts today |
| `OAuthServerSecret`, `PDSCredEncryptKey` | `[]byte`, already parsed by the caller |
| `OAuthClientSecret`, `PDSOAuthClientURI` | client URI defaults to `https://` + `Domain` |
| `SpaceSigningKey` | `atcrypto.PrivateKey`, already parsed |
| `GoogleClientID`, `GoogleClientSecret` | Google login stays optional; absent means the provider is not registered |
| `AdminPasswordHash` | argon2id hash; generation and hashing stay in `cmd/pear` |
| `UIDevProxy`, `BuiltinApps`, `BlobBucket` | as today |

Plus injection points, which are production seams that tests also use:

- `Directory identity.Directory` — defaults to `identity.DefaultDirectory()`.
  Load-bearing: the default resolves `did:plc` over the network, so without
  this seam every harness test makes live HTTP requests.
- `FGA fgastore.Store` — defaults to the existing dialect-dependent choice
  (Postgres shares the main DSN, SQLite gets a sibling `.fga.db` file).
- `Bucket *blob.Bucket` — defaults to opening `Config.BlobBucket`. `Pear` gains
  a `Close() error` that closes the bucket and the libp2p host, since `New` now
  owns resources that `main`'s deferred closes used to.
- `DisableP2P`, `DisableUI` — negative names deliberately, so the zero value is
  production behavior and only the harness has to opt out. P2P binds a libp2p
  host; UI needs an embedded or proxied asset handler. Neither belongs in a
  unit test.

`withDefaults()` applies every fallback above, so `New` sees a complete config.

**`pear.go`** — `New(ctx context.Context, cfg Config) (*Pear, error)`, holding
what is currently `main.go` lines 111–380. `Pear` exports each store and server
as a field (`DB`, `Hive`, `OrgStore`, `SpacesStore`, `PermStore`, `OAuthServer`,
`Validator`, `Router`, and the rest), so tests can both drive HTTP and seed
fixtures directly.

**`routes.go`** — `(*Pear).registerRoutes()` holds the route table.
`(*Pear).Handler() http.Handler` returns the router.

**`run.go`** — `(*Pear).Run(ctx) error`: the errgroup, the OAuth GC loop, the
HTTP/HTTPS listener, and graceful shutdown.

**`cmd/pear/migrations` moves to `internal/pearsetup/migrations`**, carrying its
`embed.FS` and the blank import that registers migrations. This is required, not
incidental: an `embed.FS` under `cmd/` cannot be reached from a library package,
and tests must run against the same schema production runs against.

`cmd/pear/main.go` is left with telemetry setup, flags to `Config`, `New`, and
`Run` — roughly 120 lines. Telemetry stays in `main` because it is
process-global and configures the default logger.

### 2. Minting real OAuth tokens

Habitat's access tokens are stateless ES256 JWTs. `NewOAuthServer` derives an
ECDSA P-256 key from `GlobalSecret` and composes
`compose.NewOAuth2JWTStrategy`, and `ValidateRaw` verifies through
`OAuth2StatelessJWTIntrospectionFactory`. No database session backs an access
token, so a valid token can be minted from the secret alone.

`OAuthServer` retains its fosite strategy as a field and gains:

```go
func (o *OAuthServer) MintAccessToken(
    ctx context.Context,
    did syntax.DID,
    scopes []string,
    ttl time.Duration,
) (string, error)
```

It mints through the same strategy the token endpoint uses, so a minted token is
indistinguishable from an issued one and cannot drift from the claim shape in
`fosite_session.go`.

Rejected alternative: have the test harness re-derive the ECDSA key and sign its
own JWT. It leaves `oauthserver` untouched but duplicates the claim and header
shape (`typ: oauth+JWT`, subject, audience, expiry), which will rot silently the
first time the session changes.

Note that `ValidateRaw` calls `orgStore.GetOrgForDID`, which falls back to the
"everyone" org rather than erroring for unknown DIDs. A minted token therefore
authenticates any DID; org membership is something the harness sets up
explicitly when a test needs it.

### 3. `internal/pearsetup/testutil`

Named to match the repo's existing `internal/db/testutil`,
`internal/authn/testutil`, and `internal/spaces/testutil`. It deliberately does
not live under `internal/pear`, which CLAUDE.md marks for removal.

```go
func New(t *testing.T, opts ...Option) *TestPear   // embeds *pearsetup.Pear

// actors
func (p *TestPear) NewOrg(name string) *Actor             // returns the org's admin
func (p *TestPear) NewMember(org *Actor, handle string) *Actor
func (p *TestPear) Anonymous() *Actor

// requests, through the full mux: middleware, service proxy, real validator
func (p *TestPear) Do(a *Actor, r *http.Request) *http.Response
func (p *TestPear) Query(a *Actor, nsid string, params url.Values, out any) *http.Response
func (p *TestPear) Procedure(a *Actor, nsid string, body, out any) *http.Response

// options
func WithDirectory(d identity.Directory) Option
func WithFGA(s fgastore.Store) Option
func WithConfig(fn func(*pearsetup.Config)) Option
```

Defaults: temp-file SQLite via `db/testutil`, `fgastore.NewMemory`, `memblob`,
generated secrets and host signing key, a hive-backed directory with no network
fallback, `DisableP2P` and `DisableUI` set, domain `pear.test`.

`Actor` holds a DID, its org, and a lazily minted access token. Requests are
served with `httptest.NewRecorder` and `ServeHTTP` rather than a live listener —
no ports, no goroutine leaks — with the `Host` header set so the host-routed
DID document and handle endpoints resolve.

### 4. Converting the existing server tests

Every test package that authenticates through the stub validator converts, one
commit each, in this order (simplest first, so harness gaps surface early):

1. `internal/notify`
2. `internal/relationship`
3. `internal/simplespace`
4. `internal/org/server`
5. `internal/spaces/server`

That is the complete set: `grep -rln authn/testutil internal` returns exactly
these five plus `internal/forwarding/service_proxy_test.go`, which tests the
proxy middleware in isolation and is left alone.

The three packages named in earlier drafts are not in scope, because they have
no stubbed authentication to remove: `internal/clique/clique_test.go` tests the
store directly, `internal/identity/server_test.go` builds a `Server` around a
mock directory with no validator, and `instance.NewServer` takes no validator at
all. They still gain from the harness if their coverage is later extended to
routed HTTP, but converting them now would be churn with no fidelity payoff.

Because the stub validator discarded its endpoint options, some handlers will
fail under the real validator. Each failure is a finding: fix it, or document it
explicitly in the test. Reaching back for `authntest` to make a conversion pass
is not acceptable — it would reinstate exactly the gap this work exists to
close. If a package's failures turn out to be a genuine bug hunt rather than a
test port, stop and report rather than folding a behavior change into the
refactor.

`internal/authn/testutil` remains for unit tests of non-server code.
`internal/pear`'s own tests (`pear_test.go`, `pear_authz_test.go`) are left
alone; the package is slated for removal.

### 5. Verification

- `cmd/pear/pear_test.go`'s SQLite and Postgres startup tests stay unchanged in
  intent and now cover flags to `Config` to `Run`. They are the proof the
  extraction did not break boot.
- A `pearsetup` test asserts `New` wires a complete instance and `/health`
  answers.
- A `pearsetup/testutil` test asserts a minted token authenticates and that an
  actor lacking a space role is actually rejected — the case the stub validator
  could never fail.
- `.testcoverage.yml` gains `internal/pearsetup` under the existing wiring
  exclusions, alongside the `server.go` pass-through entries. `internal/pearsetup/testutil`
  is likewise excluded as test infrastructure.
- `moon :test` and `golangci-lint run` pass at the end of each commit.

## Out of scope

- No mock PDS. PDS forwarding coverage stays in the `integration/` module.
- No changes to `internal/pear` beyond leaving it as is.
- No new XRPC endpoints and no changes to handler behavior, except where a
  conversion exposes a genuine authorization bug, which is reported rather than
  bundled.

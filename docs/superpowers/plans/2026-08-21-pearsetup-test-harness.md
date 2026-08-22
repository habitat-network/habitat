# Pearsetup and Server Test Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract Pear's assembly out of `cmd/pear/main.go` into `internal/pearsetup`, then add `internal/pearsetup/testutil` so server tests exercise a real, fully wired instance with real OAuth tokens instead of a stub validator.

**Architecture:** `internal/pearsetup` owns a flag-free `Config`, a `New` that builds every component, a `routes.go` that registers the route table, and a `Run` that serves. `cmd/pear` shrinks to flag mapping. `internal/pearsetup/testutil` stands the same thing up on SQLite plus in-memory FGA and blobs, mints genuine ES256 access tokens through the OAuth server's own fosite strategy, and drives requests through the full mux.

**Tech Stack:** Go 1.26, gorm, gorilla/mux, urfave/cli v3, ory/fosite v0.49.0, OpenFGA (`internal/fgastore`), gocloud.dev/blob, testify, goose migrations.

**Spec:** `docs/superpowers/specs/2026-08-21-pearsetup-test-harness-design.md`

## Global Constraints

- Go 1.26. Entrypoints in `cmd/`, everything else in `internal/`. Never add a new top-level directory.
- `internal/pearsetup` MUST NOT import `github.com/urfave/cli/v3`. Flag parsing stays in `cmd/pear`.
- Never edit generated code (`api/habitat`, `typescript/api`). Run `moon :generate` if lexicon output is stale.
- Never remove an existing comment unless you are resolving the TODO it describes.
- Read a file before editing it.
- For renames and signature changes across many files, write a `sed` script rather than editing occurrences one at a time.
- Lint must pass: `golangci-lint run`. `bodyclose` is enabled, so every `*http.Response` you obtain in a test must have its body closed — in the harness helpers use `t.Cleanup(func() { _ = resp.Body.Close() })`.
- `sloglint` is enabled: use the `slog.*Context` variants and key-value pairs.
- Commit after every task. Commit messages follow the repo's existing `[area] Summary` style, e.g. `[pearsetup] Extract config from main`.
- Coverage thresholds from `.testcoverage.yml`: 70% package and total, 60% per file.

## File Structure

**Created:**
- `internal/pearsetup/config.go` — `Config`, its defaults, and validation. No behavior.
- `internal/pearsetup/pear.go` — `New`, the `Pear` struct, `Close`.
- `internal/pearsetup/routes.go` — `registerRoutes`, `Handler`.
- `internal/pearsetup/run.go` — `Run`: errgroup, OAuth GC, listener, shutdown.
- `internal/pearsetup/migrations/` — moved verbatim from `cmd/pear/migrations/`.
- `internal/pearsetup/pear_test.go` — wiring and `/health` coverage.
- `internal/pearsetup/config_test.go` — defaults and validation coverage.
- `internal/pearsetup/testutil/testutil.go` — `TestPear`, `New`, `Option`s.
- `internal/pearsetup/testutil/actors.go` — `Actor`, `NewOrg`, `NewMember`, `Anonymous`, `Token`.
- `internal/pearsetup/testutil/request.go` — `Do`, `Query`, `Procedure`.
- `internal/pearsetup/testutil/testutil_test.go` — proves real auth accepts and rejects.

**Modified:**
- `cmd/pear/main.go` — reduced to telemetry, flags → `Config`, `New`, `Run`.
- `internal/oauthserver/oauth_server.go` — retain the fosite strategy, add `MintAccessToken`.
- `.testcoverage.yml` — exclude the two new wiring/test-infra packages.
- Five server test files, one per conversion task.

**Deleted:**
- `cmd/pear/migrations/` (moved, not deleted outright).

---

### Task 1: Move migrations into pearsetup

The `embed.FS` holding the migrations lives under `cmd/pear`, which no library package can import. It has to move before `pearsetup` can open a database.

**Files:**
- Move: `cmd/pear/migrations/` → `internal/pearsetup/migrations/`
- Modify: `cmd/pear/main.go:66-68` (the `//go:embed` block), `cmd/pear/main.go:63` (the blank import)

**Interfaces:**
- Consumes: nothing.
- Produces: package `migrations` at `github.com/habitat-network/habitat/internal/pearsetup/migrations`, registering goose migrations by import side effect.

- [ ] **Step 1: Move the directory and fix the package's own import paths**

```bash
git mv cmd/pear/migrations internal/pearsetup/migrations
grep -rl "cmd/pear/migrations" --include='*.go' . | grep -v '^./.worktrees' | \
  xargs sed -i '' 's#habitat/cmd/pear/migrations#habitat/internal/pearsetup/migrations#g'
```

- [ ] **Step 2: Move the embed declaration out of main**

The `//go:embed` directive can only reference files in its own directory, so it moves with the files. Delete these lines from `cmd/pear/main.go`:

```go
//go:embed migrations/*.go migrations/*.sql
var embedMigrations embed.FS
```

Add to a new file `internal/pearsetup/migrations/embed.go`:

```go
package migrations

import "embed"

// FS holds the goose migration files. It lives beside the migrations because a
// //go:embed directive can only reference files in its own directory, and
// internal/pearsetup needs to hand these to db.New.
//
//go:embed *.go *.sql
var FS embed.FS
```

Remove the now-unused `"embed"` import from `cmd/pear/main.go`, and change its `db.New` call to use the new symbol:

```go
db, err := db.New(cmd.String(fDB), db.WithMigrations(migrations.FS))
```

Add the import `"github.com/habitat-network/habitat/internal/pearsetup/migrations"` and delete the old blank import line `_ "github.com/habitat-network/habitat/cmd/pear/migrations"`.

- [ ] **Step 3: Verify the migration tests still run in their new home**

Run: `go test ./internal/pearsetup/migrations/...`
Expected: PASS (`20260807064326_rewrite_legacy_space_uris_test.go` runs here now).

- [ ] **Step 4: Verify pear still boots**

Run: `go test ./cmd/pear/ -run TestStartup_Sqlite -v`
Expected: PASS. This is the guard for the whole extraction — if migrations are not registered, startup fails on a missing table.

- [ ] **Step 5: Commit**

```bash
git add -A cmd/pear internal/pearsetup
git commit -m "[pearsetup] Move migrations out of cmd/pear"
```

---

### Task 2: Config and defaults

**Files:**
- Create: `internal/pearsetup/config.go`
- Test: `internal/pearsetup/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `pearsetup.Config` (all fields below), `(Config).withDefaults() Config`, `(Config).validate() error`. Every later task reads these field names.

- [ ] **Step 1: Write the failing test**

Create `internal/pearsetup/config_test.go`:

```go
package pearsetup

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/stretchr/testify/require"
)

func TestConfigDefaults(t *testing.T) {
	c := Config{Domain: "pear.example.com"}.withDefaults()

	require.Equal(t, "pear.example.com", c.HiveDomain, "hive domain falls back to domain")
	require.Equal(t, "https://pear.example.com", c.PDSOAuthClientURI, "client URI falls back to domain")
	require.Equal(t, "8000", c.Port)
	require.NotNil(t, c.Directory, "directory defaults to the network directory")
}

func TestConfigDefaultsKeepExplicitValues(t *testing.T) {
	dir := identity.NewMockDirectory()
	c := Config{
		Domain:            "pear.example.com",
		HiveDomain:        "members.example.com",
		PDSOAuthClientURI: "tunnel.example.com",
		Port:              "9999",
		Directory:         dir,
	}.withDefaults()

	require.Equal(t, "members.example.com", c.HiveDomain)
	require.Equal(t, "https://tunnel.example.com", c.PDSOAuthClientURI)
	require.Equal(t, "9999", c.Port)
	require.Equal(t, dir, c.Directory)
}

func TestConfigValidateRequiresDomain(t *testing.T) {
	err := Config{DB: "sqlite://x"}.withDefaults().validate()
	require.ErrorContains(t, err, "domain")
}
```

Note the `PDSOAuthClientURI` behavior, copied from `main.go`: a bare host gets an `https://` prefix, and an empty value falls back to the domain.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pearsetup/ -run TestConfig -v`
Expected: FAIL to build — `undefined: Config`.

- [ ] **Step 3: Write the implementation**

Create `internal/pearsetup/config.go`:

```go
// Package pearsetup assembles a complete Pear instance from a plain
// configuration value. It exists so that both cmd/pear and tests can build the
// same fully wired server; cmd/pear owns flag parsing, this package owns
// everything after it.
package pearsetup

import (
	"errors"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"gocloud.dev/blob"

	"github.com/habitat-network/habitat/internal/fgastore"
)

// Config is everything New needs to build a Pear. Fields are plain values that
// the caller has already parsed; nothing here knows about CLI flags.
type Config struct {
	// Domain is the publicly reachable domain this server is served from.
	Domain string
	// HiveDomain is the domain member identities are minted under. Defaults to Domain.
	HiveDomain string
	// DB is the database DSN, in the form internal/db accepts.
	DB string
	// Port is the TCP port to listen on. Defaults to "8000".
	Port string
	// HTTPSCerts is a directory holding fullchain.pem and privkey.pem. Empty serves plain HTTP.
	HTTPSCerts string
	// Debug turns on request logging and stdout logs.
	Debug bool

	// OAuthServerSecret is the parsed 32-byte secret backing the OAuth server,
	// its cookie store, and the password login provider.
	OAuthServerSecret []byte
	// PDSCredEncryptKey is the parsed key encrypting stored PDS credentials.
	PDSCredEncryptKey []byte
	// OAuthClientSecret is the secret this server uses as an OAuth *client* of a user's PDS.
	OAuthClientSecret string
	// PDSOAuthClientURI is the client identity URI presented to a PDS. A bare
	// host is prefixed with https://; empty falls back to Domain.
	PDSOAuthClientURI string
	// SpaceSigningKey signs space commits for repo owners on external PDSes.
	SpaceSigningKey atcrypto.PrivateKey
	// AdminPasswordHash is the argon2id hash of the instance admin password.
	AdminPasswordHash string

	// GoogleClientID and GoogleClientSecret enable the Google login provider.
	// The provider is registered only when both are set.
	GoogleClientID     string
	GoogleClientSecret string

	// UIDevProxy proxies /ui/ to a dev server instead of serving embedded assets.
	UIDevProxy string
	// BuiltinApps are the client IDs allowed to use the JWT bearer grant.
	BuiltinApps []string
	// BlobBucket is the gocloud.dev blob URL for blob storage. Ignored when Bucket is set.
	BlobBucket string

	// Directory resolves atproto identities that hive does not host. Defaults
	// to identity.DefaultDirectory(), which resolves over the network — tests
	// must override it to stay offline.
	Directory identity.Directory
	// FGA overrides the relationship store. Defaults to a store chosen from the
	// DB dialect: Postgres shares the main DSN, SQLite gets a sibling file.
	FGA fgastore.Store
	// Bucket overrides blob storage. Defaults to opening BlobBucket.
	Bucket *blob.Bucket

	// DisableP2P skips the libp2p host and its catch-all route. Named
	// negatively so the zero value is production behavior.
	DisableP2P bool
	// DisableUI skips the /ui/ handler.
	DisableUI bool
}

func (c Config) withDefaults() Config {
	if c.HiveDomain == "" {
		c.HiveDomain = c.Domain
	}
	if c.PDSOAuthClientURI == "" {
		c.PDSOAuthClientURI = c.Domain
	}
	if !strings.HasPrefix(c.PDSOAuthClientURI, "https://") {
		c.PDSOAuthClientURI = "https://" + c.PDSOAuthClientURI
	}
	if c.Port == "" {
		c.Port = "8000"
	}
	if c.Directory == nil {
		c.Directory = identity.DefaultDirectory()
	}
	return c
}

func (c Config) validate() error {
	if c.Domain == "" {
		return errors.New("domain is required")
	}
	if c.DB == "" {
		return errors.New("db DSN is required")
	}
	if c.SpaceSigningKey == nil {
		return errors.New("space signing key is required")
	}
	if len(c.OAuthServerSecret) == 0 {
		return errors.New("oauth server secret is required")
	}
	if len(c.PDSCredEncryptKey) == 0 {
		return errors.New("pds cred encrypt key is required")
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/pearsetup/ -run TestConfig -v`
Expected: PASS, three tests.

- [ ] **Step 5: Commit**

```bash
git add internal/pearsetup/config.go internal/pearsetup/config_test.go
git commit -m "[pearsetup] Add flag-free Config with defaults"
```

---

### Task 3: New — component construction

This is a mechanical move of `cmd/pear/main.go` lines 111–380. Do not redesign anything; the only edits are substituting config fields for flag reads and threading the new seams.

**Files:**
- Create: `internal/pearsetup/pear.go`
- Reference: `cmd/pear/main.go:111-380` (leave it in place until Task 5)

**Interfaces:**
- Consumes: `Config`, `withDefaults`, `validate` from Task 2.
- Produces:
  - `type Pear struct` with exported fields `Config Config`, `DB *gorm.DB`, `FGA fgastore.Store`, `Hive hive.Hive`, `Directory identity.Directory` (the hive-wrapped one), `OrgStore org.Store`, `CliqueStore clique.Store`, `SpacesStore spaces.Store`, `PermStore perms.Store`, `NotifyStore notify.Store`, `Repo repo.Repo`, `Permissions permissions.Store`, `InstanceStore instance.Store`, `PDSCredStore pdscred.Store`, `OAuthServer *oauthserver.OAuthServer`, `Validator authn.RequestValidator`, `HostKey atcrypto.PrivateKey`, `Router *mux.Router`, plus the server values needed by `routes.go` (`orgServer`, `spacesServer`, `notifyServer`, `simplespaceServer`, `relationshipServer`, `cliqueServer`, `pearServer`, `p2pServer`, `idServer`, `instanceServer`, `pdsForwarding`, `oauthClient`, `passwordProvider`, `oauthGC`, `bucket`, `uiHandler`) — these may stay unexported since only this package routes them.
  - `func New(ctx context.Context, cfg Config) (*Pear, error)`
  - `func (p *Pear) Close() error`

- [ ] **Step 1: Write the failing test**

Create `internal/pearsetup/pear_test.go`:

```go
package pearsetup_test

import (
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/stretchr/testify/require"
	"gocloud.dev/blob/memblob"

	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/pearsetup"
)

// newConfig returns a Config that builds a complete instance without touching
// the network: SQLite on disk, in-memory FGA, in-memory blobs, a mock
// directory, and no libp2p host or UI assets.
func newConfig(t *testing.T) pearsetup.Config {
	t.Helper()

	key, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)

	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	secret := make([]byte, 32)
	_, err = rand.Read(secret)
	require.NoError(t, err)
	credKey := make([]byte, 32)
	_, err = rand.Read(credKey)
	require.NoError(t, err)

	return pearsetup.Config{
		Domain:            "pear.test",
		DB:                "sqlite://" + filepath.Join(t.TempDir(), "test.db"),
		OAuthServerSecret: secret,
		PDSCredEncryptKey: credKey,
		SpaceSigningKey:   key,
		AdminPasswordHash: "$argon2id$v=19$m=65536,t=1,p=2$c29tZXNhbHQ$0000000000000000000000000000000000000000000",
		Directory:         identity.NewMockDirectory(),
		FGA:               fga,
		Bucket:            memblob.OpenBucket(nil),
		DisableP2P:        true,
		DisableUI:         true,
	}
}

func TestNewWiresComponents(t *testing.T) {
	p, err := pearsetup.New(t.Context(), newConfig(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	require.NotNil(t, p.DB)
	require.NotNil(t, p.Hive)
	require.NotNil(t, p.OrgStore)
	require.NotNil(t, p.SpacesStore)
	require.NotNil(t, p.PermStore)
	require.NotNil(t, p.OAuthServer)
	require.NotNil(t, p.Validator)
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	cfg := newConfig(t)
	cfg.Domain = ""

	_, err := pearsetup.New(t.Context(), cfg)
	require.ErrorContains(t, err, "domain")
}

func TestHealthEndpoint(t *testing.T) {
	p, err := pearsetup.New(t.Context(), newConfig(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	req.Host = "pear.test"
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok", rec.Body.String())
}
```

`TestHealthEndpoint` will not pass until Task 4 adds `Handler`. That is expected and fine — it is the next task's guard. Keep it in this commit only if it compiles; if `Handler` does not exist yet, add this third test in Task 4 instead.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pearsetup/ -run TestNew -v`
Expected: FAIL to build — `undefined: pearsetup.New`.

- [ ] **Step 3: Write the implementation**

Create `internal/pearsetup/pear.go`. Copy `cmd/pear/main.go` lines 111–380 in their existing order, applying exactly these substitutions:

| In `main.go` | In `pear.go` |
|---|---|
| `cmd.String(fDomain)` etc. | the matching `cfg.` field |
| `startupCtx` | `ctx` |
| `db.New(cmd.String(fDB), db.WithMigrations(embedMigrations))` | `db.New(cfg.DB, db.WithMigrations(migrations.FS))` |
| `setupFGA(startupCtx, cmd)` | `cfg.FGA` when non-nil, else `setupFGA(ctx, cfg.DB)` moved into this file |
| `encrypt.ParseKey(cmd.String(...))` | already-parsed `cfg.OAuthServerSecret` / `cfg.PDSCredEncryptKey`; drop the parse |
| `setupInstanceAdminPassword(startupCtx, cmd)` | `cfg.AdminPasswordHash`; the function stays in `cmd/pear` |
| `atcrypto.ParsePrivateMultibase(cmd.String(fSpaceSigningKey))` | `cfg.SpaceSigningKey`; drop the parse |
| `identity.DefaultDirectory()` (all four uses) | `cfg.Directory` |
| `blob.OpenBucket(startupCtx, cmd.String(fBlobBucket))` | `cfg.Bucket` when non-nil, else `blob.OpenBucket(ctx, cfg.BlobBucket)` |
| `meter` from `otel.Meter("habitat-meter")` in `main` | `otel.Meter("habitat-meter")` here; it is a no-op meter when telemetry is unset |

Structure:

```go
func New(ctx context.Context, cfg Config) (*Pear, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	p := &Pear{Config: cfg}
	// ... the moved construction, assigning into p as it goes ...

	p.Router = mux.NewRouter()
	p.registerMiddleware()
	p.registerRoutes()
	return p, nil
}
```

Three deviations from a pure copy, each required:

1. **P2P is conditional.** Guard the `p2p.NewServer` call with `if !cfg.DisableP2P`. Its catch-all route is guarded in Task 4.
2. **UI is conditional.** Guard `webui.New(cfg.UIDevProxy)` with `if !cfg.DisableUI`.
3. **Ownership of closables.** `main.go` used `defer blobBucket.Close()`. `New` cannot defer, so store the bucket on `Pear` and add:

```go
// Close releases the resources New acquired: the blob bucket and, when
// enabled, the libp2p host. main defers this; tests register it with
// t.Cleanup.
func (p *Pear) Close() error {
	var errs []error
	if p.bucket != nil {
		if err := p.bucket.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close blob bucket: %w", err))
		}
	}
	if p.p2pServer != nil {
		if err := p.p2pServer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close p2p host: %w", err))
		}
	}
	return errors.Join(errs...)
}
```

Move `setupFGA` into `pear.go`, retargeted at a DSN rather than a `cli.Command`, keeping its comments verbatim:

```go
func setupFGA(ctx context.Context, dsn string) (fgastore.Store, error) {
	// Share the main Postgres database for FGA when one is configured; only fall
	// back to a separate SQLite file when the main store is SQLite.
	if db.ParseDialect(dsn) == db.Postgres {
		fga, err := fgastore.NewPostgres(ctx, dsn)
		if err != nil {
			return nil, fmt.Errorf("setup fga store with postgres: %w", err)
		}
		return fga, nil
	}
	// Use a separate SQLite file for FGA to avoid lock conflicts between
	// mattn/go-sqlite3 (used by GORM) and modernc.org/sqlite (used by OpenFGA).
	// Strip the "sqlite://" scheme (as internal/db does) so we hand OpenFGA a
	// plain filesystem path rather than a URI it parses as a host.
	fgaPath := strings.TrimPrefix(dsn, "sqlite://") + ".fga.db"
	fga, err := fgastore.NewSQLite(ctx, fgaPath)
	if err != nil {
		return nil, fmt.Errorf("setup fga sqlite store %q: %w", fgaPath, err)
	}
	return fga, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/pearsetup/ -run 'TestNew|TestConfig' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pearsetup/pear.go internal/pearsetup/pear_test.go
git commit -m "[pearsetup] Add New to build every component"
```

---

### Task 4: Routes and middleware

**Files:**
- Create: `internal/pearsetup/routes.go`
- Reference: `cmd/pear/main.go:166-212` (middleware) and `:380-560` (routes)

**Interfaces:**
- Consumes: the `Pear` fields from Task 3.
- Produces: `(*Pear).registerMiddleware()`, `(*Pear).registerRoutes()`, `func (p *Pear) Handler() http.Handler`.

- [ ] **Step 1: Write the failing test**

Add to `internal/pearsetup/pear_test.go` (or move it here if you deferred it in Task 3):

```go
func TestHealthEndpoint(t *testing.T) {
	p, err := pearsetup.New(t.Context(), newConfig(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	req.Host = "pear.test"
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok", rec.Body.String())
}

func TestUnroutedPathIs404WithP2PDisabled(t *testing.T) {
	p, err := pearsetup.New(t.Context(), newConfig(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	req := httptest.NewRequest(http.MethodGet, "/no/such/path", http.NoBody)
	req.Host = "pear.test"
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}
```

The second test pins down the consequence of `DisableP2P`: in production `mux.PathPrefix("/")` hands unmatched paths to libp2p, so with P2P off there must be no catch-all.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pearsetup/ -run 'TestHealth|TestUnrouted' -v`
Expected: FAIL to build — `p.Handler undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/pearsetup/routes.go` holding two methods, moved verbatim from `main.go` with `mux` renamed to `p.Router` and each server reference pointed at its `Pear` field.

`registerMiddleware` takes `main.go:166-212`: the otelmux middleware, the referer span attribute, the CORS handler with its exact allowed headers, the debug logging handler guarded by `p.Config.Debug`, the `/health` handler with its comment intact, and `forwarding.NewServiceProxy(...)` (which is registered as middleware at `main.go:355`).

Order matters and must be preserved: `/health` is registered before any auth-gated route so it stays reachable without credentials, and the service proxy middleware must be installed before route matching.

`registerRoutes` takes every `HandleFunc`, `Host`, `Headers`, `Handle`, and `PathPrefix` call from `main.go:380-560` in the same order, with these two guards:

```go
if !p.Config.DisableUI {
	p.Router.PathPrefix("/ui/").Handler(p.uiHandler)
}
if !p.Config.DisableP2P {
	p.Router.PathPrefix("/").HandlerFunc(p.p2pServer.HandleLibp2p)
}
```

Then:

```go
// Handler returns the fully routed HTTP handler. Tests serve requests through
// it directly rather than binding a port.
func (p *Pear) Handler() http.Handler {
	return p.Router
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pearsetup/ -v`
Expected: PASS, all tests.

- [ ] **Step 5: Commit**

```bash
git add internal/pearsetup/routes.go internal/pearsetup/pear_test.go
git commit -m "[pearsetup] Move route table out of main"
```

---

### Task 5: Run, and shrink main

**Files:**
- Create: `internal/pearsetup/run.go`
- Modify: `cmd/pear/main.go` (delete lines 111–560, rewrite `run`)

**Interfaces:**
- Consumes: `Pear` from Task 3, `Handler` from Task 4.
- Produces: `func (p *Pear) Run(ctx context.Context) error`.

- [ ] **Step 1: Write `run.go`**

Move `main.go:560-620` (the errgroup block) into:

```go
// Run serves until ctx is cancelled, then shuts the HTTP server and the OAuth
// garbage collector down. It does not call Close; the caller owns that, since
// a caller may want to inspect the instance after Run returns.
func (p *Pear) Run(ctx context.Context) error {
	s := &http.Server{
		Handler:           p.Handler(),
		Addr:              fmt.Sprintf(":%s", p.Config.Port),
		ReadHeaderTimeout: 30 * time.Second,
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return p.oauthGC.Run(egCtx)
	})
	eg.Go(func() error {
		slog.InfoContext(egCtx, "starting server", "port", p.Config.Port)
		if p.Config.HTTPSCerts == "" {
			return s.ListenAndServe()
		}
		return s.ListenAndServeTLS(
			filepath.Join(p.Config.HTTPSCerts, "fullchain.pem"),
			filepath.Join(p.Config.HTTPSCerts, "privkey.pem"),
		)
	})
	eg.Go(func() error {
		<-egCtx.Done()
		slog.InfoContext(egCtx, "shutting down server")
		if err := s.Shutdown(context.Background()); err != nil {
			slog.ErrorContext(egCtx, "error shutting down http server", "err", err)
		}
		return nil
	})

	return eg.Wait()
}
```

The libp2p shutdown that lived in the third goroutine moves to `Close`, where Task 3 already put it. `ReadHeaderTimeout` is new — `golangci-lint` will otherwise flag the bare `http.Server`; if the current config does not flag it, leave the field off rather than changing behavior silently.

- [ ] **Step 2: Rewrite `cmd/pear/main.go`**

`run` keeps: telemetry setup, the `habitat.running` gauge, the signal context, the flag-to-config mapping, `slog.SetDefault(log.New(log.WithStdout(cmd.Bool(fDebug))))`, and the `errors.Is(err, context.Canceled)` handling. It keeps the helpers `setupInstanceAdminPassword` and `getFlags`. Everything else goes.

The mapping, which is now the whole job:

```go
oauthSecret, err := encrypt.ParseKey(cmd.String(fOauthServerSecret))
if err != nil {
	return fmt.Errorf("parse oauth server secret: %w", err)
}
credKey, err := encrypt.ParseKey(cmd.String(fPdsCredEncryptKey))
if err != nil {
	return fmt.Errorf("load PDS encryption key: %w", err)
}
hostKey, err := atcrypto.ParsePrivateMultibase(cmd.String(fSpaceSigningKey))
if err != nil {
	return fmt.Errorf("parse space-host signing key: %w", err)
}
passwordHash, err := setupInstanceAdminPassword(startupCtx, cmd)
if err != nil {
	return fmt.Errorf("setup instance admin password: %w", err)
}

cfg := pearsetup.Config{
	Domain:             cmd.String(fDomain),
	HiveDomain:         cmd.String(fHiveDomain),
	DB:                 cmd.String(fDB),
	Port:               cmd.String(fPort),
	HTTPSCerts:         cmd.String(fHttpsCerts),
	Debug:              cmd.Bool(fDebug),
	OAuthServerSecret:  oauthSecret,
	PDSCredEncryptKey:  credKey,
	OAuthClientSecret:  cmd.String(fOauthClientSecret),
	PDSOAuthClientURI:  cmd.String(fPdsOauthClientUri),
	SpaceSigningKey:    hostKey,
	AdminPasswordHash:  passwordHash,
	GoogleClientID:     cmd.String(fGoogleClientID),
	GoogleClientSecret: cmd.String(fGoogleClientSecret),
	UIDevProxy:         cmd.String(fUiDevProxy),
	BuiltinApps:        cmd.StringSlice(fBuiltinApps),
	BlobBucket:         cmd.String(fBlobBucket),
}

p, err := pearsetup.New(startupCtx, cfg)
if err != nil {
	return fmt.Errorf("setup pear: %w", err)
}
defer func() { _ = p.Close() }()

startupSpan.End()
slog.SetDefault(log.New(log.WithStdout(cmd.Bool(fDebug))))

err = p.Run(notifyCtx)
if !errors.Is(err, context.Canceled) {
	slog.ErrorContext(startupCtx, "server shut down returned an error", "err", err)
}
return err
```

Then prune the import block down to what remains. `goimports` will not remove them for you; delete by hand and let the compiler confirm.

- [ ] **Step 3: Verify main still boots on both dialects**

Run: `go test ./cmd/pear/ -v`
Expected: PASS — `TestStartup_Sqlite` and `TestStartup_Postgres`. The Postgres case needs Docker for testcontainers.

- [ ] **Step 4: Verify the whole build and lint**

Run: `go build ./... && golangci-lint run`
Expected: no output from either.

- [ ] **Step 5: Confirm main actually shrank**

Run: `wc -l cmd/pear/main.go`
Expected: roughly 120 lines, down from 663. If it is much larger, something that belongs in `pearsetup` is still in `main`.

- [ ] **Step 6: Commit**

```bash
git add cmd/pear/main.go internal/pearsetup/run.go
git commit -m "[pearsetup] Reduce cmd/pear to flag mapping"
```

---

### Task 6: Mint real access tokens

**Files:**
- Modify: `internal/oauthserver/oauth_server.go` (the `OAuthServer` struct, `NewOAuthServer`, and a new method)
- Test: `internal/oauthserver/oauth_server_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func (o *OAuthServer) MintAccessToken(ctx context.Context, did syntax.DID, scopes []string, ttl time.Duration) (string, error)`.

Background the implementer needs: Habitat's access tokens are stateless ES256 JWTs. `NewOAuthServer` derives an ECDSA P-256 key from `GlobalSecret` and builds `compose.NewOAuth2JWTStrategy`, and validation runs through `OAuth2StatelessJWTIntrospectionFactory`. `StatelessJWTValidator.IntrospectToken` verifies the signature and claims and matches scopes; it never consults storage and never looks up the client. So a token minted from the strategy alone validates, provided the session carries the `typ: oauth+JWT` header that `CanHandle` keys on.

- [ ] **Step 1: Write the failing test**

Add to `internal/oauthserver/oauth_server_test.go`:

```go
func TestMintAccessTokenValidates(t *testing.T) {
	srv := newTestServer(t) // reuse this file's existing constructor
	did := syntax.DID("did:web:alice.pear.test")

	token, err := srv.MintAccessToken(t.Context(), did, []string{"org:*"}, time.Hour)
	require.NoError(t, err)

	credInfo, ok, err := srv.ValidateRaw(t.Context(), token, "org:com.example.thing")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, did, credInfo.Subject)
}

func TestMintAccessTokenIsRecognizedAsOAuth(t *testing.T) {
	srv := newTestServer(t)

	token, err := srv.MintAccessToken(t.Context(), syntax.DID("did:web:alice.pear.test"), nil, time.Hour)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/whatever", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	require.True(t, srv.CanHandle(req), "minted token must carry the oauth+JWT typ header")
}

func TestMintAccessTokenExpires(t *testing.T) {
	srv := newTestServer(t)

	token, err := srv.MintAccessToken(t.Context(), syntax.DID("did:web:alice.pear.test"), nil, -time.Minute)
	require.NoError(t, err)

	_, ok, err := srv.ValidateRaw(t.Context(), token)
	require.Error(t, err)
	require.False(t, ok)
}

func TestMintAccessTokenRejectsInsufficientScope(t *testing.T) {
	srv := newTestServer(t)

	token, err := srv.MintAccessToken(
		t.Context(),
		syntax.DID("did:web:alice.pear.test"),
		[]string{"org:com.example.allowed"},
		time.Hour,
	)
	require.NoError(t, err)

	_, ok, err := srv.ValidateRaw(t.Context(), token, "org:com.example.denied")
	require.Error(t, err)
	require.False(t, ok)
}
```

Check what the existing test file names its constructor before writing `newTestServer(t)` — reuse whatever is already there rather than adding a second one.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/oauthserver/ -run TestMintAccessToken -v`
Expected: FAIL to build — `srv.MintAccessToken undefined`.

- [ ] **Step 3: Write the implementation**

Add a field to `OAuthServer`:

```go
// strategy mints and validates access tokens. Retained so MintAccessToken can
// issue tokens through the same path the token endpoint uses, rather than
// re-deriving the signing key and duplicating the claim shape.
strategy *oauth2.DefaultJWTStrategy
```

Assign `strategy: strategy` in the returned struct literal in `NewOAuthServer`.

Add the method:

```go
// MintAccessToken issues an access token for did without running the
// authorization code flow. It is the seam test harnesses use to authenticate as
// an arbitrary DID: because access tokens are stateless JWTs, a token minted
// here is indistinguishable from one issued at the token endpoint, so callers
// exercise the real validation path rather than a stub.
//
// scopes become the token's granted scopes; nil grants none. ttl sets expiry and
// may be negative to mint an already-expired token.
func (o *OAuthServer) MintAccessToken(
	ctx context.Context,
	did syntax.DID,
	scopes []string,
	ttl time.Duration,
) (string, error) {
	expiresAt := time.Now().Add(ttl)
	req := fosite.NewAccessRequest(&session{
		Subject:              did.String(),
		ClientID:             mintedTokenClientID,
		Scopes:               scopes,
		AccessTokenExpiresAt: expiresAt,
	})
	req.Client = &fosite.DefaultClient{ID: mintedTokenClientID}
	req.GrantedScope = scopes

	token, _, err := o.strategy.GenerateAccessToken(ctx, req)
	if err != nil {
		return "", fmt.Errorf("generate access token for %s: %w", did, err)
	}
	return token, nil
}
```

And the constant, near the top of the file:

```go
// mintedTokenClientID is the client_id recorded in tokens from
// MintAccessToken. Stateless introspection never resolves the client, so this
// only has to be identifiable in logs.
const mintedTokenClientID = "habitat-minted"
```

Imports to add: `"time"`, `"github.com/ory/fosite"`, and `"github.com/ory/fosite/handler/oauth2"` — check the existing import block first, several of these are likely already present.

If `fosite.NewAccessRequest` does not exist in v0.49.0 with that signature, build the request directly instead; the requirements are only that `GetSession()` returns the `*session` and `GetGrantedScopes()` returns `scopes`:

```go
req := &fosite.Request{
	Client:       &fosite.DefaultClient{ID: mintedTokenClientID},
	GrantedScope: scopes,
	Session:      &session{...},
	RequestedAt:  time.Now(),
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/oauthserver/ -v`
Expected: PASS, including the pre-existing tests.

- [ ] **Step 5: Commit**

```bash
git add internal/oauthserver/oauth_server.go internal/oauthserver/oauth_server_test.go
git commit -m "[oauthserver] Add MintAccessToken for test harnesses"
```

---

### Task 7: The test harness — standing up an instance

**Files:**
- Create: `internal/pearsetup/testutil/testutil.go`
- Test: `internal/pearsetup/testutil/testutil_test.go`

**Interfaces:**
- Consumes: `pearsetup.Config`, `pearsetup.New`, `(*Pear).Handler`, `(*Pear).Close`.
- Produces: `type TestPear struct { *pearsetup.Pear; T *testing.T }`, `func New(t *testing.T, opts ...Option) *TestPear`, `type Option func(*pearsetup.Config)`, `func WithDirectory(identity.Directory) Option`, `func WithFGA(fgastore.Store) Option`, `func WithConfig(func(*pearsetup.Config)) Option`, and the exported constant `Domain = "pear.test"`.

- [ ] **Step 1: Write the failing test**

Create `internal/pearsetup/testutil/testutil_test.go`:

```go
package testutil_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/pearsetup/testutil"
)

func TestNewServesHealth(t *testing.T) {
	p := testutil.New(t)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	resp := p.Do(nil, req)

	require.Equal(t, http.StatusOK, resp.StatusCode)
}
```

This depends on `Do` from Task 9. If you are executing tasks strictly in order, write this instead for Task 7 and replace it in Task 9:

```go
func TestNewBuildsInstance(t *testing.T) {
	p := testutil.New(t)

	require.NotNil(t, p.OrgStore)
	require.NotNil(t, p.OAuthServer)
	require.Equal(t, testutil.Domain, p.Config.Domain)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pearsetup/testutil/ -v`
Expected: FAIL to build — no such package.

- [ ] **Step 3: Write the implementation**

Create `internal/pearsetup/testutil/testutil.go`:

```go
// Package testutil stands up a complete, real Pear instance for tests: real
// stores, real routes, real authentication. Server tests use it instead of
// hand-assembling components behind a stub validator, so the authorization
// checks their handlers declare are actually exercised.
package testutil

import (
	"crypto/rand"
	"path/filepath"
	"testing"

	"github.com/alexedwards/argon2id"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/stretchr/testify/require"
	"gocloud.dev/blob/memblob"

	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/pearsetup"
)

// Domain is the domain every harness instance is served from. Tests that build
// URIs or set a Host header should use it rather than hardcoding a string.
const Domain = "pear.test"

// TestPear is a running Pear instance. It embeds *pearsetup.Pear, so every
// store and server is reachable for seeding fixtures directly, while the
// request helpers drive the same routes production serves.
type TestPear struct {
	*pearsetup.Pear

	T *testing.T
}

// Option mutates the config before the instance is built.
type Option func(*pearsetup.Config)

// WithDirectory replaces the identity directory used for DIDs hive does not
// host. The default resolves nothing, which keeps tests offline; supply a
// populated identity.MockDirectory when a test needs an external DID.
func WithDirectory(dir identity.Directory) Option {
	return func(c *pearsetup.Config) { c.Directory = dir }
}

// WithFGA replaces the relationship store.
func WithFGA(store fgastore.Store) Option {
	return func(c *pearsetup.Config) { c.FGA = store }
}

// WithConfig is the escape hatch for settings without a dedicated option.
func WithConfig(fn func(*pearsetup.Config)) Option {
	return fn
}

// New builds a Pear backed by a temporary SQLite database, an in-memory
// relationship store, and in-memory blob storage, with the libp2p host and the
// UI handler switched off. Everything is torn down when the test ends.
func New(t *testing.T, opts ...Option) *TestPear {
	t.Helper()

	hostKey, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)

	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	passwordHash, err := argon2id.CreateHash("admin", argon2id.DefaultParams)
	require.NoError(t, err)

	cfg := pearsetup.Config{
		Domain:            Domain,
		DB:                "sqlite://" + filepath.Join(t.TempDir(), "test.db"),
		OAuthServerSecret: randomKey(t),
		PDSCredEncryptKey: randomKey(t),
		OAuthClientSecret: "test-client-secret",
		SpaceSigningKey:   hostKey,
		AdminPasswordHash: passwordHash,
		Directory:         identity.NewMockDirectory(),
		FGA:               fga,
		Bucket:            memblob.OpenBucket(nil),
		DisableP2P:        true,
		DisableUI:         true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	p, err := pearsetup.New(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	return &TestPear{Pear: p, T: t}
}

// randomKey returns 32 random bytes. The OAuth server parses its secret as a
// P-256 scalar, for which any 32 random bytes are overwhelmingly likely to be
// valid.
func randomKey(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return b
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/pearsetup/testutil/ -v`
Expected: PASS.

- [ ] **Step 5: Check how long it takes**

Run: `go test ./internal/pearsetup/testutil/ -count=5`
Expected: comfortably under a second per instance. If standing up an instance takes seconds, find out what is doing I/O or network work before building more on top of it — most likely a directory lookup escaping to the network, which means a `cfg.Directory` substitution was missed in Task 3.

- [ ] **Step 6: Commit**

```bash
git add internal/pearsetup/testutil/
git commit -m "[pearsetup] Add test harness that builds a real instance"
```

---

### Task 8: Actors and real tokens

**Files:**
- Create: `internal/pearsetup/testutil/actors.go`
- Test: `internal/pearsetup/testutil/actors_test.go`

**Interfaces:**
- Consumes: `TestPear` from Task 7, `MintAccessToken` from Task 6.
- Produces:
  - `type Actor struct { DID syntax.DID; Org syntax.DID; Token string }`
  - `func (p *TestPear) NewOrg(name string) *Actor`
  - `func (p *TestPear) NewMember(org *Actor, handle string) *Actor`
  - `func (p *TestPear) Anonymous() *Actor`
  - `func (p *TestPear) ActorWithScopes(did, org syntax.DID, scopes ...string) *Actor`

Background: `org.Store.CreateOrg(ctx, name, adminHandle, adminPassword, method, loginID, handleSubdomain, contactEmail)` returns `(orgIdentity, adminIdentity, error)`. Members are created in two steps: an admin calls `IssueIdentityToken(ctx, orgDID, caller, reusable, expiresAt)`, then `CreateNewMemberIdentity(ctx, orgDID, token, internalHandle, password, loginID)` redeems it. Use those rather than inserting rows.

- [ ] **Step 1: Write the failing test**

Create `internal/pearsetup/testutil/actors_test.go`:

```go
package testutil_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/pearsetup/testutil"
)

func TestNewOrgCreatesAdmin(t *testing.T) {
	p := testutil.New(t)

	admin := p.NewOrg("acme")

	require.NotEmpty(t, admin.DID)
	require.NotEmpty(t, admin.Org)
	require.NotEmpty(t, admin.Token)

	org, err := p.OrgStore.GetOrg(t.Context(), admin.Org)
	require.NoError(t, err)
	isAdmin, err := org.IsAdmin(t.Context(), admin.DID)
	require.NoError(t, err)
	require.True(t, isAdmin, "NewOrg's actor must be an admin of the org it created")
}

func TestNewMemberJoinsOrg(t *testing.T) {
	p := testutil.New(t)
	admin := p.NewOrg("acme")

	member := p.NewMember(admin, "alice")

	require.Equal(t, admin.Org, member.Org)
	require.NotEqual(t, admin.DID, member.DID)

	org, err := p.OrgStore.GetOrg(t.Context(), admin.Org)
	require.NoError(t, err)
	isMember, err := org.IsMember(t.Context(), member.DID)
	require.NoError(t, err)
	require.True(t, isMember)

	isAdmin, err := org.IsAdmin(t.Context(), member.DID)
	require.NoError(t, err)
	require.False(t, isAdmin, "a plain member must not be an admin")
}

func TestActorTokenAuthenticates(t *testing.T) {
	p := testutil.New(t)
	admin := p.NewOrg("acme")

	credInfo, ok, err := p.OAuthServer.ValidateRaw(t.Context(), admin.Token)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, admin.DID, credInfo.Subject)
	require.Equal(t, admin.Org, credInfo.Org.DID(), "the real validator resolves the org from membership")
}

func TestAnonymousHasNoToken(t *testing.T) {
	p := testutil.New(t)

	require.Empty(t, p.Anonymous().Token)
}
```

`TestActorTokenAuthenticates` is the load-bearing one: it proves the org comes back from a real membership lookup, not from a stub that was handed the answer.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pearsetup/testutil/ -run 'TestNewOrg|TestNewMember|TestActor|TestAnonymous' -v`
Expected: FAIL to build — `p.NewOrg undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/pearsetup/testutil/actors.go`:

```go
package testutil

import (
	"fmt"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

// defaultScopes is what an actor's token grants unless a test asks for
// something narrower. "org:*" satisfies every org-scoped requirement, so tests
// that are not about scope enforcement do not have to think about it; tests
// that are should use ActorWithScopes.
var defaultScopes = []string{"org:*"}

// tokenTTL is long enough that no test can outlive its own token.
const tokenTTL = time.Hour

// Actor is an authenticated identity. Its Token is a genuine access token,
// minted through the OAuth server's own signing strategy, so requests carrying
// it run the same validation a production request runs.
type Actor struct {
	DID   syntax.DID
	Org   syntax.DID
	Token string
}

// NewOrg creates an org named name and returns its bootstrap admin.
func (p *TestPear) NewOrg(name string) *Actor {
	p.T.Helper()

	orgIdent, adminIdent, err := p.OrgStore.CreateOrg(
		p.T.Context(),
		name,
		"admin",
		"password",
		"password",
		fmt.Sprintf("%s-admin", name),
		name,
		fmt.Sprintf("admin@%s.test", name),
	)
	require.NoError(p.T, err)

	return p.ActorWithScopes(adminIdent.DID, orgIdent.DID, defaultScopes...)
}

// NewMember mints a member identity in org's organization and returns it. org
// must be an admin actor, since issuing the invite token requires one.
func (p *TestPear) NewMember(org *Actor, handle string) *Actor {
	p.T.Helper()

	token, err := p.OrgStore.IssueIdentityToken(
		p.T.Context(),
		org.Org,
		org.DID,
		false,
		time.Now().Add(time.Hour),
	)
	require.NoError(p.T, err)

	ident, err := p.OrgStore.CreateNewMemberIdentity(
		p.T.Context(),
		org.Org,
		token,
		handle,
		"password",
		fmt.Sprintf("%s@%s", handle, org.Org),
	)
	require.NoError(p.T, err)

	return p.ActorWithScopes(ident.DID, org.Org, defaultScopes...)
}

// Anonymous returns an actor with no credentials. Requests made as this actor
// carry no Authorization header.
func (p *TestPear) Anonymous() *Actor {
	return &Actor{}
}

// ActorWithScopes mints a token for an existing DID with exactly the scopes
// given. Use it to test that a handler rejects an under-scoped token.
func (p *TestPear) ActorWithScopes(did, org syntax.DID, scopes ...string) *Actor {
	p.T.Helper()

	token, err := p.OAuthServer.MintAccessToken(p.T.Context(), did, scopes, tokenTTL)
	require.NoError(p.T, err)

	return &Actor{DID: did, Org: org, Token: token}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pearsetup/testutil/ -v`
Expected: PASS.

If `TestActorTokenAuthenticates` fails on the org assertion, check `org.Store.GetOrgForDID`: it falls back to the "everyone" org rather than erroring, so a DID that was never actually added as a member will authenticate but come back with the wrong org. That failure means member creation did not take, not that the assertion is too strict.

- [ ] **Step 5: Commit**

```bash
git add internal/pearsetup/testutil/actors.go internal/pearsetup/testutil/actors_test.go
git commit -m "[pearsetup] Add test actors with real OAuth tokens"
```

---

### Task 9: Request helpers

**Files:**
- Create: `internal/pearsetup/testutil/request.go`
- Test: `internal/pearsetup/testutil/request_test.go`

**Interfaces:**
- Consumes: `TestPear`, `Actor`.
- Produces:
  - `func (p *TestPear) Do(a *Actor, r *http.Request) *http.Response`
  - `func (p *TestPear) Query(a *Actor, nsid string, params url.Values, out any) *http.Response`
  - `func (p *TestPear) Procedure(a *Actor, nsid string, body, out any) *http.Response`

- [ ] **Step 1: Write the failing test**

Create `internal/pearsetup/testutil/request_test.go`:

```go
package testutil_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/pearsetup/testutil"
)

func TestQueryAuthenticatesAsActor(t *testing.T) {
	p := testutil.New(t)
	admin := p.NewOrg("acme")

	var out habitat.NetworkHabitatOrgGetMembersOutput
	resp := p.Query(admin, "network.habitat.org.getMembers", url.Values{}, &out)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, out.Members, admin.DID.String())
}

func TestQueryRejectsAnonymous(t *testing.T) {
	p := testutil.New(t)
	p.NewOrg("acme")

	resp := p.Query(p.Anonymous(), "network.habitat.org.getMembers", url.Values{}, nil)

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"the real validator must reject an unauthenticated request")
}
```

`TestQueryRejectsAnonymous` is the test the old stub validator could never have written: `NewSuccessValidatorWithOrg` returns success unconditionally.

Check the actual output type and field name of `network.habitat.org.getMembers` in `api/habitat/` before writing the assertion — do not guess. If its shape makes this awkward, pick any other already-routed query endpoint; the point is that one authenticated call succeeds and one anonymous call is rejected.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pearsetup/testutil/ -run TestQuery -v`
Expected: FAIL to build — `p.Query undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/pearsetup/testutil/request.go`:

```go
package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/stretchr/testify/require"
)

// Do serves r through the full router — middleware, service proxy, and the
// real request validator — and returns the response. A nil or credential-less
// actor sends no Authorization header. The response body is closed when the
// test ends, so callers may read it without closing it themselves.
//
// Requests are served in-process with httptest rather than over a listener, so
// no port is bound and no goroutine outlives the test.
func (p *TestPear) Do(a *Actor, r *http.Request) *http.Response {
	p.T.Helper()

	if r.Host == "" {
		r.Host = Domain
	}
	if a != nil && a.Token != "" {
		r.Header.Set("Authorization", "Bearer "+a.Token)
	}

	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, r)

	resp := rec.Result()
	p.T.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// Query issues an XRPC query (GET) against nsid. When out is non-nil and the
// response is 200, the body is decoded into it.
func (p *TestPear) Query(a *Actor, nsid string, params url.Values, out any) *http.Response {
	p.T.Helper()

	target := "/xrpc/" + nsid
	if encoded := params.Encode(); encoded != "" {
		target += "?" + encoded
	}
	req := httptest.NewRequest(http.MethodGet, target, http.NoBody)

	resp := p.Do(a, req)
	p.decode(resp, out)
	return resp
}

// Procedure issues an XRPC procedure (POST) against nsid, JSON-encoding body
// when it is non-nil. When out is non-nil and the response is 200, the body is
// decoded into it.
func (p *TestPear) Procedure(a *Actor, nsid string, body, out any) *http.Response {
	p.T.Helper()

	var payload io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(p.T, err)
		payload = bytes.NewReader(encoded)
	}

	req := httptest.NewRequest(http.MethodPost, "/xrpc/"+nsid, payload)
	req.Header.Set("Content-Type", "application/json")

	resp := p.Do(a, req)
	p.decode(resp, out)
	return resp
}

// decode reads a successful response into out. Failures are left to the caller
// to assert on, so a test that expects an error status still sees its status
// rather than a decode failure.
func (p *TestPear) decode(resp *http.Response, out any) {
	p.T.Helper()

	if out == nil || resp.StatusCode != http.StatusOK {
		return
	}
	require.NoError(p.T, json.NewDecoder(resp.Body).Decode(out))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pearsetup/testutil/ -v`
Expected: PASS.

- [ ] **Step 5: Lint**

Run: `golangci-lint run ./internal/pearsetup/...`
Expected: no output. `bodyclose` is satisfied by the `t.Cleanup` close in `Do`; if it still complains, close explicitly at each call site instead.

- [ ] **Step 6: Commit**

```bash
git add internal/pearsetup/testutil/request.go internal/pearsetup/testutil/request_test.go
git commit -m "[pearsetup] Add XRPC request helpers to the harness"
```

---

## Conversion tasks: the shared recipe

Tasks 10 through 14 all do the same thing to a different package. The recipe:

1. Replace the file's `newTestServer`-style constructor with `testutil.New(t)` plus the actors the test needs.
2. Replace direct handler calls (`s.SetUserRelation(rec, req)`) with routed calls (`p.Procedure(actor, "network.habitat.relationship.setUserRelation", body, &out)`).
3. Replace store construction with the harness's stores (`p.SpacesStore`, `p.PermStore`, …) for fixture seeding. Seeding through stores stays fine and is often clearer than seeding through endpoints; what must not stay is a stubbed validator.
4. Replace hardcoded DIDs (`did:plc:alice`) with actor DIDs, since authorization now depends on real membership.
5. Delete the `authntest` import.

**The rule for failures:** a test that fails after conversion has found something. Read the handler's `authn.WithSpace(...)` or `WithMethods(...)` options and work out whether the test was asserting something the handler never actually permitted. Fix the test if the test was wrong. If the handler is wrong, stop, report it, and do not bundle a behavior fix into the conversion commit. Do not reintroduce `authntest` to make a conversion pass — that reinstates exactly the gap this work removes.

Order is easiest-first so harness gaps surface while the blast radius is small.

---

### Task 10: Convert internal/notify

71 lines, one server, the smallest possible proof the harness works on real test code.

**Files:**
- Modify: `internal/notify/server_test.go`

**Interfaces:**
- Consumes: `testutil.New`, `NewOrg`, `Procedure`.
- Produces: nothing.

- [ ] **Step 1: Read the current test and note what it asserts**

Run: `cat internal/notify/server_test.go`

Write down, for each test, what it proves. The converted tests must prove the same things — conversion is not licence to weaken an assertion.

- [ ] **Step 2: Convert, following the shared recipe**

The test currently lives in `package notify`. The harness lives outside it, so move the file to `package notify_test` to avoid an import cycle, and reference exported symbols only. If the test needs an unexported symbol, that is a signal it is a unit test rather than a server test — leave that portion in `package notify` in a separate file and convert only the routed part.

Register-notify is routed at `/xrpc/network.habitat.space.registerNotify`.

- [ ] **Step 3: Run the tests**

Run: `go test ./internal/notify/ -v`
Expected: PASS. If a test now fails, apply the failure rule above.

- [ ] **Step 4: Confirm the stub is gone**

Run: `grep -rn "authn/testutil" internal/notify/`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/
git commit -m "[notify] Convert server tests to the pear harness"
```

---

### Task 11: Convert internal/relationship

469 lines. Its `newTestServer(t, caller)` builds an in-memory FGA, a DB, a spaces store, and a perms store, then authenticates with `authntest.NewSuccessValidatorWithOrg(caller, caller)` — note it passes the caller as its own org, which the real validator will not do.

**Files:**
- Modify: `internal/relationship/server_test.go`

**Interfaces:**
- Consumes: `testutil.New`, `NewOrg`, `NewMember`, `Query`, `Procedure`.
- Produces: nothing.

- [ ] **Step 1: Replace the constructor**

The existing helper returns `(*Server, perms.Store, spaces.Store)`. Its replacement:

```go
// newTestPear returns a harness plus the org admin, who owns the spaces these
// tests create. The admin is the caller for cases that need the manager role.
func newTestPear(t *testing.T) (*testutil.TestPear, *testutil.Actor) {
	t.Helper()
	p := testutil.New(t)
	return p, p.NewOrg("acme")
}
```

`p.PermStore` and `p.SpacesStore` replace the returned stores.

- [ ] **Step 2: Replace the package-level DIDs**

`testOrg`, `alice`, and `bob` are currently fixed `did:plc:` strings. `testOrg` becomes the admin actor's `Org`; `alice` and `bob` become `p.NewMember(admin, "alice")` and `p.NewMember(admin, "bob")` created inside each test. They can no longer be package-level `var`s, because they now depend on a running instance.

Use a `sed` script for the mechanical part of this rename, then fix the residue by hand:

```bash
sed -i '' 's/\balice\b/alice.DID/g; s/\bbob\b/bob.DID/g' internal/relationship/server_test.go
```

Then repair the declaration sites the script also rewrote. Compile after each pass.

- [ ] **Step 3: Route the handler calls**

Each `httptest.NewRequest` plus direct handler call becomes a `p.Query` or `p.Procedure` against the NSID that `routes.go` registers for it: `setUserRelation`, `setSpaceRelation`, `deleteRelation`, `listRelations`, `checkUserRelation`, `checkSpaceRelation`, `resolveRelations`, `listRelatedSpaces`, all under `network.habitat.relationship.`.

- [ ] **Step 4: Add the space-role rejection test**

The spec calls for proving that an actor lacking a space role is actually
rejected — the case `authntest.NewSuccessValidatorWithOrg` could never fail,
because its `Request(options ...utils.Opt[authn.EndpointOptions])` discards the
`authn.WithSpace(...)` option the handler passes. Add:

```go
func TestSetUserRelationRejectsActorWithoutSpaceRole(t *testing.T) {
	p, admin := newTestPear(t)
	outsider := p.NewMember(admin, "outsider")
	target := p.NewMember(admin, "target")

	space, err := p.SpacesStore.CreateSpace(
		t.Context(), admin.Org, admin.Org, docsType, habitat_syntax.SpaceKey("doc"),
	)
	require.NoError(t, err)

	resp := p.Procedure(outsider, "network.habitat.relationship.setUserRelation",
		map[string]string{
			"subject":  target.DID.String(),
			"relation": "reader",
			"space":    space.String(),
		}, nil)

	require.NotEqual(t, http.StatusOK, resp.StatusCode,
		"a member with no role on the space must not be able to grant one")
}
```

Confirm the request field names against the lexicon in `lexicons/` before
running it, and confirm `CreateSpace`'s parameter order against
`internal/spaces/store.go` rather than trusting this snippet.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/relationship/ -v`
Expected: PASS, with the same number of tests as before, plus the new one.

Watch specifically for space-role failures. Commit 925109f1, "[Relationship] Fix passing in space role to validator", touched exactly this area, and the stub validator cannot have been covering it.

- [ ] **Step 6: Confirm the stub is gone and commit**

```bash
grep -rn "authn/testutil" internal/relationship/   # expect no output
git add internal/relationship/
git commit -m "[relationship] Convert server tests to the pear harness"
```

---

### Task 12: Convert internal/simplespace

416 lines.

**Files:**
- Modify: `internal/simplespace/server_test.go`

**Interfaces:**
- Consumes: `testutil.New`, `NewOrg`, `NewMember`, `Query`, `Procedure`.
- Produces: nothing.

- [ ] **Step 1: Convert, following the shared recipe**

Endpoints: `network.habitat.simplespace.createSpace`, `.addMember`, `.removeMember`, `.listMembers`, `.deleteSpace`.

Membership tests here are the ones most likely to change meaning under a real validator: adding a member to a space now requires the caller to hold the role the handler demands, and the members being added must be real DIDs the instance can resolve. Create them with `p.NewMember`.

- [ ] **Step 2: Run the tests**

Run: `go test ./internal/simplespace/ -v`
Expected: PASS.

- [ ] **Step 3: Confirm the stub is gone and commit**

```bash
grep -rn "authn/testutil" internal/simplespace/   # expect no output
git add internal/simplespace/
git commit -m "[simplespace] Convert server tests to the pear harness"
```

---

### Task 13: Convert internal/org/server

561 lines. This one exercises admin and membership rules directly, so it gains the most from real org state.

**Files:**
- Modify: `internal/org/server/server_test.go`

**Interfaces:**
- Consumes: `testutil.New`, `NewOrg`, `NewMember`, `ActorWithScopes`, `Query`, `Procedure`.
- Produces: nothing.

- [ ] **Step 1: Convert, following the shared recipe**

Endpoints, all under `network.habitat.org.`: `getMetadata`, `getAdmins`, `getMembers`, `addAdmin`, `removeAdmin`, `removeMembers`, `downgradeAdmin`, `issueInviteToken`, `mintMemberIdentity`, `create`.

- [ ] **Step 2: Add the negative cases the stub made impossible**

While converting, add these — they are the payoff for the whole exercise:

```go
func TestAddAdminRejectsNonAdmin(t *testing.T) {
	p := testutil.New(t)
	admin := p.NewOrg("acme")
	member := p.NewMember(admin, "alice")
	target := p.NewMember(admin, "bob")

	resp := p.Procedure(member, "network.habitat.org.addAdmin",
		map[string]string{"did": target.DID.String()}, nil)

	require.NotEqual(t, http.StatusOK, resp.StatusCode,
		"a plain member must not be able to promote an admin")
}

func TestGetMembersRejectsAnonymous(t *testing.T) {
	p := testutil.New(t)
	p.NewOrg("acme")

	resp := p.Query(p.Anonymous(), "network.habitat.org.getMembers", url.Values{}, nil)

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
```

Check the request body field names against `api/habitat/` before writing them; do not guess at `{"did": ...}`.

- [ ] **Step 3: Run the tests**

Run: `go test ./internal/org/... -v`
Expected: PASS.

- [ ] **Step 4: Confirm the stub is gone and commit**

```bash
grep -rn "authn/testutil" internal/org/   # expect no output
git add internal/org/
git commit -m "[org] Convert server tests to the pear harness"
```

---

### Task 14: Convert internal/spaces/server

819 lines, the largest. Left for last because it covers blobs, commits, and CAR files alongside authorization, so it is the most likely to need harness features the earlier tasks did not.

**Files:**
- Modify: `internal/spaces/server/server_test.go`

**Interfaces:**
- Consumes: everything the harness exposes.
- Produces: possibly a new harness helper — see Step 2.

- [ ] **Step 1: Convert, following the shared recipe**

Endpoints, all under `network.habitat.space.`: `listSpaces`, `listRepos`, `putRecord`, `getRecord`, `getBlob`, `listRecords`, `deleteRecord`, `listRepoOps`, `getLatestCommit`, `getRepo`, `getDelegationToken`, `getSpaceCredential`. Blob upload is routed at `network.habitat.repo.uploadBlob`.

The existing tests build their own host key with `atcrypto.GeneratePrivateKeyK256` and assert commits are signed with it. The harness owns the host key now — read it from `p.HostKey` rather than generating a second one, or the signature assertions will fail for a reason that has nothing to do with the code under test.

- [ ] **Step 2: Add a raw-body helper if blob upload needs one**

`Procedure` JSON-encodes its body, which is wrong for `uploadBlob`. If the blob tests need raw bytes, add to `internal/pearsetup/testutil/request.go`:

```go
// Upload issues an XRPC procedure with a raw body and an explicit content type,
// for endpoints like uploadBlob that do not take JSON.
func (p *TestPear) Upload(
	a *Actor,
	nsid string,
	contentType string,
	body []byte,
	out any,
) *http.Response {
	p.T.Helper()

	req := httptest.NewRequest(http.MethodPost, "/xrpc/"+nsid, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)

	resp := p.Do(a, req)
	p.decode(resp, out)
	return resp
}
```

Add it only if a test needs it. If nothing needs it, leave it out.

- [ ] **Step 3: Run the tests**

Run: `go test ./internal/spaces/... -v`
Expected: PASS.

- [ ] **Step 4: Confirm the stub is gone and commit**

```bash
grep -rn "authn/testutil" internal/spaces/   # expect no output
git add internal/spaces/ internal/pearsetup/
git commit -m "[spaces] Convert server tests to the pear harness"
```

---

### Task 15: Coverage config and final verification

**Files:**
- Modify: `.testcoverage.yml`

**Interfaces:**
- Consumes: everything.
- Produces: nothing.

- [ ] **Step 1: Read the current exclusions**

Run: `cat .testcoverage.yml`

Note the existing style for excluding wiring — `cmd/`, mocks, `internal/telemetry`, `internal/utils`, `internal/oauthserver`, and the `server.go` pass-throughs.

- [ ] **Step 2: Add the two new packages**

`internal/pearsetup` is assembly: its per-file coverage will be low because `pear.go` and `routes.go` are almost entirely construction, which the smoke tests execute but do not assert on line by line. `internal/pearsetup/testutil` is test infrastructure and is not the subject of coverage at all. Add both, following the file's existing syntax, with a comment saying why — matching how the `internal/pear` exclusion documents itself.

- [ ] **Step 3: Run the full Go suite**

Run: `go test ./...`
Expected: PASS. This is the first point where every conversion is checked together.

- [ ] **Step 4: Run lint and coverage**

Run: `golangci-lint run`
Expected: no output.

Run: `moon :test`
Expected: PASS, including the coverage gate.

- [ ] **Step 5: Confirm the stub validator is gone from server tests**

Run: `grep -rln "authn/testutil" internal cmd`
Expected: exactly one file, `internal/forwarding/service_proxy_test.go`, which is deliberately out of scope.

- [ ] **Step 6: Confirm the extraction actually landed**

Run: `wc -l cmd/pear/main.go internal/pearsetup/*.go`
Expected: `main.go` around 120 lines; the bulk now in `pearsetup`.

- [ ] **Step 7: Commit**

```bash
git add .testcoverage.yml
git commit -m "[pearsetup] Exclude wiring and test infra from coverage gate"
```

---

## Notes for the executor

**If `pearsetup.New` is slow in tests,** the cause is almost always a directory lookup escaping to the real network. Every `identity.DefaultDirectory()` in the moved code must have become `cfg.Directory`. Grep for it in `internal/pearsetup/` — there should be exactly one occurrence, the default in `withDefaults`.

**If a converted test hangs,** check that `DisableP2P` is set. The libp2p host binds ports and starts background goroutines.

**If SQLite locking errors appear,** note that FGA and the main store must not share a SQLite file — `setupFGA` appends `.fga.db` for exactly this reason, because gorm uses `mattn/go-sqlite3` and OpenFGA uses `modernc.org/sqlite`. The harness sidesteps this with `fgastore.NewMemory`.

**If a handler's authorization behavior looks wrong** rather than the test's expectation being wrong, stop and report it. That is a finding, not a conversion detail, and it does not belong in a refactor commit.

# Keyval Store Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `internal/keyval`, a shared GORM-backed key/value store with mandatory TTLs and per-consumer namespaces, and move `o_auth_requests` and the login provider flow state onto it.

**Architecture:** One `key_values` table keyed by `(namespace, key)` with a mandatory `expires_at`. Consumers depend on a narrow `KV` (Put/Get) interface and receive a `Namespace`-scoped instance, so isolation is structural — the namespace is in the primary key and in every statement. Reads filter `expires_at > now()`, so cleanup is an optimization rather than a correctness requirement. Postgres reclaims expired rows via a statement-level trigger; SQLite sweeps once at startup.

**Tech Stack:** Go 1.26, GORM (`gorm.io/gorm`, `gorm.io/driver/postgres`, `gorm.io/driver/sqlite`), goose + Atlas (schema migrations), testcontainers-go, testify.

**Spec:** `docs/superpowers/specs/2026-10-14-keyval-store-design.md` — read it before starting; this plan assumes its decisions and cites them.

## Global Constraints

- **Go 1.26.** Standard layout: entrypoints in `cmd/`, everything else in `internal/`.
- **TTL is mandatory on every `Put`.** There is no no-expiry mode; `expires_at` is never null.
- **Never serialize `fosite.AuthorizeRequest` or any fosite type directly.** They embed a `Client`/`Session` whose unexported state does not survive JSON. Use the explicit `requestRecord` (spec, "Consumers → `o_auth_requests`").
- **Every query is scoped `WHERE namespace = ?`.** No query may address rows outside the instance's namespace.
- **Cutoffs are bound from Go's `time.Now()`,** never `now()` in a query, so SQLite and Postgres agree and SQLite has no `now()`.
- **Conventions** (`.claude/rules/go-conventions.md`): exported `Err*` sentinels read with `errors.Is`, `fmt.Errorf` wrapping, `utils.Opt` options, no type assertions in tests, no DI containers.
- **Do not hand-edit generated migrations** except to add the trigger, and rehash with `moon run pear:migration-hash` after any hand edit.
- **Never remove existing comments** unless they are TODOs this work directly resolves (CLAUDE.md).

## Review Focus

Conditions the spec implies but no task's happy-path test covers. Each has a test in the task that owns the code.

1. **`Get`/`Consume` called with a nil `dst`** — must return an error, not panic with a nil-pointer dereference inside `json.Unmarshal`.
2. **A consumed login-state key replayed** — `Exchange` must fail the second time; single-use must survive the whole path, not just the store API.
3. **Nested login hops** (an opensocial org login running inside a member login) — the inner hop must not clobber the outer one's request key. Existing coverage at `internal/oauthserver/oauth_server_test.go:1764`; keep it passing and assert the outer hop still resumes.
4. **`[]byte` fields surviving a JSON round trip** — `pdsProviderState.DpopKey` is `[]byte`, which JSON base64-encodes; a provider that stores and reads one back must get identical bytes, not corrupted ones.
5. **A fresh `Store` over the same database reading back earlier writes** — proves state is durably in the table and not cached in memory, which a bug in the namespacing or upsert could otherwise hide.

## File Structure

| File | Responsibility |
|---|---|
| `internal/keyval/keyval.go` | Public contract: `KV`, `Store`, `ErrNotFound`, `entry` model, `Models()` |
| `internal/keyval/store.go` | Behavior: `store`, `New`, `Namespace`, `Put`, `Get`, `Delete`, `Consume`, table setup |
| `internal/keyval/keyval_test.go` | Shared dialect-agnostic suite + SQLite entry point + SQLite sweep tests |
| `internal/keyval/keyvaltest/pg.go` | Postgres testcontainer for tests (mirrors `internal/search/searchtest`) |
| `internal/keyval/store_pg_test.go` | Runs the shared suite against Postgres |
| `internal/oauthserver/fosite_storage.go` | `requestRecord`, `store` gains `kv`, PAR/code/PKCE methods move to the keyval |
| `internal/oauthserver/gc.go` | Drops the `o_auth_requests` delete (the table is gone) |
| `internal/oauthserver/oauth_server.go` | `NewOAuthServer` takes the namespaced store |
| `internal/login/{provider,google,pds,password}.go` | `Provider` state moves from `[]byte` to a store key |
| `internal/login/testutil/passthrough.go` | Matches the new `Provider` signatures |
| `internal/org/login_router.go` | Threads the key through `Authorize`/`Exchange` |
| `cmd/pear/migrations/models.go` | Adds `keyval.Models()` |
| `cmd/pear/migrations/postgres/*_keyval_sweep_trigger.sql` | Hand-written trigger (Atlas cannot generate it) |
| `cmd/pear/keyval_trigger_test.go` | Asserts the trigger actually sweeps, against the real migration |
| `cmd/pear/main.go` | Builds the root store, passes both namespaced children |
| `.testcoverage.yml` | Excludes the `keyvaltest` helper |

---

### Task 1: The `keyval` package

**Files:**
- Create: `internal/keyval/keyval.go`
- Create: `internal/keyval/store.go`
- Test: `internal/keyval/keyval_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `keyval.ErrNotFound` (`error`)
  - `type KV interface { Put(ctx context.Context, key string, value any, ttl time.Duration) error; Get(ctx context.Context, key string, dst any) error }`
  - `type Store interface { KV; Delete(ctx context.Context, key string) error; Consume(ctx context.Context, key string, dst any) error; Namespace(ns string) Store }`
  - `func New(ctx context.Context, db *gorm.DB) (Store, error)`
  - `func Models() []any`
  - `func runStoreSuite(t *testing.T, open func(t *testing.T) (Store, *gorm.DB))` — unexported, so every test file that calls it must be `package keyval`. Both Task 1's and Task 2's test files are.

- [ ] **Step 1: Write the shared suite and the SQLite entry point**

Create `internal/keyval/keyval_test.go`. Define `runStoreSuite(t, open)` taking an opener so Task 2 can reuse every test against Postgres, and `TestSQLiteStore` that calls it with a `testutil.NewDB(t, Models())`-backed opener.

The suite contains these tests. `value`/`dst` use a local struct `type payload struct{ Name string; N int }`.

| Test | Asserts |
|---|---|
| `PutGetRoundTrip` | `Put` then `Get` into `*payload` yields the same value |
| `PutUpserts` | second `Put` on the same key replaces both value and expiry; `Get` returns the new value |
| `GetMissingIsNotFound` | `Get` on an unwritten key returns an error satisfying `errors.Is(err, ErrNotFound)` |
| `GetExpiredIsNotFound` | `Put` with `-time.Second` TTL, then `Get` returns `ErrNotFound` |
| `DeleteRemoves` | `Put`, `Delete`, then `Get` returns `ErrNotFound` |
| `DeleteMissingIsNotFound` | `Delete` on an unwritten key returns `ErrNotFound` |
| `DeleteExpiredIsNotFound` | `Put` with `-time.Second` TTL, then `Delete` returns `ErrNotFound` |
| `ConsumeReturnsAndRemoves` | `Put`, `Consume` yields the value, a second `Consume` returns `ErrNotFound` |
| `ConsumeExpiredIsNotFound` | `Put` with `-time.Second` TTL, then `Consume` returns `ErrNotFound` |
| `ConcurrentConsumeYieldsOneWinner` | 8 goroutines `Consume` one key; exactly one returns no error, the rest `ErrNotFound` |
| `NamespaceIsolatesReads` | `a` writes, `b.Namespace("b").Get` returns `ErrNotFound` |
| `NamespaceIsolatesOverwrite` | `a` writes, `b` writes the same key; both read back their own value |
| `NamespaceIsolatesDelete` | `a` writes, `b` deletes that key; `a`'s value survives |
| `NilDstIsAnErrorNotAPanic` | `Get` and `Consume` with `dst == nil` each return a non-nil error and do not panic |
| `PutUnmarshalableValueWritesNothing` | `Put` a value `json.Marshal` rejects (e.g. `make(chan int)`) returns an error, and `Get` on that key afterwards returns `ErrNotFound` — proving the marshal happens before any database work |
| `FreshStoreReadsBackPriorWrites` | build a store, `Put`, then build a *second* store over the same DB and `Get` the value back (Review Focus #5) |
| `SameKeyInTwoNamespacesCoexist` | one key written under two namespaces is independently readable |

Then add two SQLite-only tests, outside the suite:

| Test | Asserts |
|---|---|
| `NewSweepsExpiredOnSQLite` | build store, `Put` one key with `-time.Second` and one with `time.Hour`, build a **second** store over the same DB, then assert only the unexpired row remains (count rows directly via `gorm` and check the expired key is gone) |

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/keyval/...`
Expected: build failure — `undefined: New`, `undefined: Models`, etc.

- [ ] **Step 3: Implement the public contract in `internal/keyval/keyval.go`**

```go
package keyval

// ErrNotFound is returned when a key is absent from the store or its entry has
// expired. The two are indistinguishable, because a caller cannot act on them
// differently.
var ErrNotFound = errors.New("keyval: entry not found")

type KV interface { /* Put, Get per Interfaces block */ }

type Store interface { /* KV + Delete, Consume, Namespace per Interfaces block */ }

type entry struct {
	Namespace string    `gorm:"column:namespace;primaryKey;size:255"`
	Key       string    `gorm:"column:key;primaryKey;size:255"`
	Value     []byte    `gorm:"column:value;not null"`
	ExpiresAt time.Time `gorm:"column:expires_at;not null;index"`
}

func (entry) TableName() string { return "key_values" }

// Models returns the GORM models this package persists, for db.Migrate.
func Models() []any { return []any{&entry{}} }
```

- [ ] **Step 4: Implement the store in `internal/keyval/store.go`**

```go
type store struct {
	db        *gorm.DB
	namespace string
}

func New(ctx context.Context, db *gorm.DB) (Store, error)
func (s *store) Namespace(ns string) Store
func (s *store) Put(ctx context.Context, key string, value any, ttl time.Duration) error
func (s *store) Get(ctx context.Context, key string, dst any) error
func (s *store) Delete(ctx context.Context, key string) error
func (s *store) Consume(ctx context.Context, key string, dst any) error
```

`New` calls `db.WithContext(ctx).AutoMigrate(&entry{})`, and on SQLite
(`db.DialectOf(db) == db.Sqlite`) then runs
`db.WithContext(ctx).Where("expires_at <= ?", time.Now()).Delete(&entry{})`.
It wraps both failures with `fmt.Errorf`. It returns the root store with an
empty namespace; no consumer is handed the root.

The `AutoMigrate` is deliberate and is a no-op in pear, where `migrations.Run`
runs first and Task 4 adds the table to the Atlas-generated migrations. It is
what lets `internal/db/testutil.NewDB(t, Models())` and `sap` build a working
store without their own migration, following the precedent
`pkg/oauthclient.NewGormStore` sets by migrating its own tables.

`Put` marshals `value` with `json.Marshal` **before** touching the database, so
a marshal error never leaves a partial write, then upserts with
`clause.OnConflict{Columns: []clause.Column{{Name: "namespace"}, {Name: "key"}},
DoUpdates: clause.AssignmentColumns([]string{"value", "expires_at"})}` on
`expires_at: time.Now().Add(ttl)`.

`Get` selects `value` where `namespace`, `key`, and `expires_at > time.Now()`
match, mapping `gorm.ErrRecordNotFound` to `ErrNotFound`, then unmarshals into
`dst`.

`Consume` issues **one** statement, not a read followed by a delete:

```sql
DELETE FROM key_values WHERE namespace = ? AND key = ? AND expires_at > ? RETURNING value
```

Use `.Raw(...).Row()` or `Clauses(clause.Returning{Columns: []clause.Column{{Name: "value"}}})`
with a `Delete`; whichever you choose, a zero `RowsAffected` maps to
`ErrNotFound`. `DELETE ... RETURNING` is atomic on both dialects (SQLite 3.35+,
Postgres), which is what makes concurrent `Consume` safe.

`Get`, `Consume` and `Delete` all reject a nil `dst` with an error before any
database work (Review Focus #1).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/keyval/... -count=1`
Expected: PASS, all suite tests plus `NewSweepsExpiredOnSQLite`.

- [ ] **Step 6: Commit**

```bash
git add internal/keyval/keyval.go internal/keyval/store.go internal/keyval/keyval_test.go
git commit -m "feat(keyval): add namespaced keyval store with mandatory TTLs"
```

---

### Task 2: Postgres dialect parity

**Files:**
- Create: `internal/keyval/keyvaltest/pg.go`
- Create: `internal/keyval/store_pg_test.go`
- Modify: `.testcoverage.yml`

**Interfaces:**
- Consumes: `keyval.New`, `keyval.Models`, and `runStoreSuite` from Task 1.
- Produces: `keyvaltest.NewDB(t *testing.T) *gorm.DB` — a Postgres database with `keyval.Models()` AutoMigrated. Also `keyvaltest.PGURLEnv` (string constant).

- [ ] **Step 1: Write the failing Postgres test**

Create `internal/keyval/store_pg_test.go` in **`package keyval`**, not
`keyval_test`, so it can reach the unexported `runStoreSuite` and the
unexported `entry` model:

```go
func TestPostgresStore(t *testing.T) {
	runStoreSuite(t, func(t *testing.T) (Store, *gorm.DB) {
		d := keyvaltest.NewDB(t)
		s, err := New(t.Context(), d)
		require.NoError(t, err)
		return s, d
	})
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/keyval/... -run TestPostgresStore`
Expected: build failure — `no required module provides package .../keyvaltest`.

- [ ] **Step 3: Implement `keyvaltest.NewDB`**

Create `internal/keyval/keyvaltest/pg.go`, modelled on `internal/search/searchtest/meilisearch.go`: a `PGURLEnv = "HABITAT_TEST_PGURL"` constant read with `os.Getenv` first; otherwise a `postgres:16-alpine` container started once under a `sync.Once` and shared across tests (matching the image `cmd/pear/pear_test.go` and `integration/testcontainers.go` already use). Open with `db.New(url)`, `AutoMigrate` the keyval models, and return the handle.

Do **not** import `cmd/pear/migrations` from this package: it imports every store including `oauthserver`, which imports `keyval` once Task 3 lands, so the import would cycle.

- [ ] **Step 4: Run the Postgres suite**

Run: `go test ./internal/keyval/... -run TestPostgresStore -count=1`
Expected: PASS. If Docker is unavailable, `HABITAT_TEST_PGURL` pointing at any
scratch Postgres works instead.

- [ ] **Step 5: Exclude the helper from coverage**

In `.testcoverage.yml`, under `exclude.paths`, add a line beside the existing
`internal/search/searchtest` entry:

```yaml
    - internal/keyval/keyvaltest # Test helper that starts Postgres
```

- [ ] **Step 6: Commit**

```bash
git add internal/keyval/keyvaltest/pg.go internal/keyval/store_pg_test.go .testcoverage.yml
git commit -m "test(keyval): run the store suite against Postgres"
```

---

### Task 3: Move `o_auth_requests` onto the store

**Files:**
- Modify: `internal/oauthserver/fosite_storage.go:26-51` (drop `OAuthRequest`, add `requestRecord`), `:56-113` (convert the three helpers), `:144-151` (`newStore` takes `kv`), `:175-222` (PAR methods), `:356-445` (code + PKCE methods), `:580-581` (`Models()`)
- Modify: `internal/oauthserver/gc.go:44-49` (drop the `OAuthRequest` delete)
- Modify: `internal/oauthserver/oauth_server.go` (`NewOAuthServer` gains a `keyval.Store` param, passed to `newStore`)
- Modify: `cmd/pear/main.go` (build the root store, pass `kv.Namespace("oauth")`)
- Modify: `internal/oauthserver/storage_test.go`, `internal/oauthserver/oauth_server_test.go`, and every other oauthserver test calling `newStore` or `testutil.NewDB(t, Models())`

**Interfaces:**
- Consumes: `keyval.Store`, `keyval.KV`, `keyval.ErrNotFound`, `keyval.New` from Task 1.
- Produces:
  - `oauthserver.NewOAuthServer(..., kv keyval.Store, ...)` — the store must already be namespaced `oauth` by the caller.
  - `store.kv keyval.Store` field.

- [ ] **Step 1: Update `newStore` and its callers so the package compiles**

Add a `kv keyval.Store` parameter to `newStore` and to `NewOAuthServer`, store
it on `store`, and update every call site. In `cmd/pear/main.go`, build the
root after `migrations.Run` and pass a namespaced child:

```go
kv, err := keyval.New(startupCtx, database)
if err != nil {
    return fmt.Errorf("setup keyval store: %w", err)
}
```

Failing here is fatal to boot, deliberately (spec, "Error handling"): a store
that silently failed to build would turn every `Put` into a runtime error later.

Then pass `kv.Namespace("oauth")` into `NewOAuthServer`. oauthserver tests that
call `testutil.NewDB(t, Models())` must add `keyval.Models()` to their model
sets and build a store the same way.

- [ ] **Step 2: Run the tests to verify they still pass (green baseline)**

Run: `go test ./internal/oauthserver/... -count=1`
Expected: PASS. This confirms the signature change alone broke nothing.

- [ ] **Step 3: Define `requestRecord` and delete `OAuthRequest`**

Replace the `OAuthRequest` struct (fosite_storage.go:37-51) with:

```go
// requestRecord is the serialized value of one in-flight OAuth flow: pushed
// authorization requests, requests bridging the PDS redirect, and issued
// authorization codes. fosite types are not serialized directly because they
// embed a Client and Session whose unexported state does not survive a JSON
// round trip.
type requestRecord struct {
	ClientID            string `json:"client_id"`
	Subject             string `json:"subject"`
	Scopes              string `json:"scopes"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	RedirectURI         string `json:"redirect_uri"`
	State               string `json:"state"`
	ResponseType        string `json:"response_type"`
	ResponseMode        string `json:"response_mode"`
}
```

- [ ] **Step 4: Convert the three helpers**

- `fromRequester(uri string, r fosite.Requester, expiresAt time.Time)` becomes
  `func requestRecordFrom(requester fosite.Requester) requestRecord`, returning
  the record only — the URI and expiry become the key and TTL at the call site.
- `toAuthorizeRequest(client fosite.Client)` becomes a method on `requestRecord`
  with the same body, reading the record's fields.
- `pkceForm()` becomes a method on `requestRecord` with the same body.

Keep all three bodies byte-for-byte where the field names do not change.

- [ ] **Step 5: Rewrite the six storage methods**

Each maps onto the store as follows; none should change their exported
behavior, including which errors wrap `fosite.ErrNotFound`.

| Method | Becomes |
|---|---|
| `CreatePARSession` | `kv.Put(ctx, requestURI, requestRecordFrom(request), parSessionTTL)` |
| `GetPARSession` | `kv.Get(ctx, requestURI, &rec)`; on `ErrNotFound`, return `errors.Join(fosite.ErrNotFound, err)`; then `s.GetClient(ctx, rec.ClientID)` and `rec.toAuthorizeRequest(client)` |
| `UpdatePARSessionSubject` | read-modify-write: `kv.Get` into a `requestRecord`, set `Subject`, `kv.Put` back with the remaining TTL |
| `DeletePARSession` | `kv.Delete(ctx, requestURI)` |
| `CreateAuthorizeCodeSession` | `kv.Put(ctx, signature, requestRecordFrom(requester), time.Until(requester.GetSession().GetExpiresAt(fosite.AuthorizeCode)))` |
| `GetAuthorizeCodeSession` | same shape as `GetPARSession` |
| `InvalidateAuthorizeCodeSession` | `kv.Delete(ctx, signature)` |
| `CreatePKCERequestSession` | read-modify-write of the two challenge fields via `kv.Get` + `kv.Put` |
| `GetPKCERequestSession` | `kv.Get` into a `requestRecord`, then return `&fosite.Request{Client: client, Form: rec.pkceForm()}` |

**Preserve the current expiry inconsistency deliberately:** `GetPKCERequestSession`
does not filter on expiry today (fosite_storage.go:437 has no `expires_at`
predicate). Routing it through `kv.Get` makes it filter, which is a behavior
change — note it in the commit message so it is a decision, not an accident.

For the read-modify-write methods, compute the new TTL as
`time.Until(existingExpiry)` so a partial update does not silently extend the
entry's life.

- [ ] **Step 6: Drop `OAuthRequest` from `Models()` and from `gc.go`**

`oauthserver.Models()` (fosite_storage.go:581) returns
`[]any{&OAuthSession{}, &ConnectedApp{}, &RegisteredClient{}}`. In `gc.go`,
delete the `OAuthRequest` delete block (lines 44-49) and drop `deleted +=`
accumulation for it; `Collector` still sweeps `o_auth_sessions`. Remove the
stale `// TODO: we need to add a goroutine here that cleans up expired sessions`
at fosite_storage.go:149 — `gc.go` already does this.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/oauthserver/... -count=1`
Expected: PASS. Then `go build ./...` to confirm no other caller broke.

- [ ] **Step 8: Commit**

```bash
git add internal/oauthserver cmd/pear/main.go
git commit -m "refactor(oauthserver): store OAuth flow state in the keyval store

The o_auth_requests table becomes one namespaced row per in-flight flow,
keyed by the same opaque URI/signature it used before. No data migration:
every row expired within its 10-minute TTL by construction.

GetPKCERequestSession now filters on expiry via kv.Get, where the direct
query did not."
```

---

### Task 4: Generate the schema migration and add the trigger

**Files:**
- Modify: `cmd/pear/migrations/models.go` (add `keyval.Models()`)
- Create: `cmd/pear/migrations/{postgres,sqlite}/<timestamp>_add_key_values.sql` (Atlas-generated)
- Create: `cmd/pear/migrations/postgres/<timestamp>_keyval_sweep_trigger.sql` (hand-written)
- Create: `cmd/pear/keyval_trigger_test.go`

**Interfaces:**
- Consumes: `keyval.Models` from Task 1; the `OAuthRequest` removal from Task 3.
- Produces: nothing consumed by later tasks; this task makes production match
  what Tasks 1-3 assume.

- [ ] **Step 1: Add `keyval.Models()` to the aggregate**

In `cmd/pear/migrations/models.go`, add `keyval.Models()` to the slice inside
`Models()` and add the `internal/keyval` import.

- [ ] **Step 2: Generate the migration**

Run: `moon run pear:migration-diff -- add_key_values`
Expected: new files in both `migrations/postgres/` and `migrations/sqlite/`
containing a `CREATE TABLE "key_values"` (with the `expires_at` index) and a
`DROP TABLE "o_auth_requests"`. **Read both files and confirm the drop is
present** — Task 3 removed the model, and this is the only step that emits it.

If `atlas migrate diff` reports it needs Docker for the Postgres dev database,
start one (`infra/docker-compose.yml`) and set
`ATLAS_POSTGRES_DEV_URL=postgres://habitat:habitat@localhost:5432/habitat?sslmode=disable&search_path=public`.

- [ ] **Step 3: Hand-write the trigger migration**

Atlas cannot generate a trigger from a GORM model, so add
`cmd/pear/migrations/postgres/<timestamp>_keyval_sweep_trigger.sql`
containing, in the `-- +goose Up` section, the function and trigger exactly as
written in the spec's "Reclaiming expired rows" section. Use
`-- +goose StatementBegin` / `-- +goose StatementEnd` around each of the
`DROP TRIGGER IF EXISTS` and `CREATE TRIGGER` statements so goose does not
split them incorrectly. The `-- +goose Down` section drops the trigger and
function.

Do not add a SQLite counterpart — SQLite has no triggers and `keyval.New`
sweeps at startup.

- [ ] **Step 4: Rehash and verify no drift**

Run: `moon run pear:migration-hash`
Then: `moon run pear:migration-check`
Expected: both succeed. The spec verified that `atlas schema diff` ignores
triggers, so the hand-written migration must produce no drift. If it *does*
report drift, stop and re-read the spec's "Schema provisioning" section rather
than editing models to silence it.

- [ ] **Step 5: Write the trigger behavior test**

Create `cmd/pear/keyval_trigger_test.go` in `package main`. Start a
`postgres:16-alpine` container the way `cmd/pear/pear_test.go` already does,
apply the **real** migrations with
`migrations.Run(ctx, migrations.PearMigrationContext{DB: d})` (this is what
makes the test exercise the actual committed SQL rather than a copy), then:

| Test | Asserts |
|---|---|
| `TestKeyvalTriggerSweepsExpiredOnWrite` | insert one row with `expires_at` in the past and one far future; insert a third row; assert only the future one remains |
| `TestKeyvalTriggerLeavesUnexpiredAlone` | with no expired rows present, a write changes no row count |
| `TestKeyvalTriggerDoesNotRecurse` | a write succeeds with no error, proving the trigger's own `DELETE` does not re-fire it |

Read rows with `d.Table("key_values").Count(...)` so the test depends only on
the table name, not on the unexported model.

This file lives in `cmd/` deliberately: it is the only place that can apply the
committed migration without an import cycle (`cmd/pear/migrations` imports
`oauthserver`, which imports `keyval`). `cmd/` is coverage-excluded in
`.testcoverage.yml`, so the test carries no threshold burden.

- [ ] **Step 6: Run the tests**

Run: `go test ./cmd/pear/... -run 'TestKeyval|TestStartup' -count=1`
Expected: PASS, including the two pre-existing startup tests against SQLite and
Postgres.

- [ ] **Step 7: Commit**

```bash
git add cmd/pear
git commit -m "feat(pear): add key_values table and expired-row sweep trigger"
```

---

### Task 5: Move login provider state out of the cookie

**Files:**
- Modify: `internal/login/provider.go:26-37` (interface signatures)
- Modify: `internal/login/google.go:38-90, 115-160`
- Modify: `internal/login/pds.go:32-80`
- Modify: `internal/login/password.go:58-84` (no store needed)
- Modify: `internal/login/testutil/passthrough.go:37-52`
- Modify: `internal/login/login_test.go`, `internal/login/google_test.go`
- Modify: `internal/org/login_router.go:107-208`
- Modify: `internal/oauthserver/oauth_server.go:62-113, 313-322, 487-514, 916-921`
- Modify: `internal/oauthserver/mcp.go:275-283`
- Modify: `internal/oauthserver/oauth_server_test.go:1764`
- Modify: `cmd/pear/main.go:278, 286-292`

**Interfaces:**
- Consumes: `keyval.Store`, `keyval.ErrNotFound` from Task 1.
- Produces:
  - `login.Provider` with `Authorize(ctx context.Context, loginHint string) (redirectURI, state, providerStateKey string, err error)` and `Exchange(ctx context.Context, query url.Values, providerStateKey string) (loginID string, profile Profile, err error)`
  - `func NewGoogleProvider(clientID, clientSecret, redirectURL string, db *gorm.DB, encryptionKey []byte, kv keyval.Store) (Provider, error)`
  - `func NewPDSProvider(oauthClient pdsclient.PdsOAuthClient, credStore pdscred.PDSCredentialStore, dir identity.Directory, kv keyval.Store) Provider`
  - `org.LoginRouter.Authorize(ctx, did) (string, string, string, error)` and `Exchange(ctx, did, query, providerStateKey string) error`

- [ ] **Step 1: Update the `Provider` interface and the password provider**

In `internal/login/provider.go`, change both method signatures to the ones in
the Interfaces block, and reword the `Authorize` doc comment: it returns an
opaque *key* into the provider's store, not the state payload itself.

`NewPasswordProvider` keeps its signature — the password provider has no
inter-provider state, so it needs no store. Its `Authorize` returns
`(redirect, "", "", nil)` and `Exchange` ignores the key. This is what keeps
`internal/org/org_test.go` and `internal/org/testutil/testutil.go` untouched.

- [ ] **Step 2: Update `PassthroughProvider`**

In `internal/login/testutil/passthrough.go`, change both method signatures to
match and keep returning empty strings for state and key.

- [ ] **Step 3: Run to confirm the failures are where you expect**

Run: `go build ./... 2>&1 | head -30`
Expected: compile errors in `google.go`, `pds.go`, `login_router.go`,
`oauth_server.go`, `mcp.go`, and `cmd/pear/main.go` — nothing else.

- [ ] **Step 4: Move the Google provider's state into the store**

In `internal/login/google.go`:

- `NewGoogleProvider` gains a trailing `kv keyval.Store` parameter, stored on
  `googleProvider`.
- `Authorize` marshals `googleProviderState` as it does today, mints a key with
  `utils.RandomNonce(32)`, `kv.Put(ctx, key, googleProviderState{Verifier, State}, loginStateTTL)`,
  and returns `(authURL, stateStr, key, nil)`.
- `Exchange` `kv.Consume(ctx, key, &s)` into a `googleProviderState` instead of
  unmarshalling `stateBytes`, and returns `keyval.ErrNotFound` wrapped as
  `fmt.Errorf("google state: %w", err)` when the key is unknown or already
  consumed (Review Focus #2 — the second call must fail).

Add `const loginStateTTL = 10 * time.Minute` in `internal/login`, matching the
OAuth session cookie's `MaxAge` at `internal/oauthserver/oauth_server.go:227`,
so a stored entry never outlives the cookie that references it.

- [ ] **Step 5: Move the PDS provider's state into the store**

In `internal/login/pds.go`, the same shape: `NewPDSProvider` gains a trailing
`kv keyval.Store`, `Authorize` puts its `pdsProviderState` under a
`utils.RandomNonce(32)` key and returns it, and `Exchange` consumes by key.

Keep the struct exactly as it is. `DpopKey []byte` becomes a base64 string on
the way into JSON and decodes back to identical bytes — add an assertion in
`internal/login/login_test.go` that the consumed `DpopKey` equals the original
`dpopKey.Bytes()` (Review Focus #4).

- [ ] **Step 6: Thread the key through `LoginRouter`**

In `internal/org/login_router.go`, change `Authorize` to return
`(string, string, string, error)` and `Exchange` to take
`providerStateKey string`. The bodies only change in the types they pass.

- [ ] **Step 7: Switch the cookie slots to carry the key**

In `internal/oauthserver/oauth_server.go`:

- `loginHop.ProviderState []byte` becomes `ProviderStateKey string` (keep the
  `json:"p"` tag).
- `saveLoginHop(session *sessions.Session, requestKey, state, providerStateKey string)`.
- `restoreLoginHop` is unchanged apart from the field type.
- `HandleCallback` (line 494) reads the cookie value as a string:
  `providerStateKey, _ := cookie.Values[providerStateCookie].(string)`.
- `HandleOpensocial` (line 916) and `mcp.go` (line 275) keep their current
  shape; only the variable type follows.

The security property is unchanged: the row is reachable only through the
signed cookie that holds the key, so a leaked OAuth `state` in a redirect URL
still cannot redeem it (spec, "Login provider state"). Do not switch to keying
the row by OAuth `state`.

- [ ] **Step 8: Wire the providers in `cmd/pear/main.go`**

Pass `kv.Namespace("login")` to `NewPDSProvider` and `NewGoogleProvider`. The
root store is already built in Task 3; reuse it.

- [ ] **Step 9: Run the tests to verify they pass**

Run: `go test ./internal/login/... ./internal/org/... ./internal/oauthserver/... -count=1`
Expected: PASS, including the nested-hop test at
`internal/oauthserver/oauth_server_test.go:1764` (Review Focus #3). Update that
test to set `session.Values[providerStateCookie]` to a key string and assert
the outer hop still resumes after the inner one.

- [ ] **Step 10: Commit**

```bash
git add internal/login internal/org internal/oauthserver cmd/pear/main.go
git commit -m "refactor(login): keep provider flow state server-side

Login providers persist their OAuth flow state in the keyval store and
return an opaque key, which rides in the OAuth session cookie in place of
the serialized payload. The cookie keeps binding the state to the browser
session exactly as before, and the pdsProviderState DPoP private key no
longer crosses the network in a signed-but-unencrypted cookie."
```

---

### Task 6: Full verification

**Files:**
- Modify: none expected. If any file changes, it is because a step below found
  something real; describe it in the commit.

**Interfaces:**
- Consumes: everything from Tasks 1-5.

- [ ] **Step 1: Build and vet the whole module**

Run: `go build ./... && go vet ./...`
Expected: no output.

- [ ] **Step 2: Run the full Go test suite**

Run: `go test ./... -count=1`
Expected: PASS. `internal/keyval` must clear the 70% package / 60% file
thresholds in `.testcoverage.yml`.

- [ ] **Step 3: Run the linter and formatters**

Run: `moon run pear:lint`
Expected: clean. `bodyclose` and `sloglint` are enabled; a new HTTP handler is
not introduced, but `sloglint` applies to any logging you added.

- [ ] **Step 4: Confirm the schema is still consistent**

Run: `moon run pear:migration-check`
Expected: pass, with no drift.

- [ ] **Step 5: Verify coverage thresholds hold**

Run: `moon run pear:coverage`
This inherits the `coverage` task from `.moon/tasks/go-library.yml`, which runs
`go-test-coverage --config .testcoverage.yml` against `coverage.out` and fails
on a threshold breach.
Expected: pass; `internal/keyval` above 70% package / 60% file, and
`keyvaltest` reported as excluded rather than as an uncovered package.

- [ ] **Step 6: Write the release note for the dropped table**

`o_auth_requests` is dropped by the generated migration, which invalidates OAuth
authorizations in flight across a deploy — bounded by the 10-minute
`parSessionTTL`, and only for users mid-authorize. Add a line to whatever
release-notes file the repo maintains, or flag it in the PR description if
there is none.

- [ ] **Step 7: Commit any remaining changes**

```bash
git add -A
git commit -m "chore: keyval store verification follow-ups"
```

Only commit if Step 6 or an earlier step actually changed a file.
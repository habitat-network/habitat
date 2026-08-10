# Sap Protocol Compliance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make sap's permissioned-space sync protocol-compliant: six sync-correctness fixes on the host and sap (A1–A6) plus space-credential authorization for repo-host reads (B1 host-side, B2 sap-side).

**Architecture:** Two parts. Host-side (`internal/spaces`, `internal/notify`): the oplog carries each op's previous CID, `listRepoOps` rejects a `since` ahead of the repo head, `notifyWrite` ships the repo's commit hash, and repo-host read handlers accept space credentials (skip membership when the credential names the space, enforced by `authn.WithSpace`). Sap-side (`pkg/sap`): recovery calls the correct getRepo NSID, deletes flow as `value: null` tombstones through the outbox into the docs-server crawler, backfill crawls verify each repo's rev/hash and requeue drift, and repo-host reads authenticate with lazily-minted, cached, renewable per-space credentials instead of a member's OAuth session.

**Tech Stack:** Go 1.26, GORM, indigo (atproto syntax/oauth/identity/spacecommit), generated `api/habitat` bindings, `lexicons/` XRPC definitions, TypeScript docs-server (node:sqlite, tsx), Moon.

---

## Context — the bugs being fixed

| # | Bug | Where | Fix |
|---|-----|-------|-----|
| A2 | `listRepoOps` never sets `OpEntry.prev`, so sap can't fold the previous element out of its LtHash on updates/deletes → hash diverges → every update/delete forces a full CAR recovery. Host rows are overwritten in place, so prev must be captured at write time. | `internal/spaces/spaces.go` | Add `PrevCid` to `spaceRecord`, set it in `PutRecord`/`DeleteRecord`, expose as `Record.Prev` from `ListRepoOps`, map to `OpEntry.Prev` in the handler. |
| A5 | A `since` beyond the repo head returns an empty page (indistinguishable from at-head), so a sap that is *ahead* of the host silently stops syncing. | `internal/spaces`, `pkg/sap/syncer` | `ListRepoOps` returns `ErrRevTooFar` → handler 400s with error name `RevNotFound`; sap maps it to desync → full recovery. |
| A6 | `notifyWrite` carries `hash: ""` even though the lexicon requires it, so sap can't detect a missed write whose rev equals its own. | `internal/notify`, `internal/spaces` | `spaces.Notifier.NotifyWrite` gains a `hash []byte` param; `PutRecord` passes the folded LtHash state; the deliverer base64-encodes it. (sap already consumes hash.) |
| B1 | Repo-host read handlers (listRepos, listRecords, getRepo, listRepoOps, getLatestCommit, getBlob, getRecord) run `IsMember(_, org, space, "")` for space credentials (empty subject) → always fail → space credentials are unusable for reads. | `internal/spaces/server.go` | Shared `spaceAuthorized` helper (mirrors `internal/notify.Server.authorize`): a credential naming the space is authorized by construction (`authn.WithSpace` already validated the match); otherwise require membership. |
| A1 | sap recovery calls `/xrpc/com.atproto.space.getRepo`, but the host mounts `network.habitat.space.getRepo` → production recovery 404s. (The e2e test mux mirrored the wrong NSID, so it never caught this.) | `pkg/sap/syncer/recover.go`, `pkg/sap/sap_test.go` | Fix the NSID in `recover.go` and the test mux. |
| A3 | Deletes never propagate: host `DeleteRecord` appends no space event and fires no `notifyWrite`, so sap is never notified; even when it does sync, `applyOps` skips emitting deletes (`op.Value == nil` → `continue`), so docs-server never removes docs. | `internal/spaces/spaces.go`, `pkg/sap/syncer/sync.go`, `typescript/apps/docs-server` | Host `DeleteRecord` appends a delete event + `notifyWrite`. Sap emits `[]byte("null")` tombstones for deletes. docs-server crawler deletes the doc/CRDT state on `value === null`. |
| A4 | Backfill crawls call `tracker.Track` per repo but never compare the host's `listRepos` rev/hash with local state, so a write that slipped past the notifier is never caught on the hourly re-crawl. | `pkg/sap/crawl`, `pkg/sap/syncer` | Add `Tracker.Check(ctx, space, did, rev, hashB64)`; crawl calls it per repo; `Engine.Check` requeues drifted repos. |
| B2 | sap talks to the repo host as a member's OAuth session; the protocol wants repo-host reads authorized by the space. | new `pkg/sap/credential`, `pkg/sap/session` | `session.Store.ClientForSpace` returns a client minting/caching/renewing space credentials (`getDelegationToken` → `getSpaceCredential`); `NotifySpaceDeleted` drops cached credentials. |

**Key facts the implementation depends on (verified):**
- `spaceRecord` rows are overwritten in place (composite PK `Space/Repo/Collection/Rkey`); `Rev` has a **global** `uniqueIndex` across the table.
- `ListRepoOps` (store) already returns `Record{Cid, Rev, ...}`; the generated `NetworkHabitatSpaceListRepoOpsOpEntry` already has `Prev string json:"prev"` and `Cid string` — only the value is never populated.
- `Validator.Validate` enforces `credInfo.Space == validator.space` when `WithSpace` is set (`internal/authn/auth_methods.go:86`), so handlers only need to skip membership when `credInfo.Space != ""`.
- sap already accepts `hash` (`Sap.NotifyWrite(ctx, space, repo, rev, hash []byte)`); `TestSap`'s mux already decodes base64 hash.
- `Cmd/pear`'s `setupPear` mux registers `com.atproto.space.getRepo` — the wrong NSID; fix it in Task 5.
- The docs-server crawler acks every message; `parseSpaceRecordUri` handles both `at://` and legacy `ats://` forms.
- Tests use `spaces_testutil.NewTestStore(t)` returning a `*testStore{Store, Notifier *testutil.TestNotifier, EventStore, FGA}`; `spaces_test.go` declares shared vars `orgId`, `owner`, `alice`, `groupType`.
- Test commands: `go test ./internal/spaces/...`, `go test ./internal/notify/...`, `go test ./pkg/sap/...`, `go test ./cmd/sap/...`, `golangci-lint run`; TypeScript: `moon docs-server:build` (and `npm test` in `typescript/apps/docs-server` once Task 6 adds it).

---

## File structure

**Host-side:**
- `internal/spaces/spaces.go` — `spaceRecord.PrevCid`, `Record.Prev`, `ErrRevTooFar`; write/read/notify changes.
- `internal/spaces/server.go` — `spaceAuthorized` helper; map `Record.Prev` → `OpEntry.Prev`; map `ErrRevTooFar` → `RevNotFound`; replace membership checks in 7 handlers.
- `internal/notify/notifier.go` — `Deliverer.NotifyWrite` sends base64 hash.
- `internal/notify/testutil/notifier.go` — `TestNotifier.NotifyWrite` records `Hash []byte`.

**Sap-side:**
- `pkg/sap/syncer/recover.go` — getRepo NSID fix.
- `pkg/sap/syncer/sync.go` — emit `null` tombstones; map `RevNotFound` → desync.
- `pkg/sap/syncer/engine.go` — new `Check` method; `Clients` unchanged.
- `pkg/sap/crawl/crawl.go` — `Tracker` gains `Check`; crawl calls it with host rev/hash.
- `pkg/sap/credential/credential.go` (new) — space credential manager + transport.
- `pkg/sap/session/session.go` / `getter.go` — `ClientForSpace` uses credentials; `DropSpace` drops caches.

**docs-server:**
- `typescript/apps/docs-server/src/crawler.ts` — `classify` helper; delete on `value === null`.
- `typescript/apps/docs-server/src/docMetadataStore.ts` — `deleteDoc`.
- `typescript/apps/docs-server/src/docCrdtStore.ts` — `deleteState`.

**Tests:**
- `internal/spaces/spaces_test.go`, `internal/spaces/server_test.go`, `internal/notify/notifier_test.go`, `pkg/sap/syncer/syncer_test.go`, `pkg/sap/crawl/crawl_test.go`, `pkg/sap/sap_test.go`, `pkg/sap/credential/credential_test.go` (new), `typescript/apps/docs-server/src/*.test.ts` (new).

**Order:** Tasks 1–4 are host-side (1=A2, 2=A5, 3=A6, 4=B1); Tasks 5–8 sap-side (5=A1, 6=A3, 7=A4, 8=B2). B1 must land before B2 so the real host accepts the credentials sap mints. Task 6 depends on Task 1 (delete ops need `prev`). Everything else is independent.

---

### Task 1: Host oplog carries previous CIDs (A2)

**Files:**
- Modify: `internal/spaces/spaces.go` — model (lines 34-45), `Record` struct (lines 67-76), `PutRecord` (558-675), `DeleteRecord` (912-969), `ListRepoOps` (845-895)
- Modify: `internal/spaces/server.go:814-825` — `OpEntry.Prev`
- Test: `internal/spaces/spaces_test.go`

- [x] **Step 1: Write the failing store tests**

Append to `internal/spaces/spaces_test.go`:

```go
// TestListRepoOpsPrev tracks the previous cid of each op so a syncer can fold
// the prior element out of its LtHash on updates and deletes.
func TestListRepoOpsPrev(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, cid1, err := s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 1})
	require.NoError(t, err)
	_, cid2, err := s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 2})
	require.NoError(t, err)

	// An update overwrites in place: one op whose prev is the old cid.
	ops, err := s.ListRepoOps(t.Context(), uri, owner, "", 100)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.Equal(t, cid1.String(), ops[0].Prev)
	require.Equal(t, cid2.String(), ops[0].Cid.String())
	require.NotNil(t, ops[0].Value)

	// A delete soft-removes the row: one op whose prev is the last cid, with no
	// cid and no value.
	require.NoError(t, s.DeleteRecord(t.Context(), uri, owner, coll, "k1"))
	ops, err = s.ListRepoOps(t.Context(), uri, owner, "", 100)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.Equal(t, cid2.String(), ops[0].Prev)
	require.Empty(t, ops[0].Cid.String())
	require.Nil(t, ops[0].Value)
}

// TestListRepoOpsPrevCreateIsEmpty pins that a create op has no prev.
func TestListRepoOpsPrevCreateIsEmpty(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 1})
	require.NoError(t, err)

	ops, err := s.ListRepoOps(t.Context(), uri, owner, "", 100)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.Empty(t, ops[0].Prev)
}
```

- [x] **Step 2: Run to verify failure**

Run: `go test ./internal/spaces/ -run TestListRepoOpsPrev -count=1`
Expected: FAIL — `ops[0].Prev` is `""`, not `cid1.String()`.

- [x] **Step 3: Add `PrevCid` to the model and `Prev` to `Record`**

In `internal/spaces/spaces.go`, change the `spaceRecord` struct (lines 34-45):

```go
type spaceRecord struct {
	Space      habitat_syntax.SpaceURI `gorm:"primaryKey"`
	Repo       syntax.DID              `gorm:"primaryKey"`
	Collection syntax.NSID             `gorm:"primaryKey"`
	Rkey       syntax.RecordKey        `gorm:"primaryKey"`
	Value      []byte
	Rev        syntax.TID `gorm:"uniqueIndex"`
	PrevCid    string     // cid of the record's prior version, for the oplog
	Cid        string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  gorm.DeletedAt
}
```

In the same file, add `Prev string` to the `Record` struct (lines 67-76):

```go
type Record struct {
	Owner      syntax.DID
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	Value      map[string]any
	Rev        string
	Prev       string
	Cid        cid.Cid
	UpdatedAt  time.Time
}
```

`AutoMigrate` (line 243) will add the `prev_cid` column to existing tables automatically.

- [x] **Step 4: Set `PrevCid` in `PutRecord`**

In `internal/spaces/spaces.go`, `PutRecord`, the "Maintain the cached LtHash" block already loads the existing row into `existing` (lines 643-652). Capture its cid before the `Save` (insert between line 652 and the `h.Add` at 653):

```go
		} else if err == nil {
			h.Remove(spacecommit.RecordElement(collection, rkey, existing.Cid))
		}
		prevCid := ""
		if err == nil {
			prevCid = existing.Cid
		}
		h.Add(spacecommit.RecordElement(collection, rkey, cid.String()))
```

Then in the `Save` (lines 658-666), add `PrevCid: prevCid`:

```go
		return tx.Save(&spaceRecord{
			Repo:       repo,
			Space:      spaceUri,
			Collection: collection,
			Rkey:       rkey,
			Value:      bytes,
			Rev:        tid,
			PrevCid:    prevCid,
			Cid:        cid.String(),
		}).Error
```

- [x] **Step 5: Set `PrevCid` in `DeleteRecord`**

In `internal/spaces/spaces.go`, `DeleteRecord`, the rows are loaded into `rows` before the `Updates`. The composite PK means at most one live row matches, so carry its cid into the update (replace the `Updates` map at lines 940-946):

```go
		if len(rows) == 0 {
			return nil
		}
		rev := s.clock.Next()
		if err := tx.Model(&spaceRecord{}).
			Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
				uri, repo, collection, rkey).
			Updates(map[string]any{
				"deleted_at": time.Now(),
				"rev":        rev,
				"prev_cid":   rows[0].Cid,
			}).Error; err != nil {
			return fmt.Errorf("delete record: %w", err)
		}
```

- [x] **Step 6: Expose `Prev` from `ListRepoOps`**

In `internal/spaces/spaces.go`, `ListRepoOps`, add `Prev: row.PrevCid` to both `Record` literals (the soft-deleted branch at 873-881 and the live branch at 883-891):

```go
		if row.DeletedAt.Valid {
			records[i] = Record{
				Owner:      row.Repo,
				Collection: row.Collection,
				Rkey:       row.Rkey,
				Value:      nil,
				Rev:        string(row.Rev),
				Prev:       row.PrevCid,
				UpdatedAt:  row.DeletedAt.Time,
				// empty cid
			}
		} else {
			records[i] = Record{
				Owner:      row.Repo,
				Collection: row.Collection,
				Rkey:       row.Rkey,
				Value:      value,
				Rev:        string(row.Rev),
				Prev:       row.PrevCid,
				UpdatedAt:  row.UpdatedAt,
				Cid:        cid.MustParse(row.Cid),
			}
		}
```

- [x] **Step 7: Map `Prev` in the server handler**

In `internal/spaces/server.go`, `ListRepoOps`, the op mapping (lines 814-825) becomes:

```go
	ops := make([]habitat.NetworkHabitatSpaceListRepoOpsOpEntry, len(records))
	for i, rec := range records {
		ops[i] = habitat.NetworkHabitatSpaceListRepoOpsOpEntry{
			Rev:        rec.Rev,
			Collection: rec.Collection.String(),
			Rkey:       rec.Rkey.String(),
			Prev:       rec.Prev,
			Cid:        rec.Cid.String(),
		}
		if !params.ExcludeValues {
			ops[i].Value = rec.Value
		}
	}
```

- [x] **Step 8: Run the tests**

Run: `go test ./internal/spaces/ -count=1`
Expected: PASS, including `TestListRepoOpsPrev` and `TestListRepoOpsPrevCreateIsEmpty`.

- [x] **Step 9: Commit**

```bash
git add internal/spaces/spaces.go internal/spaces/server.go internal/spaces/spaces_test.go
git commit -m "feat(spaces): carry previous record cid through the oplog (A2)"
```

---

### Task 2: Reject a `since` ahead of the repo head (A5)

**Files:**
- Modify: `internal/spaces/spaces.go:845-895` — `ListRepoOps`
- Modify: `internal/spaces/server.go:766-812` — map `ErrRevTooFar` → 400 `RevNotFound`
- Modify: `pkg/sap/syncer/sync.go:152-189` — `listRepoOps` error detection
- Modify: `pkg/sap/syncer/sync.go:47-57` — `syncRepo` maps it to desync
- Test: `internal/spaces/server_test.go`, `pkg/sap/syncer/syncer_test.go`

- [x] **Step 1: Write the failing handler test**

Append to `internal/spaces/server_test.go`:

```go
// TestServer_ListRepoOpsSinceAheadRejects pins that a since beyond the repo
// head is an error (not an empty page), so an ahead-of-host syncer falls back
// to a full recovery instead of silently stopping.
func TestServer_ListRepoOpsSinceAheadRejects(t *testing.T) {
	s := newTestServerWithOpts(t, testServerOptions{})
	uri, err := s.Store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 1})
	require.NoError(t, err)

	// A TID-like string that sorts after any real TID (base32 is a-z + 2-7).
	ahead := strings.Repeat("z", 13)
	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+
			"&repo="+owner.String()+"&since="+ahead,
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListRepoOps(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var body atclient.ErrorBody
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	require.Equal(t, "RevNotFound", body.Name)
}
```

(`atclient` is already imported in `server_test.go`. Note `atclient.ErrorBody` uses `Name` with json tag `error`.)

- [x] **Step 2: Write the failing sap test**

Append to `pkg/sap/syncer/syncer_test.go`:

```go
// TestEngineSyncRepoSinceAheadMarksDesynced pins that a RevNotFound from the
// host (our rev is ahead of the repo head) desyncs the repo so the dispatcher
// rebuilds it from a full getRepo snapshot.
func TestEngineSyncRepoSinceAheadMarksDesynced(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("since") != "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(atclient.ErrorBody{
				Name:    "RevNotFound",
				Message: "since is ahead of the repo head",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{
			Commit: habitat.NetworkHabitatSpaceDefsSignedCommit{Ver: 0},
		})
	}))
	t.Cleanup(srv.Close)

	e, _, db := newTestEngine(t, srv.URL)
	require.NoError(t, e.Track(t.Context(), space, repoDID))
	require.NoError(t, db.Model(&repo{}).
		Where("space = ? AND did = ?", space, repoDID).
		Updates(map[string]any{"state": stateSyncing, "rev": "3lrev"}).Error)

	require.NoError(t, e.syncRepo(t.Context(), space, repoDID))

	var r repo
	require.NoError(t, db.First(&r, "space = ? AND did = ?", space, repoDID).Error)
	require.Equal(t, stateDesynced, r.State)
	require.Contains(t, r.ErrorMsg, "ahead of the repo head")
}
```

Add `"github.com/bluesky-social/indigo/atproto/atclient"` to the imports in `syncer_test.go`.

- [x] **Step 3: Run to verify both tests fail**

Run: `go test ./internal/spaces/ -run TestServer_ListRepoOpsSinceAheadRejects -count=1 && go test ./pkg/sap/syncer/ -run TestEngineSyncRepoSinceAheadMarksDesynced -count=1`
Expected: both FAIL — the handler returns 200 with an empty ops page; the engine settles active instead of desynced.

- [x] **Step 4: Host store — return `ErrRevTooFar`**

Add to the sentinel var block in `internal/spaces/spaces.go` (lines 213-221):

```go
	ErrRepoNotFound       = errors.New("repo not found")
	ErrRevTooFar          = errors.New("since revision is ahead of the repo head")
```

In `internal/spaces/spaces.go`, `ListRepoOps` (lines 845-858), add a head check before building the query:

```go
func (s *store) ListRepoOps(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
	since string,
	limit int,
) ([]Record, error) {
	// A since beyond the repo's head revision is a client error: nothing will
	// ever be listed after it, and an empty page would be indistinguishable
	// from the normal at-head case. Syncers use the RevNotFound error to detect
	// they are ahead of the host and resync from scratch.
	if since != "" {
		var head spaceRepo
		err := s.db.WithContext(ctx).
			Where("space = ? AND repo = ?", uri, repo).
			First(&head).Error
		if err == nil && since > string(head.Rev) {
			return nil, ErrRevTooFar
		}
	}
	query := s.db.WithContext(ctx).
		Unscoped().
		Model(&spaceRecord{}).
		Where("space = ? AND repo = ?", uri, repo)
```

- [x] **Step 5: Host handler — map to `RevNotFound`**

In `internal/spaces/server.go`, `ListRepoOps`, replace the error branch at lines 808-812:

```go
	records, err := s.store.ListRepoOps(ctx, spaceURI, repoDID, params.Since, limit)
	if errors.Is(err, ErrRevTooFar) {
		httpx.WriteError(ctx, w, "RevNotFound",
			"since revision is ahead of the repo head", http.StatusBadRequest)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list repo ops: %w", err))
		return
	}
```

(Note: `internal/spaces/server.go` is `package spaces`, so refer to the sentinel as `ErrRevTooFar`, not `spaces.ErrRevTooFar`.)

- [x] **Step 6: Sap — detect `RevNotFound` in `listRepoOps`**

In `pkg/sap/syncer/sync.go`, add a sentinel near the top (after the imports):

```go
// errRevTooFar reports a RevNotFound from the host: our since is ahead of the
// repo head, so incremental sync cannot continue and the repo must be rebuilt.
var errRevTooFar = errors.New("since is ahead of the repo head")
```

In `pkg/sap/syncer/sync.go`, `listRepoOps`, replace the status check at lines 180-184:

```go
	decodeErr := json.NewDecoder(resp.Body).Decode(&output)
	closeErr := resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if body, err := decodeErrorBody(resp); err == nil && body.Name == "RevNotFound" {
			return output, fmt.Errorf("%w: %s", errRevTooFar, body.Message)
		}
		return output, fmt.Errorf("list repo ops: %s", resp.Status)
	}
```

`decodeErrorBody` decodes an `atclient.ErrorBody` from a non-OK response; add it to `pkg/sap/syncer/sync.go`:

```go
func decodeErrorBody(resp *http.Response) (atclient.ErrorBody, error) {
	var body atclient.ErrorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return body, err
	}
	return body, nil
}
```

Add `"github.com/bluesky-social/indigo/atproto/atclient"` to the imports of `sync.go`.

- [x] **Step 7: Sap — map it to desync in `syncRepo`**

In `pkg/sap/syncer/sync.go`, `syncRepo`, replace the `listRepoOps` error branch at lines 54-57:

```go
		output, err := listRepoOps(ctx, client, space, repoDID, since)
		if err != nil {
			if errors.Is(err, errRevTooFar) {
				return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
					fmt.Errorf("list repo ops: %w", err))
			}
			return e.scheduleRetry(ctx, space, repoDID, stateError, err)
		}
```

- [x] **Step 8: Run all tests**

Run: `go test ./internal/spaces/ -count=1 && go test ./pkg/sap/... -count=1`
Expected: PASS, including the two new tests and the existing `TestServer_ListRepoOps` / `TestEngineSyncRepoVerifiesAndSettles`.

- [x] **Step 9: Commit**

```bash
git add internal/spaces/spaces.go internal/spaces/server.go internal/spaces/server_test.go pkg/sap/syncer/sync.go pkg/sap/syncer/syncer_test.go
git commit -m "feat(spaces,sap): reject listRepoOps since beyond repo head (A5)"
```

---

### Task 3: `notifyWrite` carries the repo commit hash (A6)

**Files:**
- Modify: `internal/spaces/spaces.go:199-211` — `Notifier` interface
- Modify: `internal/spaces/spaces.go:637-673` — `PutRecord` notify call
- Modify: `internal/notify/notifier.go:45-73` — `Deliverer.NotifyWrite`
- Modify: `internal/notify/testutil/notifier.go` — `TestNotifier`
- Modify: `internal/notify/notifier_test.go` — update calls + assert hash
- Test: `internal/spaces/spaces_test.go`

- [ ] **Step 1: Write the failing store test**

Append to `internal/spaces/spaces_test.go`:

```go
// TestPutRecordNotifiesRepoHash pins that the notifier receives the repo's
// LtHash state so syncers can detect writes that arrive with the same rev but
// a different hash (i.e. a write we missed).
func TestPutRecordNotifiesRepoHash(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, cid1, err := s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 1})
	require.NoError(t, err)
	_, cid2, err := s.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"v": 2})
	require.NoError(t, err)

	require.Len(t, s.Notifier.Writes, 2)

	var expected spacecommit.LtHash
	expected.Add(spacecommit.RecordElement(coll, "k1", cid1.String()))
	expected.Add(spacecommit.RecordElement(coll, "k2", cid2.String()))
	require.Equal(t, expected.Sum(), s.Notifier.Writes[1].Hash)
}
```

(`spacecommit` is already imported in `spaces_test.go`; `s.Notifier` is `*testutil.TestNotifier` from `spaces_testutil.NewTestStore`.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/spaces/ -run TestPutRecordNotifiesRepoHash -count=1`
Expected: FAIL to compile — `NotifyWrite` has no hash argument.

- [ ] **Step 3: Change the `Notifier` interface**

In `internal/spaces/spaces.go`, `Notifier` (lines 201-211):

```go
type Notifier interface {
	// NotifyWrite reports that a repo advanced to a new revision within a
	// space, carrying the repo's LtHash commit state so syncers can detect
	// writes that arrive at the same rev but a different hash.
	NotifyWrite(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		rev syntax.TID,
		hash []byte,
	)
	// NotifySpaceDeleted reports that a space was deleted.
	NotifySpaceDeleted(ctx context.Context, space habitat_syntax.SpaceURI)
}
```

- [ ] **Step 4: Pass the hash from `PutRecord`**

In `internal/spaces/spaces.go`, `PutRecord`, the notifier call at line 673. `h` is the in-memory LtHash, already folded and saved by `saveRepoHash` at line 654:

```go
	s.eventStore.NotifyEvent(ctx)
	// Best-effort: notify registered syncers that this repo advanced, with the
	// LtHash state it now has so they can compare hashes.
	s.notifier.NotifyWrite(ctx, spaceUri, repo, newRev, h.State())
	return recordUri, &cid, nil
```

- [ ] **Step 5: Update the notifier deliverer**

In `internal/notify/notifier.go`, import `"encoding/base64"` and change `Deliverer.NotifyWrite` (lines 45-73):

```go
func (d *Deliverer) NotifyWrite(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	rev syntax.TID,
	hash []byte,
) {
	regs, err := d.store.ListForRepo(ctx, space, repo)
	if err != nil {
		slog.ErrorContext(ctx, "notify: list registrations",
			"err", err, "space", space, "repo", repo)
		return
	}
	if len(regs) == 0 {
		return
	}

	body, err := json.Marshal(habitat.NetworkHabitatSpaceNotifyWriteInput{
		Space: space.String(),
		Repo:  repo.String(),
		Rev:   rev.String(),
		Hash:  base64.StdEncoding.EncodeToString(hash),
	})
	if err != nil {
		slog.ErrorContext(ctx, "notify: marshal notifyWrite", "err", err)
		return
	}

	d.fanout(ctx, space.SpaceOwner(), nsidNotifyWrite, regs, body)
}
```

- [ ] **Step 6: Update `TestNotifier`**

In `internal/notify/testutil/notifier.go`:

```go
type writeCall struct {
	Space habitat_syntax.SpaceURI
	Repo  syntax.DID
	Rev   syntax.TID
	Hash  []byte
}

func (n *TestNotifier) NotifyWrite(
	_ context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	rev syntax.TID,
	hash []byte,
) {
	n.Writes = append(n.Writes, writeCall{Space: space, Repo: repo, Rev: rev, Hash: hash})
}
```

- [ ] **Step 7: Update `notifier_test.go` and assert the hash is delivered**

In `internal/notify/notifier_test.go`, every `notifier.NotifyWrite(...)` call gains a hash argument. The two-arg-plus-context calls at lines 62, 113, 132, 158 become, e.g.:

```go
	notifier.NotifyWrite(t.Context(), space, repo, "3lrev", []byte{0x01, 0x02})
```

In `TestNotifierDeliversToRegisteredEndpoints` (line 38), after the existing `require.Equal(t, repo.String(), in.Repo)` assertion, add:

```go
		require.Equal(t, "AQI=", in.Hash)
```

(That is `base64("AQI=")` of `[]byte{0x01, 0x02}`; import `"encoding/base64"` if you prefer asserting via `base64.StdEncoding.EncodeToString([]byte{0x01, 0x02})` instead of the literal.)

- [ ] **Step 8: Run all affected tests**

Run: `go test ./internal/spaces/ -count=1 && go test ./internal/notify/ -count=1`
Expected: PASS, including `TestPutRecordNotifiesRepoHash` and the four deliverer tests.

- [ ] **Step 9: Commit**

```bash
git add internal/spaces/spaces.go internal/notify/notifier.go internal/notify/testutil/notifier.go internal/notify/notifier_test.go internal/spaces/spaces_test.go
git commit -m "feat(spaces): notifyWrite delivers the repo commit hash (A6)"
```

---

### Task 4: Repo-host reads accept space credentials (B1)

**Files:**
- Modify: `internal/spaces/server.go` — add `spaceAuthorized`; replace membership checks in `ListRepos` (373-381), `GetRecord` (511-526), `GetBlob` (607-619), `ListRecords` (658-669), `GetRepo` (714-729), `ListRepoOps` (788-801), `GetLatestCommit` (902-917)
- Modify: `internal/authn/testutil/stub.go` — add `NewSuccessMethodForSpace`
- Modify: `internal/spaces/server_test.go:37-41` — `testServerOptions` gains `spaceToken`
- Modify: `internal/spaces/server_test.go:49-83` — wire the option
- Test: `internal/spaces/server_test.go`

- [ ] **Step 1: Add a space-credential auth stub**

In `internal/authn/testutil/stub.go`, add a method that authenticates as a space credential (no subject, no org):

```go
// successMethodForSpace authenticates as a space credential naming space.
type successMethodForSpace struct{ space habitat_syntax.SpaceURI }

// NewSuccessMethodForSpace returns a Method that always validates, yielding a
// credential scoped to exactly space (like a real space credential).
func NewSuccessMethodForSpace(space habitat_syntax.SpaceURI) authn.Method {
	return &successMethodForSpace{space: space}
}

func (m *successMethodForSpace) CanHandle(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func (m *successMethodForSpace) Validate(
	w http.ResponseWriter, r *http.Request, _ ...string,
) (*authn.CredentialInfo, bool) {
	return &authn.CredentialInfo{Space: m.space}, true
}
```

Check the file's existing imports (`authn`, `habitat_syntax`, `net/http`, `strings`) and add any that are missing.

- [ ] **Step 2: Write the failing handler tests**

Append to `internal/spaces/server_test.go`:

```go
// TestServer_ListReposWithSpaceCredential pins that a credential naming the
// space authorizes a repo-host read without a membership check (subject is
// empty for space credentials).
func TestServer_ListReposWithSpaceCredential(t *testing.T) {
	s := newTestServerWithOpts(t, testServerOptions{})
	everyoneOrg := org.NewEveryoneOrg("everyone.example.com")
	uri, err := s.Store.CreateSpace(t.Context(), everyoneOrg.DID(), owner, groupType, "test")
	require.NoError(t, err)
	_, _, err = s.Store.PutRecord(t.Context(), uri, owner,
		syntax.NSID("network.habitat.note"), "k1", map[string]any{"v": 1})
	require.NoError(t, err)

	s2 := newTestServerWithOpts(t, testServerOptions{
		spaceToken: authntest.NewSuccessMethodForSpace(uri),
	})

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepos?space="+uri.String(),
		http.NoBody,
	)
	req.Header.Set("Authorization", "Bearer space-credential")
	w := httptest.NewRecorder()
	s2.ListRepos(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// TestServer_ListReposRequiresMembershipWithoutSpaceCredential pins that a
// non-member with an ordinary (subject-bearing) credential is still rejected.
func TestServer_ListReposRequiresMembershipWithoutSpaceCredential(t *testing.T) {
	s := newTestServerWithOpts(t, testServerOptions{})
	everyoneOrg := org.NewEveryoneOrg("everyone.example.com")
	uri, err := s.Store.CreateSpace(t.Context(), everyoneOrg.DID(), owner, groupType, "test")
	require.NoError(t, err)

	// alice is not a member (no AddMember); an ordinary credential must fail.
	s2 := newTestServerWithOpts(t, testServerOptions{
		oauth: authntest.NewSuccessMethod(alice),
	})
	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepos?space="+uri.String(),
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s2.ListRepos(w, req)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
```

`org` and `authntest` are already imported in `server_test.go`.

- [ ] **Step 3: Run to verify they fail**

Run: `go test ./internal/spaces/ -run 'TestServer_ListRepos(WithSpaceCredential|RequiresMembership)' -count=1`
Expected: `WithSpaceCredential` FAILS with a 404 (`IsMember` on an empty subject), `RequiresMembership` PASSES.

- [ ] **Step 4: Add the `spaceAuthorized` helper**

In `internal/spaces/server.go`, add a helper next to `ListRepos` (mirrors `internal/notify.Server.authorize`, server.go:95-120):

```go
// spaceAuthorized checks the credential may read spaceURI: a space credential
// must name exactly that space (enforced by the validator's WithSpace option),
// and any other credential must belong to a member of the space. It writes the
// error response and returns false when the caller is not authorized.
func (s *Server) spaceAuthorized(
	ctx context.Context,
	w http.ResponseWriter,
	credInfo *authn.CredentialInfo,
	spaceURI habitat_syntax.SpaceURI,
) bool {
	if credInfo.Space != "" {
		if credInfo.Space != spaceURI {
			httpx.WriteInvalidRequest(ctx, w, "credential does not authorize this space",
				fmt.Errorf("credential space %q does not match %q", credInfo.Space, spaceURI))
			return false
		}
		return true
	}

	member, err := s.store.IsMember(ctx, credInfo.Org.DID(), spaceURI, credInfo.Subject)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check membership: %w", err))
		return false
	}
	if !member {
		httpx.WriteSpaceNotFound(ctx, w, fmt.Errorf("not a member"))
		return false
	}
	return true
}
```

- [ ] **Step 5: Replace the membership checks in the seven handlers**

In `internal/spaces/server.go`, replace each handler's membership block with a call to `spaceAuthorized`:

**`ListRepos`** — replace lines 373-381:

```go
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}
```

**`GetRecord`** — replace lines 511-526 (the `if credInfo.Subject != "" { ... }` block):

```go
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}
```

**`GetBlob`** — replace lines 607-619:

```go
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}
```

**`ListRecords`** — replace lines 658-669:

```go
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}
```

**`GetRepo`** — replace lines 714-729:

```go
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}
```

**`ListRepoOps`** — replace lines 788-801:

```go
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}
```

**`GetLatestCommit`** — replace lines 902-917:

```go
	if !s.spaceAuthorized(ctx, w, credInfo, spaceURI) {
		return
	}
```

Each of these uses `credInfo` from the same validator invocation, and `ctx`/`w`/`spaceURI` are already in scope. If any handler had a distinct error message (e.g. `WriteSpaceNotFound(ctx, w, fmt.Errorf("not a member: %w", err))` in `ListRepoOps`), the helper's `"not a member"` message replaces it uniformly.

- [ ] **Step 6: Wire the `spaceToken` test option**

In `internal/spaces/server_test.go`, add the field to `testServerOptions` (lines 37-41) and default it in `newTestServerWithOpts` (before the `NewServer` call):

```go
type testServerOptions struct {
	hostKey     atcrypto.PrivateKey
	oauth       authn.Method
	serviceAuth authn.Method
	spaceToken  authn.Method
}
```

In `newTestServerWithOpts`, after the `serviceAuth` default block (lines 59-61):

```go
	if opts.spaceToken == nil {
		opts.spaceToken = authn.NewSpaceCredentialAuthMethod(h, opts.hostKey)
	}
```

This requires the `h` variable — move this block after `h, err := hive.NewHive(...)` (line 63). Then pass `opts.spaceToken` as the `NewServer` argument that is currently `authn.NewSpaceCredentialAuthMethod(h, opts.hostKey)` (line 74).

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/authn/... -count=1 && go test ./internal/spaces/ -count=1`
Expected: PASS, including both new handler tests and all existing server tests (the default `spaceToken` is unchanged).

- [ ] **Step 8: Commit**

```bash
git add internal/authn/testutil/stub.go internal/spaces/server.go internal/spaces/server_test.go
git commit -m "feat(spaces): accept space credentials on repo-host reads (B1)"
```

---

### Task 5: Sap recovery uses the correct getRepo NSID (A1)

**Files:**
- Modify: `pkg/sap/syncer/recover.go:35-44` — request NSID
- Modify: `pkg/sap/sap_test.go:283-288` — mux path
- Test: `pkg/sap/syncer/syncer_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/sap/syncer/syncer_test.go`:

```go
// TestEngineRecoverRepoUsesHabitatNsid pins that full recovery calls the
// host's network.habitat.space.getRepo endpoint (com.atproto.space.getRepo
// does not exist on the host).
func TestEngineRecoverRepoUsesHabitatNsid(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")

	var requestedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	e, _, db := newTestEngine(t, srv.URL)
	require.NoError(t, e.Track(t.Context(), space, repoDID))
	require.NoError(t, db.Model(&repo{}).
		Where("space = ? AND did = ?", space, repoDID).
		Update("state", stateDesynced).Error)

	require.NoError(t, e.recoverRepo(t.Context(), space, repoDID))
	require.Equal(t, "/xrpc/network.habitat.space.getRepo", requestedPath)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/sap/syncer/ -run TestEngineRecoverRepoUsesHabitatNsid -count=1`
Expected: FAIL — `requestedPath` is `/xrpc/com.atproto.space.getRepo`.

- [ ] **Step 3: Fix the NSID**

In `pkg/sap/syncer/recover.go`, replace the path at lines 39-40:

```go
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"/xrpc/network.habitat.space.getRepo?"+params.Encode(), nil)
```

Also fix the doc comment at the top of the file (line 18) which says `com.atproto.space.getRepo`:

```go
// recoverRepo rebuilds a desynced repo from a full network.habitat.space.getRepo
// CAR snapshot: it fetches the CAR, recomputes the repo's LtHash from the
// recovered record set, verifies the CAR's signed commit, then emits the
// records and settles the repo active in a single transaction. This is the
// canonical recovery path for repos whose incremental sync failed
// verification.
```

- [ ] **Step 4: Fix the e2e mux**

In `pkg/sap/sap_test.go`, `setupPear`, the mux currently registers the wrong path at line 286:

```go
	mux.HandleFunc("/xrpc/network.habitat.space.getRepo", spacesServer.GetRepo)
```

- [ ] **Step 5: Run the tests**

Run: `go test ./pkg/sap/... -count=1 && go test ./cmd/sap/ -count=1`
Expected: PASS, including the new NSID test and `TestSap`.

- [ ] **Step 6: Commit**

```bash
git add pkg/sap/syncer/recover.go pkg/sap/syncer/syncer_test.go pkg/sap/sap_test.go
git commit -m "fix(sap): recover from network.habitat.space.getRepo (A1)"
```

---

### Task 6: Delete propagation — host events, sap tombstones, docs-server deletion (A3)

**Files:**
- Modify: `internal/spaces/spaces.go:912-969` — `DeleteRecord` appends a delete event + notifies
- Modify: `pkg/sap/syncer/sync.go:137-139` — emit `null` tombstone instead of `continue`
- Modify: `typescript/apps/docs-server/src/crawler.ts` — `classify` + delete branch
- Modify: `typescript/apps/docs-server/src/docMetadataStore.ts` — `deleteDoc`
- Modify: `typescript/apps/docs-server/src/docCrdtStore.ts` — `deleteState`
- Modify: `typescript/apps/docs-server/package.json` — add `test` script
- Test: `internal/spaces/spaces_test.go`, `pkg/sap/syncer/syncer_test.go`, `typescript/apps/docs-server/src/*.test.ts`

- [ ] **Step 1: Write the failing host test**

Append to `internal/spaces/spaces_test.go`:

```go
// TestDeleteRecordEmitsDeleteEventAndNotifies pins that a delete is propagated
// to syncers: it appends a delete event (so the event feed / outbox carries it)
// and notifies the repo advanced.
func TestDeleteRecordEmitsDeleteEventAndNotifies(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 1})
	require.NoError(t, err)
	require.NoError(t, s.DeleteRecord(t.Context(), uri, owner, coll, "k1"))

	// The notifier fired for the delete too.
	require.Len(t, s.Notifier.Writes, 2)

	// The event feed carries a delete action for the record.
	events, err := s.EventStore.ListEvents(t.Context(), uri, 0, 100)
	require.NoError(t, err)
	require.NotEmpty(t, events)
	last := events[len(events)-1]
	require.Equal(t, "delete", last.Ops[0].Action)
}
```

Verify `events.Store` exposes a `ListEvents` method (or whatever the test helper for reading appended events is) before writing this test — if it doesn't, drop the event assertion and rely on the syncer-side test in Step 2 for end-to-end coverage. At minimum the notifier assertion must hold.

- [ ] **Step 2: Write the failing syncer tombstone test**

Append to `pkg/sap/syncer/syncer_test.go`:

```go
// TestEngineSyncRepoEmitsDeleteTombstone pins that a delete op (cid empty,
// value null) is emitted to the outbox as a JSON null so consumers can remove
// their copy, rather than skipped.
func TestEngineSyncRepoEmitsDeleteTombstone(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	clock := syntax.NewTIDClock(0)
	rev1, rev2 := clock.Next().String(), clock.Next().String()

	ops := []habitat.NetworkHabitatSpaceListRepoOpsOpEntry{
		{Rev: rev1, Collection: "network.habitat.test", Rkey: "k1", Cid: "bafyaaa",
			Value: map[string]any{"n": 1}},
		{Rev: rev2, Collection: "network.habitat.test", Rkey: "k1", Prev: "bafyaaa"},
	}
	var lt spacecommit.LtHash
	commit := habitat.NetworkHabitatSpaceDefsSignedCommit{
		Ver:  int64(spacecommit.Version),
		Rev:  rev2,
		Hash: base64.StdEncoding.EncodeToString(lt.Sum()),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := habitat.NetworkHabitatSpaceListRepoOpsOutput{Commit: commit}
		if r.URL.Query().Get("since") == "" {
			out.Ops = ops
			out.Cursor = rev2
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)

	e, emitter, db := newTestEngine(t, srv.URL)
	require.NoError(t, e.Track(t.Context(), space, repoDID))
	require.NoError(t, db.Model(&repo{}).
		Where("space = ? AND did = ?", space, repoDID).
		Update("state", stateSyncing).Error)

	require.NoError(t, e.syncRepo(t.Context(), space, repoDID))

	require.Len(t, emitter.emitted, 1)
}
```

This test needs `memEmitter` to record tombstone emissions. In Step 4 we change `applyOps` to emit `null` for deletes; update `memEmitter.Emit` in `syncer_test.go` to also record values:

```go
type memEmitter struct {
	mu      sync.Mutex
	emitted []habitat_syntax.SpaceRecordURI
	values  map[habitat_syntax.SpaceRecordURI][]byte
}

func (e *memEmitter) Emit(
	_ context.Context,
	uri habitat_syntax.SpaceRecordURI,
	value []byte,
) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emitted = append(e.emitted, uri)
	if e.values == nil {
		e.values = make(map[habitat_syntax.SpaceRecordURI][]byte)
	}
	e.values[uri] = value
	return nil
}
```

Then extend the test with:

```go
	require.Equal(t, []byte("null"), emitter.values["ats://did:plc:owner/network.habitat.space/s1/network.habitat.test/k1"])
```

- [ ] **Step 3: Run both tests to verify they fail**

Run: `go test ./internal/spaces/ -run TestDeleteRecordEmitsDeleteEventAndNotifies -count=1 && go test ./pkg/sap/syncer/ -run TestEngineSyncRepoEmitsDeleteTombstone -count=1`
Expected: host test FAILS (no delete event, one notify only); syncer test FAILS (zero emissions).

- [ ] **Step 4: Host — emit the delete event and notify**

In `internal/spaces/spaces.go`, `DeleteRecord`, the transaction currently folds the deleted rows out of the LtHash and updates the rows. Before the `h.Remove` loop (around line 954), append a delete event per deleted record, and notify after the transaction. The full tail of `DeleteRecord` becomes:

```go
		rev := s.clock.Next()
		if err := tx.Model(&spaceRecord{}).
			Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
				uri, repo, collection, rkey).
			Updates(map[string]any{
				"deleted_at": time.Now(),
				"rev":        rev,
				"prev_cid":   rows[0].Cid,
			}).Error; err != nil {
			return fmt.Errorf("delete record: %w", err)
		}
		// Propagate the delete to syncers as a space event carrying no value.
		for _, row := range rows {
			recordUri := habitat_syntax.ConstructSpaceRecordURI(uri, repo, row.Collection, row.Rkey)
			if err := s.eventStore.WithTx(tx).AppendSpaceEvent(
				ctx,
				uri,
				repo,
				rev,
				row.PrevCid,
				[]events.EventOps{
					{
						Action: "delete",
						Uri:    recordUri,
						Value:  nil,
						Cid:    "",
					},
				},
			); err != nil {
				return fmt.Errorf("append delete event: %w", err)
			}
		}
		// Fold the deleted records out of the cached LtHash.
		h, _, _, err := loadRepoHash(tx, uri, repo)
		if err != nil {
			return err
		}
		for _, row := range rows {
			h.Remove(spacecommit.RecordElement(row.Collection, row.Rkey, row.Cid))
		}
		// Drop the hash row entirely once the repo holds no more records
		var remaining int64
		if err := tx.Model(&spaceRecord{}).
			Where("space = ? AND repo = ?", uri, repo).
			Count(&remaining).Error; err != nil {
			return err
		}
		if remaining == 0 {
			return tx.Where("space = ? AND repo = ?", uri, repo).Delete(&spaceRepo{}).Error
		}
		return saveRepoHash(tx, uri, repo, h, rev)
	})
	if err != nil {
		return fmt.Errorf("delete record: %w", err)
	}
	s.eventStore.NotifyEvent(ctx)
	// Best-effort: tell syncers the repo advanced so they pull the delete op.
	if remaining > 0 {
		s.notifier.NotifyWrite(ctx, uri, repo, rev, h.State())
	}
	return nil
}
```

Note: `remaining` and `rev` and `h` are scoped inside the transaction closure; `s.eventStore`, `s.notifier`, `events`, and `habitat_syntax` are already imported. If the transaction-early-returns (remaining == 0), `rev`/`h` are unavailable after the closure — handle by computing the notify inside the closure via a captured `outRev`/`outHash` var declared before the closure, or move the notify into the closure's success path. Use the latter: declare `var outRev syntax.TID; var outHash []byte` before the closure, assign them at each success path (`outRev = rev; outHash = h.State()`), then after the closure:

```go
	if err != nil {
		return fmt.Errorf("delete record: %w", err)
	}
	s.eventStore.NotifyEvent(ctx)
	s.notifier.NotifyWrite(ctx, uri, repo, outRev, outHash)
	return nil
```

- [ ] **Step 5: Sap — emit `null` tombstones for deletes**

In `pkg/sap/syncer/sync.go`, `applyOps`, replace the skip at lines 137-139:

```go
		if op.Value == nil {
			// A delete: emit a JSON null tombstone so consumers remove their
			// copy. The URI identifies the deleted record.
			uri := habitat_syntax.ConstructSpaceRecordURI(space, repoDID, collection, rkey)
			if err := emitter.Emit(ctx, uri, []byte("null")); err != nil {
				return err
			}
			continue
		}
```

(Note `uri` is already computed below at line 144 for the value case; this adds a second construction for the delete case — fine.)

- [ ] **Step 6: docs-server — metadata store delete**

In `typescript/apps/docs-server/src/docMetadataStore.ts`, add:

```ts
  // deleteDoc removes a doc when its record is deleted.
  deleteDoc(spaceUri: string): void {
    this.db.prepare(`DELETE FROM docs WHERE space_uri = ?`).run(spaceUri);
  }
```

- [ ] **Step 7: docs-server — CRDT store delete**

In `typescript/apps/docs-server/src/docCrdtStore.ts`, add:

```ts
  // deleteState removes a doc's persisted CRDT state when its record is deleted.
  deleteState(spaceUri: string): void {
    this.db.prepare(`DELETE FROM doc_crdt WHERE space_uri = ?`).run(spaceUri);
  }
```

- [ ] **Step 8: docs-server — crawler classifies and deletes**

In `typescript/apps/docs-server/src/crawler.ts`, add a `classify` helper and use it in `process`. Add after the `OutboxMessage` interface:

```ts
// A single outbox message classifies into at most one store mutation.
export type CrawlAction =
  | { kind: "delete"; spaceUri: string }
  | {
      kind: "upsert";
      spaceUri: string;
      docId: string;
      title: string;
      blob?: string;
    }
  | { kind: "none" };

// classify decides what the crawler should do with an outbox message: null
// value means the record was deleted; otherwise the doc's markdown or CRDT
// record is upserted.
export function classify(msg: OutboxMessage, parsed: ParsedRecordUri): CrawlAction {
  if (msg.value === null) {
    return { kind: "delete", spaceUri: parsed.spaceUri };
  }
  if (parsed.collection === MARKDOWN_COLLECTION) {
    const value = (msg.value ?? {}) as { title?: string };
    return {
      kind: "upsert",
      spaceUri: parsed.spaceUri,
      docId: parsed.skey,
      title: value.title || "Untitled",
    };
  }
  if (parsed.collection === CRDT_COLLECTION) {
    const value = (msg.value ?? {}) as { blob?: string };
    if (value.blob) {
      return { kind: "upsert", spaceUri: parsed.spaceUri, docId: "", title: "", blob: value.blob };
    }
    return { kind: "none" };
  }
  return { kind: "none" };
}
```

Replace the body of `process` (lines 151-178):

```ts
  private async process(msg: OutboxMessage): Promise<void> {
    const parsed = parseSpaceRecordUri(msg.uri);
    if (!parsed) {
      return;
    }
    if (parsed.type === ORG_SPACE_TYPE) {
      // The space owner is the org whose membership changed; refetch it.
      await this.orgs.refresh(parsed.owner);
      return;
    }
    const action = classify(msg, parsed);
    if (action.kind === "delete") {
      this.meta.deleteDoc(action.spaceUri);
      await this.crdt.deleteState(action.spaceUri);
      return;
    }
    if (action.kind === "upsert") {
      if (action.blob) {
        await this.crdt.upsertState(action.spaceUri, action.blob);
      } else {
        this.meta.upsertDoc({
          spaceUri: action.spaceUri,
          docId: action.docId,
          title: action.title,
        });
      }
    }
  }
```

- [ ] **Step 9: docs-server — test script and tests**

Add a `test` script to `typescript/apps/docs-server/package.json` (tsx is already a dependency):

```json
  "scripts": {
    "dev": "tsx watch src/index.ts",
    "build": "tsc --build --force",
    "start": "node dist/index.js",
    "test": "tsx --test src/**/*.test.ts"
  }
```

Create `typescript/apps/docs-server/src/crawler.test.ts`:

```ts
import assert from "node:assert/strict";
import test from "node:test";
import { DatabaseSync } from "node:sqlite";
import { classify, parseSpaceRecordUri } from "./crawler";
import { DocMetadataStore } from "./docMetadataStore";
import { DocCrdtStore } from "./docCrdtStore";

test("parseSpaceRecordUri parses at:// and ats:// forms", () => {
  const current = parseSpaceRecordUri(
    "at://did:web:org/space/network.habitat.group/s1/did:web:org/network.habitat.docs.markdown/self",
  );
  assert.deepEqual(current, {
    spaceUri: "at://did:web:org/space/network.habitat.group/s1",
    owner: "did:web:org",
    type: "network.habitat.group",
    skey: "s1",
    collection: "network.habitat.docs.markdown",
  });

  const legacy = parseSpaceRecordUri(
    "ats://did:web:org/network.habitat.group/s1/did:web:org/network.habitat.docs.markdown/self",
  );
  assert.equal(legacy?.spaceUri, current?.spaceUri);
});

test("classify maps a null value to a delete", () => {
  const parsed = parseSpaceRecordUri(
    "at://did:web:org/space/network.habitat.group/s1/did:web:org/network.habitat.docs.markdown/self",
  );
  const action = classify({ id: 1, uri: "x", value: null }, parsed!);
  assert.deepEqual(action, { kind: "delete", spaceUri: parsed!.spaceUri });
});

test("classify maps a markdown value to an upsert", () => {
  const parsed = parseSpaceRecordUri(
    "at://did:web:org/space/network.habitat.group/s1/did:web:org/network.habitat.docs.markdown/self",
  );
  const action = classify({ id: 1, uri: "x", value: { title: "Hi" } }, parsed!);
  assert.deepEqual(action, {
    kind: "upsert",
    spaceUri: parsed!.spaceUri,
    docId: "s1",
    title: "Hi",
  });
});

test("deleteDoc removes the doc", () => {
  const db = new DatabaseSync(":memory:");
  const meta = new DocMetadataStore(db);
  meta.upsertDoc({ spaceUri: "s", docId: "d", title: "t" });
  meta.deleteDoc("s");
  assert.deepEqual(meta.docsBySpaceUris(["s"]), []);
});

test("deleteState removes the crdt state", () => {
  const db = new DatabaseSync(":memory:");
  // DocCrdtStore requires a PearClient; exercise the SQL directly via the same
  // table so we don't fake the client.
  db.exec(`CREATE TABLE IF NOT EXISTS doc_crdt (
    space_uri TEXT PRIMARY KEY, state TEXT NOT NULL, updated_at INTEGER NOT NULL)`);
  db.prepare(`INSERT INTO doc_crdt (space_uri, state, updated_at) VALUES (?, ?, ?)`)
    .run("s", "state", 1);
  const crdt = new DocCrdtStore(
    { putRecord: async () => ({ uri: "", cid: "" }), getRecord: async () => undefined } as never,
    db,
  );
  crdt.deleteState("s");
  const row = db.prepare(`SELECT COUNT(*) AS n FROM doc_crdt`).get();
  assert.equal((row as { n: number }).n, 0);
});
```

Note the `DocCrdtStore` constructor creates the table itself; the test's own `CREATE TABLE IF NOT EXISTS` is harmless. If the constructor signature is awkward to fake, simplify the CRDT store test to use `new DocCrdtStore(...)` with a minimal PearClient stub (match `createSpace`/`spaceUri`/`putRecord`/`getRecord`/`grantRole` as needed for construction only).

- [ ] **Step 10: Run all tests**

Run: `go test ./internal/spaces/ -count=1 && go test ./pkg/sap/syncer/ -count=1 && npm test`
Expected: PASS (in `typescript/apps/docs-server`, run `npm test`). Then run `moon docs-server:build` to confirm TypeScript compiles.

- [ ] **Step 11: Commit**

```bash
git add internal/spaces/spaces.go internal/spaces/spaces_test.go pkg/sap/syncer/sync.go pkg/sap/syncer/syncer_test.go typescript/apps/docs-server/src/crawler.ts typescript/apps/docs-server/src/docMetadataStore.ts typescript/apps/docs-server/src/docCrdtStore.ts typescript/apps/docs-server/src/crawler.test.ts typescript/apps/docs-server/package.json
git commit -m "feat(sap,spaces,docs-server): propagate record deletes end-to-end (A3)"
```

---

### Task 7: Crawl verifies repo rev/hash and requeues drift (A4)

**Files:**
- Modify: `pkg/sap/crawl/crawl.go:69-72` — `Tracker` interface
- Modify: `pkg/sap/crawl/crawl.go:304-330` — `enumerateRepos` calls `Check`
- Modify: `pkg/sap/syncer/engine.go` — add `Check`
- Modify: `pkg/sap/crawl/crawl_test.go` — `recorder` gains `Check`
- Test: `pkg/sap/crawl/crawl_test.go`, `pkg/sap/syncer/syncer_test.go`

- [ ] **Step 1: Write the failing engine test**

Append to `pkg/sap/syncer/syncer_test.go`:

```go
// TestEngineCheckRequeuesDriftedRepo pins that a backfill crawl's rev/hash
// comparison requeues an active repo that has drifted from the host (a write
// that slipped past the notifier).
func TestEngineCheckRequeuesDriftedRepo(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	clock := syntax.NewTIDClock(0)
	rev1, rev2 := clock.Next().String(), clock.Next().String()

	e, _, db := newTestEngine(t, "https://example.test")
	var lt spacecommit.LtHash
	lt.Add(spacecommit.RecordElement("network.habitat.test", "k1", "bafyaaa"))
	require.NoError(t, e.Track(t.Context(), space, repoDID))
	require.NoError(t, db.Model(&repo{}).
		Where("space = ? AND did = ?", space, repoDID).
		Updates(map[string]any{
			"state": stateActive,
			"rev":   rev1,
			"hash":  lt.State(),
		}).Error)

	// Host reports a newer rev (a write happened that we missed).
	require.NoError(t, e.Check(t.Context(), space, repoDID, syntax.TID(rev2), base64.StdEncoding.EncodeToString(lt.Sum())))

	var r repo
	require.NoError(t, db.First(&r, "space = ? AND did = ?", space, repoDID).Error)
	require.Equal(t, statePending, r.State)
}

// TestEngineCheckLeavesCurrentReposActive pins that an up-to-date repo is not
// requeued by the crawl.
func TestEngineCheckLeavesCurrentReposActive(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	rev := syntax.NewTIDClock(0).Next().String()

	e, _, db := newTestEngine(t, "https://example.test")
	var lt spacecommit.LtHash
	lt.Add(spacecommit.RecordElement("network.habitat.test", "k1", "bafyaaa"))
	require.NoError(t, e.Track(t.Context(), space, repoDID))
	require.NoError(t, db.Model(&repo{}).
		Where("space = ? AND did = ?", space, repoDID).
		Updates(map[string]any{"state": stateActive, "rev": rev, "hash": lt.State()}).Error)

	require.NoError(t, e.Check(t.Context(), space, repoDID, syntax.TID(rev), base64.StdEncoding.EncodeToString(lt.Sum())))

	var r repo
	require.NoError(t, db.First(&r, "space = ? AND did = ?", space, repoDID).Error)
	require.Equal(t, stateActive, r.State)
}
```

- [ ] **Step 2: Write the failing crawl test**

Append to `pkg/sap/crawl/crawl_test.go`:

```go
// TestCrawlerChecksRepoRevAndHash pins that the crawl compares the host's
// listRepos rev/hash against the tracker (so drift requeues) instead of only
// tracking newly-seen repos.
func TestCrawlerChecksRepoRevAndHash(t *testing.T) {
	space := habitat_syntax.SpaceURI("ats://did:web:owner/network.habitat.space/s1")
	repoDID := syntax.DID("did:web:alice")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.listSpaces":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListSpacesOutput{
				Spaces: []habitat.NetworkHabitatSpaceListSpacesSpace{{Uri: space.String()}},
			})
		case "/xrpc/network.habitat.space.listRepos":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListReposOutput{
				Repos: []habitat.NetworkHabitatSpaceListReposRepo{
					{Did: repoDID.String(), Rev: "3lrev2", Hash: "aGFzaA=="},
				},
			})
		}
	}))
	t.Cleanup(srv.Close)

	rec := &recorder{}
	c, err := New(db_testutil.NewDB(t), fakeClients{base: mustParseURL(t, srv.URL)}, rec, rec, nil, nil, nil)
	require.NoError(t, err)

	client := &http.Client{Transport: rewriteTransport{base: mustParseURL(t, srv.URL)}}
	require.NoError(t, c.enumerateRepos(t.Context(), client, space))

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Contains(t, rec.checks, repoDID)
}
```

Add a `mustParseURL` helper if the file lacks one (mirror `rewriteTransport` usage from the existing tests). Extend the `recorder` type (lines 37-63) with a `checks` field and a `Check` method:

```go
type recorder struct {
	mu      sync.Mutex
	access  []habitat_syntax.SpaceURI
	tracked []syntax.DID
	checks  []syntax.DID
}

func (r *recorder) Check(
	_ context.Context,
	_ habitat_syntax.SpaceURI,
	repo syntax.DID,
	_ syntax.TID,
	_ string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checks = append(r.checks, repo)
	return nil
}
```

- [ ] **Step 3: Run both tests to verify they fail**

Run: `go test ./pkg/sap/syncer/ -run TestEngineCheck -count=1 && go test ./pkg/sap/crawl/ -run TestCrawlerChecksRepoRevAndHash -count=1`
Expected: FAIL — `Check` is undefined on `*Engine` / the recorder's `checks` stays empty.

- [ ] **Step 4: Add `Check` to the engine**

In `pkg/sap/syncer/engine.go`, add a method after `NotifyWrite` (line 200):

```go
// Check is the backfill-crawl counterpart of NotifyWrite: it requeues a repo
// whose stored rev/hash no longer matches what the host's listRepos reported
// (a write that slipped past the notifier). Unknown repos are tracked.
func (e *Engine) Check(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	rev syntax.TID,
	hashB64 string,
) error {
	err := e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var r repo
		err := tx.Where("space = ? AND did = ?", space, did).First(&r).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&repo{Space: space, DID: did, State: statePending}).Error
		}
		if err != nil {
			return err
		}
		switch r.State {
		case statePending, stateDesynced:
			// Already claimable (or headed for full recovery); nothing to do.
			return nil
		}
		// The host's listRepos output is the ground truth. An active repo that
		// is at the host's rev with a matching hash is current; anything else
		// is requeued (error/syncing repos get another pass anyway).
		matches := r.Rev == rev
		if matches && hashB64 != "" {
			hostSum, err := base64.StdEncoding.DecodeString(hashB64)
			if err != nil {
				return fmt.Errorf("decode host hash: %w", err)
			}
			matches = bytes.Equal(spacecommit.Load(r.Hash).Sum(), hostSum)
		}
		if r.State == stateActive && matches {
			return nil
		}
		return tx.Model(&repo{}).
			Where("space = ? AND did = ?", space, did).
			Updates(map[string]any{
				"state":       statePending,
				"retry_count": 0,
				"retry_after": 0,
				"dirty":       false,
			}).Error
	})
	if err != nil {
		return fmt.Errorf("check repo: %w", err)
	}
	e.notif.Notify()
	return nil
}
```

Add `"encoding/base64"` to the imports of `engine.go` (`bytes` is already imported).

- [ ] **Step 5: Extend the crawl `Tracker` and call `Check`**

In `pkg/sap/crawl/crawl.go`, extend the interface (lines 69-72):

```go
// Tracker receives discovered repos and reports host-observed rev/hash for
// drift detection. Satisfied by syncer.Engine.
type Tracker interface {
	Track(ctx context.Context, space habitat_syntax.SpaceURI, repo syntax.DID) error
	Check(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		rev syntax.TID,
		hashB64 string,
	) error
}
```

In `enumerateRepos` (lines 324-328), replace the `Track` call:

```go
	for _, r := range output.Repos {
		did := syntax.DID(r.Did)
		hashB64 := ""
		if h, ok := r.Hash.(string); ok {
			hashB64 = h
		}
		if err := c.tracker.Check(ctx, space, did, syntax.TID(r.Rev), hashB64); err != nil {
			return err
		}
	}
	return nil
```

- [ ] **Step 6: Run the tests**

Run: `go test ./pkg/sap/... -count=1`
Expected: PASS, including the new engine and crawl tests and the existing crawl/syncer tests.

- [ ] **Step 7: Commit**

```bash
git add pkg/sap/crawl/crawl.go pkg/sap/crawl/crawl_test.go pkg/sap/syncer/engine.go pkg/sap/syncer/syncer_test.go
git commit -m "feat(sap): crawl verifies repo rev/hash and requeues drift (A4)"
```

---

### Task 8: Sap uses space credentials for repo-host reads (B2)

> Depends on Task 4 (B1): the host must accept space credentials before sap switches to them. The e2e's permissive host stub also needs the two credential endpoints mounted (Step 4).

**Files:**
- Create: `pkg/sap/credential/credential.go`
- Create: `pkg/sap/credential/credential_test.go`
- Modify: `pkg/sap/session/getter.go` — `resumed` gains `DelegationToken`, `manager()`, `credentialClient(space)`, `dropCredential(space)`
- Modify: `pkg/sap/session/session.go` — `Store.ClientForSpace` returns a credential client; `DropSpace` drops caches
- Modify: `pkg/sap/sap_test.go` — mount the credential endpoints in `setupPear`
- Test: `pkg/sap/credential/credential_test.go`, `pkg/sap/sap_test.go` (existing `TestSap`)

- [ ] **Step 1: Write the failing credential unit test**

Create `pkg/sap/credential/credential_test.go`:

```go
package credential

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// stubDelegator hands out a fixed delegation token.
type stubDelegator struct{}

func (stubDelegator) DelegationToken(context.Context, habitat_syntax.SpaceURI) (string, error) {
	return "test-delegation", nil
}

// TestManagerMintsCachesAndAuthenticates covers mint, cache (no second mint on
// a hot call), and a repo-host request carrying the credential.
func TestManagerMintsCachesAndAuthenticates(t *testing.T) {
	var (
		mu           sync.Mutex
		credCalls    int
		repoAuth     string
		repoRevCalls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.getSpaceCredential":
			mu.Lock()
			credCalls++
			mu.Unlock()
			require.Equal(t, "Bearer test-delegation", r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(
				habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: "space-cred"})
		case "/xrpc/network.habitat.space.listRepos":
			mu.Lock()
			repoAuth = r.Header.Get("Authorization")
			repoRevCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListReposOutput{})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	space := habitat_syntax.SpaceURI("at://did:web:org/space/network.habitat.group/s1")
	m := NewManager(srv.URL, srv.Client(), stubDelegator{})

	token, err := m.Credential(t.Context(), space)
	require.NoError(t, err)
	require.Equal(t, "space-cred", token)

	// Cached: a second call does not re-mint.
	token, err = m.Credential(t.Context(), space)
	require.NoError(t, err)
	require.Equal(t, "space-cred", token)
	mu.Lock()
	require.Equal(t, 1, credCalls)
	mu.Unlock()

	// A repo-host read through the manager's client carries the credential.
	resp, err := m.ClientForSpace(space).Get(
		"/xrpc/network.habitat.space.listRepos?space=" + space.String())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	mu.Lock()
	require.Equal(t, "Bearer space-cred", repoAuth)
	require.Equal(t, 1, repoRevCalls)
	mu.Unlock()

	// DropSpace evicts; the next mint re-exchanges.
	m.DropSpace(space)
	token, err = m.Credential(t.Context(), space)
	require.NoError(t, err)
	require.Equal(t, "space-cred", token)
	mu.Lock()
	require.Equal(t, 2, credCalls)
	mu.Unlock()
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/sap/credential/ -run TestManagerMintsCachesAndAuthenticates -count=1`
Expected: FAIL — package `credential` does not exist yet.

- [ ] **Step 3: Implement the credential package**

Create `pkg/sap/credential/credential.go`:

```go
// Package credential mints, caches, and renews per-space host credentials for
// sap's repo-host reads, so a syncer talks to the host as the space (authorized
// by the space's membership) rather than as an individual member's OAuth
// session.
package credential

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// renewalLead is how far before a credential's nominal expiry it is renewed.
const renewalLead = 5 * time.Minute

// Delegator mints a short-lived delegation token for a space. The session's
// OAuth access authorizes it (getDelegationToken is OAuth-only).
type Delegator interface {
	DelegationToken(ctx context.Context, space habitat_syntax.SpaceURI) (string, error)
}

// Manager mints and caches space credentials for one host (the session's
// Habitat instance). Credentials are minted lazily on first use, shared across
// callers via singleflight, and renewed just before they expire.
type Manager struct {
	host  string       // host base URL (scheme + host)
	httpc *http.Client // for the credential exchange and repo-host reads
	deleg Delegator    // mints delegation tokens
	sf    singleflight.Group

	mu     sync.Mutex
	creds  map[habitat_syntax.SpaceURI]string
	expire map[habitat_syntax.SpaceURI]time.Time
}

// NewManager builds a manager for one host.
func NewManager(host string, httpc *http.Client, deleg Delegator) *Manager {
	return &Manager{
		host:   host,
		httpc:  httpc,
		deleg:  deleg,
		creds:  make(map[habitat_syntax.SpaceURI]string),
		expire: make(map[habitat_syntax.SpaceURI]time.Time),
	}
}

// Credential returns a valid space credential for space, minting or renewing
// it as needed. Concurrent mints for the same space are deduped.
func (m *Manager) Credential(ctx context.Context, space habitat_syntax.SpaceURI) (string, error) {
	m.mu.Lock()
	token, exp := m.creds[space], m.expire[space]
	m.mu.Unlock()
	if token != "" && time.Until(exp) > renewalLead {
		return token, nil
	}
	tok, err, _ := m.sf.Do(space.String(), func() (any, error) {
		return m.mint(ctx, space)
	})
	if err != nil {
		return "", err
	}
	return tok.(string), nil
}

// mint exchanges a fresh delegation token for a space credential. The host
// mints credentials with ~1h expiry; renewing just before expiry (see
// renewalLead) keeps them from going stale.
func (m *Manager) mint(ctx context.Context, space habitat_syntax.SpaceURI) (string, error) {
	delegation, err := m.deleg.DelegationToken(ctx, space)
	if err != nil {
		return "", fmt.Errorf("get delegation token: %w", err)
	}
	body, err := json.Marshal(habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
		Space: space.String(),
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		m.abs("/xrpc/network.habitat.space.getSpaceCredential"), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+delegation)
	resp, err := m.httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("get space credential: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("get space credential: %s: %s", resp.Status, msg)
	}
	var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode space credential: %w", err)
	}
	m.mu.Lock()
	m.creds[space] = out.Credential
	m.expire[space] = time.Now().Add(time.Hour)
	m.mu.Unlock()
	return out.Credential, nil
}

// DropSpace evicts the cached credential for a deleted space.
func (m *Manager) DropSpace(space habitat_syntax.SpaceURI) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.creds, space)
	delete(m.expire, space)
}

// ClientForSpace returns an *http.Client that attaches a valid space
// credential to every request for space.
func (m *Manager) ClientForSpace(space habitat_syntax.SpaceURI) *http.Client {
	return &http.Client{Transport: &spaceTransport{m: m, space: space}}
}

// spaceTransport resolves path-only repo-host requests against the host and
// authenticates them with the space's credential.
type spaceTransport struct {
	m     *Manager
	space habitat_syntax.SpaceURI
}

func (t *spaceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !req.URL.IsAbs() {
		base, err := url.Parse(t.m.host)
		if err != nil {
			return nil, fmt.Errorf("parse host url: %w", err)
		}
		req.URL.Scheme = base.Scheme
		req.URL.Host = base.Host
	}
	token, err := t.m.Credential(req.Context(), t.space)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return t.m.httpc.Do(req)
}

func (m *Manager) abs(path string) string {
	return m.host + path
}
```

- [ ] **Step 4: Wire the session store to mint and use credentials**

In `pkg/sap/session/getter.go`, add `sync.Mutex` and the credential manager to `resumed`, plus the `DelegationToken` implementation:

```go
import (
	...
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/pkg/sap/credential"
)

type resumed struct {
	*oauth.ClientSession
	wg   sync.WaitGroup
	mgrMu sync.Mutex
	mgr  *credential.Manager
}

// DelegationToken implements credential.Delegator by calling the host's
// getDelegationToken with this session's OAuth auth.
func (s *resumed) DelegationToken(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) (string, error) {
	lxm := syntax.NSID("network.habitat.space.getDelegationToken")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"/xrpc/network.habitat.space.getDelegationToken?space="+url.QueryEscape(space.String()), nil)
	if err != nil {
		return "", err
	}
	resp, err := s.DoWithAuth(s.Client, req, lxm)
	if err != nil {
		return "", fmt.Errorf("get delegation token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get delegation token: %s", resp.Status)
	}
	var out habitat.NetworkHabitatSpaceGetDelegationTokenOutput
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode delegation token: %w", err)
	}
	return out.Token, nil
}

// manager lazily builds the space-credential manager for this session.
func (s *resumed) manager() *credential.Manager {
	s.mgrMu.Lock()
	defer s.mgrMu.Unlock()
	if s.mgr == nil {
		s.mgr = credential.NewManager(s.Data.HostURL, s.Client, s)
	}
	return s.mgr
}

// credentialClient returns a client that reads from space's host as the space
// (via a space credential) instead of as this session's account.
func (s *resumed) credentialClient(space habitat_syntax.SpaceURI) *http.Client {
	return s.manager().ClientForSpace(space)
}

// dropCredential evicts the cached credential for space.
func (s *resumed) dropCredential(space habitat_syntax.SpaceURI) {
	s.manager().DropSpace(space)
}
```

`habitat_syntax` needs importing in `getter.go` if not present. `s.Data.HostURL` is the session's host (the Habitat instance that owns every space it can see).

In `pkg/sap/session/session.go`, refactor `ClientForSession` to share a `resumeFor` helper and switch `ClientForSpace` to credentials:

```go
// resumeFor loads and resumes the session for a DID.
func (s *Store) resumeFor(ctx context.Context, did syntax.DID) (*resumed, error) {
	var sess session
	if err := s.db.WithContext(ctx).First(&sess, "did = ?", did).Error; err != nil {
		return nil, fmt.Errorf("load session %s: %w", did, err)
	}
	return s.getter.resume(ctx, sess.DID, sess.SessionID)
}

// ClientForSession returns an HTTP client authenticated as the session's
// account against its host (OAuth).
func (s *Store) ClientForSession(ctx context.Context, did syntax.DID) (*http.Client, error) {
	resumed, err := s.resumeFor(ctx, did)
	if err != nil {
		return nil, err
	}
	return resumed.authClient(), nil
}
```

And in `ClientForSpace` (lines 135-144), the candidate loop becomes:

```go
	var errs []error
	for _, did := range candidates {
		resumed, err := s.resumeFor(ctx, did)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return resumed.credentialClient(space), nil
	}
	return nil, fmt.Errorf("no session with access to %s: %w", space, errors.Join(errs...))
```

Also update the doc comment on `ClientForSpace` (lines 107-109) to note it returns a credential-authed client. And extend `DropSpace` (lines 159-164) to evict credential caches:

```go
func (s *Store) DropSpace(ctx context.Context, space habitat_syntax.SpaceURI) error {
	s.getter.dropSpaceCredential(space)
	return s.db.WithContext(ctx).
		Where("space = ?", space).
		Delete(&spaceAccess{}).Error
}
```

Add to `getter.go`:

```go
// dropSpaceCredential evicts every resumed session's cached credential for a
// deleted space.
func (g *getter) dropSpaceCredential(space habitat_syntax.SpaceURI) {
	g.sessions.Range(func(_, v any) bool {
		if r, ok := v.(*resumed); ok {
			r.dropCredential(space)
		}
		return true
	})
}
```

- [ ] **Step 5: Run the credential unit tests**

Run: `go test ./pkg/sap/credential/ -count=1 && go test ./pkg/sap/session/ -count=1`
Expected: PASS. (`go test ./pkg/sap/...` will FAIL here until Step 6 updates `TestSap`'s host.)

- [ ] **Step 6: Mount the credential endpoints in the e2e host**

In `pkg/sap/sap_test.go`, `setupPear`, add the two credential endpoints to the mux so `ClientForSpace` can mint credentials against the stub host. Add after the existing `mux.HandleFunc` registrations (lines 283-287):

```go
	mux.HandleFunc("/xrpc/network.habitat.space.getDelegationToken",
		func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteJSON(r.Context(), w,
				habitat.NetworkHabitatSpaceGetDelegationTokenOutput{Token: "test-delegation"})
		})
	mux.HandleFunc("/xrpc/network.habitat.space.getSpaceCredential",
		func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteJSON(r.Context(), w,
				habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: "test-credential"})
		})
```

The stub host's auth is permissive (`NewSuccessMethodForOrg`), so the `test-credential` bearer is accepted on repo-host reads. Import `"github.com/habitat-network/habitat/internal/httpx"` in `sap_test.go`.

- [ ] **Step 7: Run all sap tests**

Run: `go test ./pkg/sap/... -count=1 && go test ./cmd/sap/ -count=1`
Expected: PASS — `TestSap` now exercises sap reading through a space credential (minted against the stub endpoints), with all 30 records landing in the outbox and 5 repos settling active.

- [ ] **Step 8: Commit**

```bash
git add pkg/sap/credential pkg/sap/session/getter.go pkg/sap/session/session.go pkg/sap/sap_test.go
git commit -m "feat(sap): authorize repo-host reads with space credentials (B2)"
```

---

## Final verification

Run the full check before calling the work done:

```bash
go build ./... && go vet ./pkg/sap/... ./internal/spaces/... ./internal/notify/...
go test ./internal/spaces/... ./internal/notify/... ./pkg/sap/... ./cmd/sap/... -count=1
golangci-lint run
moon docs-server:build
```

Expected: everything passes. Then `git log --oneline -10` should show the eight commits from Tasks 1-8.

## Out of scope (explicitly not in this plan)

- Blob sync (`blobList`, `blobStore`).
- OAuth scope work (the host advertises only `scopes_supported: ["atproto"]`).
- A full healing protocol beyond the existing desync → getRepo recovery path.
- Lexicon edits (the `RevNotFound` error name is a wire string, not code-generated; the `OpEntry.prev` field already exists in the generated Go bindings).
- Background credential renewal goroutines — renewal is synchronous and singleflight-guarded on demand.

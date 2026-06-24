# ReBAC Relationships — Backend Implementation Plan

> **For agentic workers:** Use the `go-tdd` skill for each new store method. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the backend for the `network.habitat.relationship.*` lexicons (already landed), letting apps manage access-control relationships as AT Protocol records owned by the org repo within the space they govern. OpenFGA stays an internal implementation detail; the app-facing surface is tuples, groups, and a fixed role vocabulary.

**Architecture:** A new `internal/relationship` package exposes a `Store` that mirrors relationship tuples into both (a) AT Protocol records in the governing space (via `internal/spaces`, written with `repo = space owner DID` so they are org-owned and app-readable) and (b) the `internal/fgastore` OpenFGA graph (for `Check`/expand). A single role-translation function is the only place app role names (`owner`/`manager`/`writer`/`reader`/`member`) map to FGA relation names. `internal/relationship/server.go` registers XRPC handlers in `cmd/pear/main.go` alongside the existing `space.*` routes.

**Tech Stack:** Go 1.26, OpenFGA (via `internal/fgastore`), GORM, AT Protocol (indigo), gorilla/mux.

**Role ↔ FGA relation mapping** (the only place OpenFGA names appear):

| App role  | FGA relation         | Object type |
|-----------|----------------------|-------------|
| `owner`   | `owner`              | space       |
| `manager` | `can_manage_members` | space       |
| `writer`  | `can_write`          | space       |
| `reader`  | `can_read`           | space       |
| `member`  | `member`             | group       |

**Subject encodings** (FGA `user` field; `<esc>` = `url.QueryEscape`):

| Subject (lexicon union)                  | FGA user string                       |
|------------------------------------------|---------------------------------------|
| `userSubject{did}`                       | `user:<esc did>`                      |
| `groupSubject{group}`                    | `group:<esc groupURI>#member`         |
| `spaceRoleSubject{space,role}`           | `space:<esc spaceURI>#<fga relation>` |
| `orgRoleSubject{org,role}`               | `organization:<esc orgDID>#<role>`    |

---

### Task 1: fgastore — extend the authorization model

**Files:** Modify `internal/fgastore/model.go`; test `internal/fgastore/fgastore_test.go`.

- [ ] **1.1** Add constants: `TypeGroup = "group"`, `RelationGroupMember = "member"`.
- [ ] **1.2** Add a `group` type definition with a single direct relation `member`.
- [ ] **1.3** Expand `DirectlyRelatedUserTypes` so usersets are accepted:
  - space relations (`owner`, `can_read`, `can_write`, `can_manage_members`) and `group.member` each accept: `{Type: user}`, `{Type: group, Relation: member}`, `{Type: space, Relation: <each space relation>}`, `{Type: organization, Relation: admin}`, `{Type: organization, Relation: member}`.
  - Use `&openfgav1.RelationReference{Type: ..., RelationOrWildcard: &openfgav1.RelationReference_Relation{Relation: ...}}` for userset references.
- [ ] **1.4** Keep the existing computed-union implications (`owner ⇒ manager ⇒ writer ⇒ reader`; `writer ⇒ reader`). The model stays **static** — fixed vocabulary, no dynamic-model machinery.
- [ ] **1.5** Tests: `Check` resolves (a) direct group membership, (b) nested groups, (c) cross-space role inheritance (`space:A#can_write` granted `can_write` on `space:B`), (d) whole-org assignment (`organization:O#member` granted `can_read` on a space; confirm org admins inherit via the existing `member` union).

### Task 2: fgastore — userset / group encoding helpers

**Files:** Modify `internal/fgastore/encoding.go`; test `internal/fgastore/encoding_test.go`.

- [ ] **2.1** Add `GroupObjectKey(uri habitat_syntax.SpaceRecordURI) string` → `group:<esc>`. (Group URIs are space-record URIs.)
- [ ] **2.2** Add userset encoders for the four subject kinds in the table above. Reuse the existing `MemberUserString` for `user:` and the existing organization-object encoding for the org case (mirror however `organization:` keys are already built for org membership tuples).
- [ ] **2.3** Add parse/inverse helpers as needed for `listTuples`/`listSubjects` round-tripping, plus round-trip tests.

### Task 3: relationship store

**Files:** Create `internal/relationship/relationship.go`, `internal/relationship/store.go`; test `internal/relationship/store_test.go`. Reuse `internal/spaces` (`PutRecord`/`ListRecords`/`DeleteRecord`, `ownerContextualTuple`), `internal/fgastore` (`Write`/`Delete`/`Check`/`ListUsers`/`ListObjects`/`Read`).

- [ ] **3.1** Define typed app-facing types: `Role` (typedef string with consts `RoleOwner/Manager/Writer/Reader/Member`), `Subject` (discriminated interface with `userSubject`/`groupSubject`/`spaceRoleSubject`/`orgRoleSubject` implementations, `isSubject()`), `Object` (interface for `spaceObject`/`groupObject`). Parse/serialize against the generated `interface{}` union fields (mirror `internal/permissions/grantee.go` `ParseGranteesFromInterface` / `ConstructInterfaceFromGrantees`).
- [ ] **3.2** `roleToFGARelation(role, objectType)` and inverse — the single translation point. Validate combinations (reject `member` on a space object and space roles on a group object → return a sentinel `ErrInvalidTuple`).
- [ ] **3.3** `governingSpace(object) SpaceURI` — for a space object, the space itself; for a group object, the space embedded in the group's space-record URI.
- [ ] **3.4** `WriteTuple(ctx, subject, relation, object)`: resolve governing space; `spaces.PutRecord(space, space.SpaceOwner(), "network.habitat.relationship.tuple", tid, value)` so the record is **org-owned**; then `fgastore.Write(encodedSubject, fgaRelation, encodedObject)`. Return the record URI. Idempotent on the FGA side.
- [ ] **3.5** `DeleteTuple(ctx, uri)`: read the tuple record, `fgastore.Delete(...)`, then `spaces.DeleteRecord(...)`.
- [ ] **3.6** `ListTuples(ctx, space, filters)`: `spaces.ListRecords` for the tuple collection in the space, decode, apply optional object/subject/relation filters.
- [ ] **3.7** `Check(ctx, did, role, space)` → bool via `fgastore.Check`.
- [ ] **3.8** `ListSubjects(ctx, space, role)` → `[]DID` via `fgastore.ListUsers` (decode `user:` results to DIDs); `ListObjects(ctx, did, role)` → `[]SpaceURI` via `fgastore.ListObjects`.
- [ ] **3.9** `CreateGroup(ctx, space, name, description)`: `spaces.PutRecord(space, space.SpaceOwner(), "network.habitat.relationship.group", tid, value)` → group URI. `ListGroups(ctx, space)` via `ListRecords`. `DeleteGroup(ctx, uri)`: delete the group record, its membership tuples (object = group), and tuples referencing it as a subject.
- [ ] **3.10** Note in code comments: `space.addMember`/`removeMember`/`getMembers` are special cases of write/delete/list tuple (subject=user, relation=writer|reader, object=space); consolidation is a future cleanup, not part of this task.

### Task 4: XRPC server + wiring

**Files:** Create `internal/relationship/server.go`; modify `cmd/pear/main.go` (route registration) and the server constructor that owns `spaces`/`fgastore`.

- [ ] **4.1** `Server` struct holding `Store`, `oauth`/`serviceAuth` `authn.Method`, and a `*schema.Decoder` for query params (mirror `internal/spaces/server.go`).
- [ ] **4.2** Handlers decode the generated `habitat.NetworkHabitatRelationship*` types, authenticate via `authn.Validate(w, r, oauth, serviceAuth)`, then authorize against the **governing space** using the same FGA space check pattern as `internal/spaces/server.go`: `RelationSpaceMemberManager` for writes (`writeTuple`/`deleteTuple`/`createGroup`/`deleteGroup`), `RelationSpaceReader` for reads (`listTuples`/`check`/`listSubjects`/`listObjects`/`listGroups`).
- [ ] **4.3** Map store sentinels to HTTP: `ErrInvalidTuple` → 400 `InvalidTuple`, missing space → 404 `SpaceNotFound`, unauthorized → 403.
- [ ] **4.4** Register routes in `cmd/pear/main.go`:
  ```
  /xrpc/network.habitat.relationship.writeTuple
  /xrpc/network.habitat.relationship.deleteTuple
  /xrpc/network.habitat.relationship.listTuples
  /xrpc/network.habitat.relationship.check
  /xrpc/network.habitat.relationship.listSubjects
  /xrpc/network.habitat.relationship.listObjects
  /xrpc/network.habitat.relationship.createGroup
  /xrpc/network.habitat.relationship.deleteGroup
  /xrpc/network.habitat.relationship.listGroups
  ```

### Task 5: Verify end-to-end

- [ ] **5.1** `go build ./...` and `go test ./internal/fgastore/... ./internal/relationship/...`.
- [ ] **5.2** `golangci-lint run` on the new package.
- [ ] **5.3** Walk the worked examples from the plan against a live `Check`: org members → reader, group → writer, nested group, and `spaceA writers ⇒ writers of spaceB`.
- [ ] **5.4** Confirm tuple/group records appear in the governing space under the org DID's repo (readable via `space.listRecords`), proving interoperable visibility to other apps.

### Notes / risks

- **Reserved collections:** like `network.habitat.clique`, the `tuple`/`group` collections should only be writable via these XRPC endpoints, never via the generic record-write path. Add them to whatever reserved-collection guard `clique` uses.
- **Org-owned writes:** `spaces.PutRecord` currently records `repo = caller`. Here we deliberately pass `repo = space.SpaceOwner()` (the org). Confirm `internal/spaces` allows an arbitrary `repo` argument (it does — `PutRecord(ctx, space, repo, ...)`); the authorization gate is the FGA space check in the handler, not the record's repo field.
- **Coverage:** `internal/relationship` is new and must meet the 70%/60% thresholds in `.testcoverage.yml`; `internal/pear` is excluded and must not be extended.

# ReBAC Relationships — Backend Implementation Plan

> **For agentic workers:** Use the `go-tdd` skill for each new store method. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the backend for the `network.habitat.relationship.*` lexicons (already landed), letting apps manage access-control relationships as AT Protocol records owned by the org repo within the space they govern. OpenFGA stays an internal implementation detail; the app-facing surface is tuples over spaces and a fixed role vocabulary.

**Groups are spaces, and so are orgs' member/admin sets.** There is no separate group concept in the auth model, and no separate org concept either. A "group" is a space of type `network.habitat.group` whose metadata lives in a `network.habitat.group.profile` record inside that space. Membership in a group is just a role on the group-space (any role implies membership; the bottom role `reader` captures "everyone in the group"). A group is used as a grantee elsewhere via a `spaceRoleSubject` that references the group-space (typically with `role: reader`). Group lifecycle reuses the existing `space.*` endpoints (`createSpace` with `type=network.habitat.group`, `deleteSpace`, `listSpaces?type=network.habitat.group`), and the profile record is written via the generic `space.putRecord`/`space.getRecord`.

An org's members and admins are modeled the same way — as group-spaces (e.g. a "members" group-space and an "admins" group-space per org, with the admins group-space nested as a member of the members group-space to preserve `admin ⇒ member`). So "all members/admins of an org" is referenced via `spaceRoleSubject` over those group-spaces, not a dedicated subject kind.

This removes the `group` FGA type, the `member` relation, the `organization` FGA type/userset wiring, and the `orgRoleSubject` lexicon def and `createGroup`/`deleteGroup`/`listGroups` XRPC — nothing is lost, because every former group/org-grantee capability maps onto space roles and the cross-space `spaceRoleSubject` userset. (The `organization` FGA type was already dormant: org membership is stored in the GORM-backed `internal/org` package, and nothing wrote org tuples into FGA, so `orgRoleSubject` never resolved to anything anyway.)

**Architecture:** A new `internal/relationship` package exposes a `Store` that mirrors relationship tuples into both (a) AT Protocol records in the governing space (via `internal/spaces`, written with `repo = space owner DID` so they are org-owned and app-readable) and (b) the `internal/fgastore` OpenFGA graph (for `Check`/expand). A single role-translation function is the only place app role names (`owner`/`manager`/`writer`/`reader`) map to FGA relation names. `internal/relationship/server.go` registers XRPC handlers in `cmd/pear/main.go` alongside the existing `space.*` routes.

**Tech Stack:** Go 1.26, OpenFGA (via `internal/fgastore`), GORM, AT Protocol (indigo), gorilla/mux.

**Role ↔ FGA relation mapping** (the only place OpenFGA names appear):

| App role  | FGA relation         | Object type |
|-----------|----------------------|-------------|
| `owner`   | `owner`              | space       |
| `manager` | `can_manage_members` | space       |
| `writer`  | `can_write`          | space       |
| `reader`  | `can_read`           | space       |

(A group-space is a space, so it uses the same relations; "group member" = `can_read` on the group-space.)

**Subject encodings** (FGA `user` field; `<esc>` = `url.QueryEscape`):

| Subject (lexicon union)                  | FGA user string                       |
|------------------------------------------|---------------------------------------|
| `userSubject{did}`                       | `user:<esc did>`                      |
| `spaceRoleSubject{space,role}`           | `space:<esc spaceURI>#<fga relation>` |

(A group used as a grantee — including an org's members/admins group-space — is just `spaceRoleSubject{space: <group-space URI>, role: reader}` → `space:<esc>#can_read`.)

---

### Task 1: fgastore — extend the authorization model

**Files:** Modify `internal/fgastore/model.go`; test `internal/fgastore/fgastore_test.go`.

- [ ] **1.1** Expand `DirectlyRelatedUserTypes` so usersets are accepted on the space relations (`owner`, `can_read`, `can_write`, `can_manage_members`). Each accepts: `{Type: user}` and `{Type: space, Relation: <each space relation>}`.
  - Use `&openfgav1.RelationReference{Type: ..., RelationOrWildcard: &openfgav1.RelationReference_Relation{Relation: ...}}` for userset references.
  - The cross-space userset references are what make groups-as-spaces work: a group-space granted a role on another space, and nested groups (a group-space granted a role on another group-space), both resolve through these. Org members/admins are group-spaces too, so they need no special casing.
- [ ] **1.2** Keep the existing computed-union implications (`owner ⇒ manager ⇒ writer ⇒ reader`; `writer ⇒ reader`). The model stays **static** — fixed vocabulary, no dynamic-model machinery. The model needs only `user` and `space` types: **no `group` type, no `member` relation** (groups are spaces) and **no `organization` type/userset wiring** (org member/admin sets are group-spaces). The dormant `organization` type can be removed from `model.go` as part of this task since nothing populates or references it.
- [ ] **1.3** Tests: `Check` resolves (a) direct membership of a group-space (user is `can_read` on the group-space), (b) nested groups (group-space A's readers granted `can_read` on group-space B), (c) cross-space role inheritance (`space:A#can_write` granted `can_write` on `space:B`), (d) whole-org assignment via a group-space (an org "members" group-space granted `can_read` on a space; confirm admins inherit by nesting the "admins" group-space inside the "members" group-space).

### Task 2: fgastore — userset encoding helpers

**Files:** Modify `internal/fgastore/encoding.go`; test `internal/fgastore/encoding_test.go`.

- [ ] **2.1** Add userset encoders for the two subject kinds in the table above. Reuse the existing `MemberUserString` for `user:`, and the existing `SpaceObjectKey` for the space portion of `spaceRoleSubject` (append `#<fga relation>`). There is no separate group or org key — a group (and an org's member/admin set) is a space, so it uses `SpaceObjectKey`.
- [ ] **2.2** Add parse/inverse helpers as needed for `listTuples`/`listSubjects` round-tripping, plus round-trip tests.

### Task 3: relationship store

**Files:** Create `internal/relationship/relationship.go`, `internal/relationship/store.go`; test `internal/relationship/store_test.go`. Reuse `internal/spaces` (`PutRecord`/`ListRecords`/`DeleteRecord`, `ownerContextualTuple`), `internal/fgastore` (`Write`/`Delete`/`Check`/`ListUsers`/`ListObjects`/`Read`).

- [ ] **3.1** Define typed app-facing types: `Role` (typedef string with consts `RoleOwner/Manager/Writer/Reader`), `Subject` (discriminated interface with `userSubject`/`spaceRoleSubject` implementations, `isSubject()`), `Object` (just a space). Parse/serialize against the generated `interface{}` union fields (mirror `internal/permissions/grantee.go` `ParseGranteesFromInterface` / `ConstructInterfaceFromGrantees`).
- [ ] **3.2** `roleToFGARelation(role)` and inverse — the single translation point. Validate the role is one of the four space roles → return a sentinel `ErrInvalidTuple` otherwise.
- [ ] **3.3** `governingSpace(object) SpaceURI` — the object is always a space, so this is the space itself.
- [ ] **3.4** `WriteTuple(ctx, subject, relation, object)`: resolve governing space; `spaces.PutRecord(space, space.SpaceOwner(), "network.habitat.relationship.tuple", tid, value)` so the record is **org-owned**; then `fgastore.Write(encodedSubject, fgaRelation, encodedObject)`. Return the record URI. Idempotent on the FGA side.
- [ ] **3.5** `DeleteTuple(ctx, uri)`: read the tuple record, `fgastore.Delete(...)`, then `spaces.DeleteRecord(...)`.
- [ ] **3.6** `ListTuples(ctx, space, filters)`: `spaces.ListRecords` for the tuple collection in the space, decode, apply optional object/subject/relation filters.
- [ ] **3.7** `Check(ctx, did, role, space)` → bool via `fgastore.Check`.
- [ ] **3.8** `ListSubjects(ctx, space, role)` → `[]DID` via `fgastore.ListUsers` (decode `user:` results to DIDs); `ListObjects(ctx, did, role)` → `[]SpaceURI` via `fgastore.ListObjects`.
- [ ] **3.9** Groups need no dedicated store methods. A group is created with `spaces.CreateSpace(type=network.habitat.group)` + `spaces.PutRecord(group, owner, "network.habitat.group.profile", "self", value)`; listed via `spaces.ListSpaces(type=network.habitat.group)`; deleted via `spaces.DeleteSpace` (which should cascade its tuples). Membership is `WriteTuple(userSubject, reader, spaceObject{group})`. Document this mapping in code comments so callers know not to reach for a group-specific API.
- [ ] **3.10** Note in code comments: `space.addMember`/`removeMember`/`getMembers` are special cases of write/delete/list tuple (subject=user, relation=writer|reader, object=space); consolidation is a future cleanup, not part of this task.

### Task 4: XRPC server + wiring

**Files:** Create `internal/relationship/server.go`; modify `cmd/pear/main.go` (route registration) and the server constructor that owns `spaces`/`fgastore`.

- [ ] **4.1** `Server` struct holding `Store`, `oauth`/`serviceAuth` `authn.Method`, and a `*schema.Decoder` for query params (mirror `internal/spaces/server.go`).
- [ ] **4.2** Handlers decode the generated `habitat.NetworkHabitatRelationship*` types, authenticate via `authn.Validate(w, r, oauth, serviceAuth)`, then authorize against the **object space** using the same FGA space check pattern as `internal/spaces/server.go`: `RelationSpaceMemberManager` for writes (`writeTuple`/`deleteTuple`), `RelationSpaceReader` for reads (`listTuples`/`check`/`listSubjects`/`listObjects`).
- [ ] **4.3** Map store sentinels to HTTP: `ErrInvalidTuple` → 400 `InvalidTuple`, missing space → 404 `SpaceNotFound`, unauthorized → 403.
- [ ] **4.4** Register routes in `cmd/pear/main.go`:
  ```
  /xrpc/network.habitat.relationship.writeTuple
  /xrpc/network.habitat.relationship.deleteTuple
  /xrpc/network.habitat.relationship.listTuples
  /xrpc/network.habitat.relationship.check
  /xrpc/network.habitat.relationship.listSubjects
  /xrpc/network.habitat.relationship.listObjects
  ```
  (Group lifecycle — create/list/delete — uses the existing `space.*` routes; no relationship-specific group endpoints.)

### Task 5: Verify end-to-end

- [ ] **5.1** `go build ./...` and `go test ./internal/fgastore/... ./internal/relationship/...`.
- [ ] **5.2** `golangci-lint run` on the new package.
- [ ] **5.3** Walk the worked examples from the plan against a live `Check`: an org members group-space → reader of a space, a group-space's readers → writer of another space, nested group-spaces (admins inside members), and `spaceA writers ⇒ writers of spaceB`.
- [ ] **5.4** Confirm tuple records appear in the governing space under the org DID's repo (readable via `space.listRecords`), and group-spaces appear via `space.listSpaces?type=network.habitat.group` with their profile readable via `space.getRecord` — proving interoperable visibility to other apps.

### Notes / risks

- **Reserved collections:** like `network.habitat.clique`, the `tuple` collection should only be writable via these XRPC endpoints, never via the generic record-write path. Add it to whatever reserved-collection guard `clique` uses. The `network.habitat.group.profile` record is an ordinary space record written via `space.putRecord` and is **not** reserved.
- **Org-owned writes:** `spaces.PutRecord` currently records `repo = caller`. Here we deliberately pass `repo = space.SpaceOwner()` (the org). Confirm `internal/spaces` allows an arbitrary `repo` argument (it does — `PutRecord(ctx, space, repo, ...)`); the authorization gate is the FGA space check in the handler, not the record's repo field.
- **Group-space ownership:** group-spaces are created through `space.createSpace`, so they follow normal space ownership/authz. Who manages a group's membership is the group-space's own managers — more granular than a shared governing space.
- **DeleteSpace cascade:** deleting a group-space must also remove tuples that reference it (as object or via `spaceRoleSubject`). Confirm/extend `space.deleteSpace` (or the relationship store) handles this so dangling FGA usersets don't linger.
- **Coverage:** `internal/relationship` is new and must meet the 70%/60% thresholds in `.testcoverage.yml`; `internal/pear` is excluded and must not be extended.

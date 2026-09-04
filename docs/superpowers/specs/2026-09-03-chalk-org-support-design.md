# Chalk org support

## Summary

Chalk (the collaborative doc-editing demo app) currently only ever operates
in "personal" mode: every doc lives in a space owned by the creating
member's own DID, and chalk has no notion of organizations. This adds
support for chalk to connect to — and create/list docs under — an
`opensocial` org, alongside the existing personal mode.

## Background: what already exists

- **Auth today**: chalk has no OAuth client of its own. `login.tsx` calls
  sap's `POST /session/add` (`cmd/sap/server.go:81`, `handleAddSession`)
  with `{handle, return_to}`; sap runs the atproto PDS OAuth dance and
  redirects the browser to `return_to?did=<resolved DID>`.
  `session.callback.tsx` reads that DID and sets chalk's own signed cookie
  session (`{did}`, no token/expiry — chalk never sees an access token).
  All later pear calls go through `SapClient.call()` →
  sap's `/proxy/<nsid>` with a `Habitat-Did: <did>` header; sap resumes
  that DID's tracked OAuth session and forwards the call.
- **Opensocial org sign-in** (built earlier this session,
  `internal/oauthserver/oauth_server.go`): when the identity being signed
  into is an opensocial org, pear's `/oauth/authorize` redirects to
  `/ui/login/opensocial` instead of starting a login provider directly. The
  page shows the org's profile and the requesting client's metadata; an
  admin enters their own handle, which pear resolves and checks for the
  `admin` role via `opensocial.Store.GetUserRoles` before completing PDS
  login and issuing an auth code for the org's own DID. On token exchange,
  `opensocialStore.GrantAppAccess(orgDID, clientID)` records that the
  client is now authorized for that org.
- **`handleAddSession` is generic**: it resolves `handle` via
  `syntax.ParseAtIdentifier`, which accepts a bare DID string, not just a
  real handle. An opensocial org's directory identity resolves (via its
  `#habitat` PDS service entry) straight to pear's own oauthserver, so
  calling `/session/add` with the org's DID as `handle` drives the
  opensocial admin sign-in flow above, unmodified. No sap code changes are
  needed to *start* an org connection.
- **`Atproto-Proxy` forwarding** (`internal/forwarding/service_proxy.go`):
  an XRPC request carrying `Atproto-Proxy: <did>#<serviceId>` is
  intercepted and forwarded to that DID's named service, authenticated via
  a service-auth JWT minted from the caller's own session — the caller
  doesn't need a session of their own with the target DID. sap's
  `/proxy/<nsid>` handler (`cmd/sap/server.go:365`) clones and forwards all
  incoming headers except a small deny-list (hop-by-hop, the auth headers
  it replaces), so a `Atproto-Proxy` header chalk sets on its outgoing call
  passes straight through untouched — no sap changes needed here either.

**Net result: this feature requires no Go/backend changes.** Everything
below is new/changed TypeScript in `typescript/apps/chalk/`.

## Data model changes

`typescript/apps/chalk/src/db/schema.ts` (Cloudflare D1 + Drizzle):

- `docs` gets a new `isOrg` column (`boolean`/`integer`, not null, default
  `false`): whether the doc's space is owned by an org (`true`) or a
  personal space (`false`). Existing rows backfill to `false` — no other
  data or env var is needed for the backfill, since `ownerDid` on those
  rows is already the creating member's own DID, exactly what personal
  mode means today.
- New `connectedOrgs` table: `(memberDid, orgDid, orgName, connectedAt)`,
  primary key `(memberDid, orgDid)`. Chalk's own record that a member has
  successfully connected an org (populated by the org-connect callback,
  below). This is distinct from — and doesn't replace — the live "orgs I'm
  a member of" list, which is always fetched fresh from pear.

`ownerDid` keeps its current meaning (the space's owning DID): for an org
doc this becomes the org's DID; for a personal doc it stays the creating
member's own DID, unchanged from today. There is no fixed "personal space
authority" — personal mode has no single shared owner DID, same as today.

## Session changes

Chalk's existing signed cookie session (`typescript/apps/chalk/src/server/session.ts`)
gets one more field: `currentOrg?: string` (an org DID). Absent/unset means
"Personal" mode. There's no per-org token stored in chalk's session — org
calls are always made as the member's own DID via sap, using
`Atproto-Proxy` (for space creation) or the member's already tracked org
sap session — see "Connecting an org" below for which spaces need which.

## Connecting an org

**New page**: an org-picker, mirroring
`frontend/src/routes/_requireAuth/opensocial/index.tsx`. A new chalk
server function calls `SapClient.call()` — scoped to the member's own DID —
running `network.habitat.space.listSpaces({type:
"community.opensocial.members"})`, the same query
`frontend/src/queries/opensocial.ts`'s `myOrgsQueryOptions` uses, giving
every org DID the member belongs to.

For each org, fetch its display name via the **same `Atproto-Proxy`
mechanism `createDoc` uses**, not frontend's delegation-token/space-
credential dance: `SapClient.call("network.habitat.space.getRecord", "GET",
{space: aboutSpaceUri, repo: orgDID, collection:
"community.opensocial.profile", rkey: "self"}, {atprotoProxy:
"${orgDID}#habitat"})`, still scoped to the member's own DID. This works
for the same reason `createDoc`'s org-mode call does: the about space's own
`community.opensocial.access` record already admits both `admin` and
`member` roles as readers (set when the org was created — see
`opensocial.Store.NewOrg`), so `CheckUserHasSpaceRole`
(`internal/perms/store.go:412`) grants any org member read access with no
separate credential exchange. No avatar — name only, for scope.

Selecting an org calls a chalk server function that POSTs to sap's
`/session/add` with `{handle: orgDID, return_to:
"<CHALK_BASE_URL>/session/org-callback"}`, then redirects the browser to
the returned `redirect_url`. This drives the opensocial admin sign-in flow:
the member enters their own handle again (proving they're an admin of that
org) and completes PDS login: pear issues an auth code, sap exchanges it
for a token, and pear's `HandleToken` records the app-access grant for
chalk's client ID against that org.

**New route** `/session/org-callback` (parallel to the existing
`session.callback.tsx`): reads `?did=<orgDID>` and calls a server function
that:
1. Fetches the org's profile via the same member-scoped, `Atproto-Proxy`'d
   `getRecord` call described above — not via a `Habitat-Did: <orgDID>`
   call resuming sap's newly-added org session. sap's session store is
   keyed only by DID (`pkg/sap/session/session.go:45`), so two different
   members connecting the same org would otherwise clobber each other's
   resumable session; routing every org read through the *calling
   member's own* session sidesteps that entirely, and is what confirms the
   flow actually worked (a member who wasn't really an admin never reaches
   this point, since `HandleOpensocial` — pear's side — already checked
   `admin` before completing PDS login).
2. On success: upserts `(memberDid, orgDid, orgName)` into
   `connectedOrgs`, and renders "Successfully approved Chalk with
   {orgName}" plus a button back home. That button sets `currentOrg` to
   this org DID in the session.
3. On failure: renders an error instead (no state is persisted).

## Using an org: doc creation and listing

**`createDoc`** (`typescript/apps/chalk/src/server/functions.ts`): the
`SapClient` stays scoped to the **member's own DID** in both modes — org
mode does not resume a separate org session for this call.

- **Org mode** (`session.currentOrg` set): call
  `community.opensocial.createSpace` with `{org: currentOrg, type:
  DOCS_SPACE_TYPE, roles: ["admin", "member"]}`, adding `Atproto-Proxy:
  <currentOrg>#habitat` to the request (a new optional parameter on
  `SapClient.call()`) — this satisfies the lexicon's service-auth
  requirement by proxying through the member's own session rather than
  needing sap to hold a separate org session for this call. `ownerDid` =
  `currentOrg`; `isOrg` = `true`.

  Passing both org roles (`opensocial.AdminRoleRkey`,
  `opensocial.MemberRoleRkey`) as `roles` means the space's own
  `community.opensocial.access` record admits every member of the org, not
  just admins — this is *the* access control for org docs (see "Org docs
  are visible org-wide" below); no per-user grant is made or needed.

- **Personal mode** (no `currentOrg`): unchanged —
  `network.habitat.simplespace.createSpace` with `{did, type:
  DOCS_SPACE_TYPE}`. `ownerDid` = the member's own DID; `isOrg` = `false`.
  `doc_access` still gets the acting member's own `owner` grant immediately
  (as today), since personal-mode sharing stays per-user.

## Org docs are visible org-wide (no fine-grained sharing yet)

Per your last note: any member of a connected org can see (and, for now,
edit) every doc owned by that org, regardless of who created it — there's
no per-doc, per-user sharing within an org yet, unlike personal mode's
existing share/revoke model.

This falls out of existing pear behavior with no new backend code:
`internal/perms/store.go:412` (`CheckUserHasSpaceRole`) already treats any
DID that passes the space's own `community.opensocial.access` record (i.e.
holds one of the `roles` the space was created with) as an implicit
reader/writer — this is the same check
`network.habitat.relationship.checkUserRelation` uses, which is what
chalk's existing `hasDocAccess`/`docRole` (`functions.server.ts`) and the
`ws.$docId.ts` route already call before serving a doc. So granting
`roles: ["admin", "member"]` at space-creation time (above) is sufficient
— **`hasDocAccess`, `docRole`, and the websocket route need no changes at
all** for org docs to be readable/writable org-wide.

Consequences for the rest of chalk:
- **`createDoc`** in org mode skips the local `upsertDocAccess` call
  entirely (see above) — there's no individual grant to mirror locally,
  since access isn't per-user.
- **`listDocs`** (`docsForAccessor` in `typescript/apps/chalk/src/db/index.ts`)
  branches by mode instead of always joining `doc_access`:
  - Personal mode: unchanged — the existing `doc_access` inner-join query,
    `WHERE isOrg = false` implied by every personal doc's `doc_access` row
    (org docs never get one).
  - Org mode: `SELECT * FROM docs WHERE isOrg = true AND ownerDid =
    currentOrg` — no `doc_access` join. Every doc owned by the org, not
    just ones the caller happens to have a local grant row for, and
    regardless of creator.
- **Sharing UI**: the `ShareDialog` (per-user editor/viewer grants) only
  makes sense in personal mode; it's hidden (or disabled with an
  explanatory message) when viewing an org doc, since `shareDoc`/
  `revokeDocAccess` grant individual relations that don't affect org docs'
  actual (org-wide) access at all.

## Error handling

- Org-picker load failure (can't reach pear/sap): render an error state on
  that page; doesn't affect existing personal-mode usage.
- `/session/add` failure when connecting an org: surfaced as an error
  before ever redirecting the browser (mirrors existing `login.tsx`
  handling for member login failures).
- Org-callback profile-fetch failure: rendered as an error on that page;
  nothing is written to `connectedOrgs`, and `currentOrg` is not set.
- `createDoc` in org mode failing (e.g. the member is no longer an admin,
  or the app-access grant was revoked) surfaces the proxy/service-auth
  error to the caller same as any other `SapClient.call()` failure today —
  no new error handling path needed.

## Testing

No Go changes, so no new Go tests. On the chalk (vitest) side:
- Unit tests for the new server functions: org-list parsing/rendering,
  org-connect callback (success and failure), and the session's new
  `currentOrg` field (get/set, default resolution).
- `createDoc`: a test per mode (personal vs org) asserting the right nsid,
  body (including `roles` in org mode), and (for org mode) `Atproto-Proxy`
  header are sent; that the right `ownerDid`/`isOrg` values land in the
  `docs` row; and that `upsertDocAccess` is called in personal mode but
  skipped in org mode.
- `docsForAccessor`: a test per mode — personal mode still requires a
  `doc_access` row to see a doc; org mode returns every doc for the org
  regardless of `doc_access` (including one from a different creator with
  no `doc_access` row at all).
- A migration/backfill assertion: existing rows come back with
  `isOrg = false` after migration.

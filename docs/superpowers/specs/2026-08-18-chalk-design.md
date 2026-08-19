# chalk: a TanStack Start collaborative docs app

## Summary

Replace `typescript/apps/docsv2` (React SPA) and `typescript/apps/docs-server`
(Hono BFF) with a single new app, `typescript/apps/chalk`, built on
[TanStack Start](https://tanstack.com/start/latest). chalk owns routing, SSR,
auth, and all data access — there is no separate frontend/backend split, and
no client-side atproto OAuth. Both `docsv2` and `docs-server` are deleted once
chalk replaces them.

This is a "BFF" app per the
[atproto OAuth app types guide](https://atproto.com/guides/about-oauth#types-of-app):
chalk itself is the atproto OAuth client, holding sessions server-side (via
`sap`) rather than issuing tokens to the browser.

## Why

- The current model has the frontend hold its own atproto OAuth session
  (DPoP tokens in the browser) and call docs-server's XRPC endpoints through
  pear's service-auth proxy. This is fragile (see the `http.NoBody` /
  audience bugs just fixed) and puts token handling in the browser
  unnecessarily.
- The current data model has one org-owned CRDT record per doc, written by
  sap acting as the org. There's no per-user attribution, and org membership
  is a layer of indirection the docs product doesn't need.
- Two apps (a Vite SPA + a Hono server) for one product surface is more
  moving parts than necessary; TanStack Start collapses that into one.

## Non-goals

- No change to `sap`'s core session/proxy/outbox machinery beyond the one
  addition described below.
- No change to `pear`'s space/permission model.
- No integration test suite for chalk (per direction: vitest + msw only,
  no testcontainers-style setup).
- No public XRPC surface for chalk's doc operations — see "API shape" below.

## Architecture overview

```
Browser (chalk UI, TanStack Router)
   │  fetch (server fn calls) + one held-open stream (subscribeDoc)
   ▼
chalk (Node process, TanStack Start)
   ├─ Route tree + SSR (doc list, doc editor, login page)
   ├─ Server functions: startLogin, sessionCallback, createDoc, listDocs,
   │  sendEdit, subscribeDoc
   ├─ Debounce queue (per docId+memberDid, 2s idle / 10s max)
   ├─ sap-outbox consumer (successor to docs-server's Crawler)
   ├─ In-process pub/sub (docId -> subscribers), sqlite (merged doc cache)
   └─ session (member DID), via @tanstack/react-start/server's built-in
      useSession (encrypted, httpOnly cookie) per the TanStack Start
      authentication guide
   │
   │  sap's /proxy/<nsid> (Habitat-Did + Habitat-Session headers)
   ▼
sap (unchanged proxy/session/outbox; + return_to on the OAuth callback)
   │
   ▼
pear (space records, blobs) — one repo per member
```

## Auth: per-member BFF login via sap

sap's `pkg/sap/session` already tracks one resumable OAuth session per
arbitrary DID (org DID today) with no special-casing — `Store.Add(did,
sessionID)` and `oauthClient.ResumeSession(did, sessionID)` don't care whose
DID it is. This is reused as-is for individual members; no sap data-model
change needed.

**Flow:**

1. `GET /login` (chalk route) — handle-entry form.
2. `startLogin(handle)` (chalk server function) — calls sap's internal
   session-start endpoint (existing `/org/add` handler, generalized in
   naming only) with `{handle, return_to: "<chalk-base-url>/session/callback"}`,
   then `throw redirect(...)` the browser to the returned PDS-authorize URL.
3. Browser completes PDS OAuth; PDS redirects to **sap's** public
   `/oauth-callback` (sap is the registered OAuth client — this doesn't
   change). Sap calls `AddSession(did, sessionID)` as today.
4. **sap change:** `handleOAuthCallback` currently responds with a bare
   `200 OK` and nothing else. It gains support for the `return_to` passed
   through step 2 (carried via the OAuth `state`) and 303-redirects the
   browser to `<return_to>?did=<did>` after `AddSession` succeeds.
5. `GET /session/callback` (chalk route) — reads `did` from the query,
   confirms sap has a live session for it (a cheap sap call), and writes it
   via `useAppSession()` (chalk's wrapper around TanStack Start's
   `useSession`, per the
   [authentication guide's session-management step](https://tanstack.com/start/latest/docs/framework/react/guide/authentication#2-session-management)):
   `const session = await useAppSession(); await session.update({ did })`.
   `useAppSession` is configured once with a `name`, a `password` (32+ char
   encryption key from env, alongside chalk's other secrets), and
   `cookie: { secure, sameSite: "lax", httpOnly: true, maxAge }` — the
   session data itself is encrypted client-side inside the cookie, no server
   session store needed. Redirects to `/`.
6. Every chalk server function that needs a caller calls `useAppSession()`
   and reads `session.data.did`. No `did` → `redirect` to `/login` (page
   routes) or throw an auth error (server functions called from an
   already-loaded page, e.g. an expired session mid-edit). Logout calls
   `session.clear()`.
7. chalk's calls to pear (via sap's `/proxy/<nsid>`) send
   `Habitat-Did: <memberDid>` and `Habitat-Session: <sessionID>` — the
   member's own identity, not an org's. (Today's docs-server never sent
   `Habitat-Session`; sap's proxy handler requires it, so this was already
   an incomplete path — chalk fixes it by threading the session ID from the
   cookie.)

No client-side OAuth library (`openid-client`, DPoP, `authManager.ts`) is
needed anywhere in chalk.

## Ownership & sharing (no org notion)

- A doc's space is created owned by its **creating member's own DID**
  (`pear.createSpace(memberDid)`), not an org.
- Sharing is a direct `network.habitat.relationship.writeUserRelation` grant
  from the owner to another member DID (`owner`/`manager`/`writer`/`reader`),
  exactly as today's `grantRole`, minus the org indirection.
- `OrgDirectory`, `orgFor`, `listOrgMembers`, and the
  `network.habitat.organization`-space handling in the crawler/outbox
  consumer are all deleted — chalk has no notion of orgs anywhere.

## Data model

- `network.habitat.docs.markdown` (canonical, rendered) and
  `network.habitat.docs.crdt` (canonical, merged CRDT snapshot) continue to
  exist at `self` under the **doc owner's** repo, exactly as today — this is
  what a reader loads.
- **New:** each editing member's in-flight delta is written to
  `network.habitat.docs.crdt` under **their own repo** within the same space
  (`at://<owner>/space/network.habitat.docs/<skey>/<memberDid>/network.habitat.docs.crdt/self`),
  as `{blob: <ref>, updatedAt}` — the blob is the member's Yjs update,
  uploaded via `network.habitat.repo.uploadBlob` and referenced from the
  record, replacing the inline base64 field (today's `// TODO this should be
  a blob` in `docCrdtStore.ts`). `space.putRecord`'s `repo` field already
  requires "the DID of the repo to write to (the authenticated member)" —
  this per-member write pattern is already how the space/record model
  works, chalk is just the first thing to use it this way.

## Write path (client → pear)

1. The editor sends local Yjs updates to `sendEdit(docId, update)` (a normal
   mutation server function, not a stream) as they happen.
2. chalk appends to an in-memory per-`(docId, memberDid)` debounce queue:
   flush 2s after the last edit if idle, but never let more than 10s of
   edits go unpersisted.
3. On flush: `uploadBlob` the accumulated Yjs update, `putRecord` the
   member's `crdt` record to point at it (authenticated as that member via
   sap, per the auth section above).

## Read/merge path (pear → clients)

1. chalk's sap-outbox consumer (successor to `Crawler`, same
   subscribe-and-ack-by-id pattern against sap's `/channel` websocket)
   observes every member's `crdt` record write across every doc space it
   knows about.
2. On each event: merge the delta into that doc's in-memory `Y.Doc` (Yjs
   merges are commutative — no ordering coordination needed across members),
   persist the merged state to chalk's sqlite, publish to the doc's
   in-process pub/sub topic, and debounce-write the re-rendered canonical
   markdown + merged CRDT snapshot back to pear under the **doc owner's**
   repo (same debounce policy as the write path, keyed by docId).
3. `subscribeDoc(docId)` — an async-generator server function
   ([TanStack Start streaming](https://tanstack.com/start/latest/docs/framework/react/guide/streaming-data-from-server-functions))
   — yields the doc's current merged state immediately on subscribe (so a
   fresh page load doesn't wait for the next edit), then yields again on
   every subsequent publish to that doc's topic, until the client
   disconnects.

## API shape

No public XRPC routes for doc operations. `createDoc`, `listDocs`,
`sendEdit`, `subscribeDoc` are plain TanStack server functions called
directly by the chalk UI — there is no other client to interop with, and the
two-app XRPC-proxy design this superseded existed only because docsv2 and
docs-server were separate processes.

The `network.habitat.docs.markdown`/`.crdt` **record collections** remain as
they are today (still what's written into spaces, still lexicon-defined) —
only the transport between chalk's UI and its own server changes.

## Testing

- Go changes (sap's `return_to` addition) get standard table-driven tests
  per the `go-tdd` skill.
- chalk uses **vitest**. No integration-test setup (no testcontainers-style
  live pear+sap harness) — unit-test the debounce queue, the merge logic,
  and server-function handlers in isolation, mocking pear/sap HTTP calls
  with **msw** where needed.

## Open items for the implementation plan

- Exact scaffold for a TanStack Start app in this monorepo: the existing
  `templates/web-app` Moon generator is Vite-SPA-only and doesn't fit
  (no SSR/server entry). chalk will be scaffolded following TanStack
  Start's own guide and wired into Moon by hand (`moon.yml`,
  `package.json` scripts) rather than via that generator.
- Exact shape of sap's generalized session-start endpoint (rename
  `/org/add` or add an alias) and how `return_to` threads through
  `StartAuthFlow`'s `state` — left to the plan/implementation, not
  fully speced here.
- Migration of any existing docsv2/docs-server data (doc records already
  written under the old org-owned model) is out of scope for this design;
  raise separately if needed before cutover.

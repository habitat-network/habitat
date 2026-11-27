# Habitat app

One app for working in a Habitat group. It combines and replaces three
earlier apps:

- **chalk**: collaborative docs (tiptap + Yjs over a Durable Object), with
  comment threads and sharing.
- **drop**: a shared bucket of uploaded files.
- **frontend**: the group's management plane: members and invites, roles
  and permissions, MCP servers, and the member's own connections.

Everything is scoped to a group, under `/g/$org/...`.

## Pages

| Sidebar section | Page                                                       | Route                 |
| --------------- | ---------------------------------------------------------- | --------------------- |
| Workspace       | Files (docs and uploads, list or grid, details panel)      | `/g/$org/files`       |
|                 | Doc editor                                                 | `/g/$org/docs/$docId` |
|                 | Search (group records plus connected MCP servers)          | `/g/$org/search`      |
|                 | Apps (deploy code, run it at its own URL)                  | `/g/$org/apps`        |
| Manage          | Members (roster, invites)                                  | `/g/$org/members`     |
|                 | Roles & access                                             | `/g/$org/roles`       |
|                 | MCP servers                                                | `/g/$org/mcp`         |
|                 | Group settings (profile, your profile in the group, leave) | `/g/$org/settings`    |
| You             | Connections (account, groups, authorized apps)             | `/connections`        |
|                 | Join a group (invites, look up a group)                    | `/join`               |
|                 | Create a group                                             | `/groups/new`         |

The sidebar shell is `src/components/shell/AppSidebar.tsx`; pages use the
`internal` design system (`internal/components/ui`).

## How it talks to Habitat

The app is a public atproto OAuth client of a Habitat instance (pear).

- **Members** sign in at `/login` with their handle or work email. The
  OAuth session (tokens + DPoP key) is sealed with `APP_CREDENTIALS_KEY` and
  stored in D1, and an encrypted `app_session` cookie names the member's
  DID and caches their groups.
- **Groups** are connected by an admin, who runs the same OAuth flow with
  the group's DID (pear shows its admin approval page). The app keeps the
  group's own session too, which docs and files need: their spaces are
  owned by the group. Until a group is connected its pages say so.
- **Management calls** (`org.atgrp.*`, `network.habitat.mcp.*`)
  are made as the signed-in member and proxied to the group's Habitat
  instance (`atproto-proxy: <org>#habitat`), so pear decides what each
  member may do. Pages only hide controls the member's roles don't grant.
- **Sync**: docs, comments, files and rosters are indexed into D1 from each
  connected group's `network.habitat.space.subscribe` stream, opened with
  the group's own session (`src/server/stream/`, using
  `@habitat-network/jetstream`), with one cursor per group. Each group has
  one `OrgStream` Durable Object, so there is exactly one connection per
  group however many isolates serve requests. It has no alarm or cron: a
  request made against a group (`requireOrg`) calls `follow()`, which
  catches D1 up over `getSnapshot` from the saved cursor, then keeps the
  live WebSocket open. Concurrent calls share one catch-up. After five
  minutes without a request the object closes the stream, and the next
  request catches up again.
  Writes that must show at once (uploads, file deletes, removing a member)
  are made by the caller through the typed `Habitat` service as the writer,
  then wait on the group's object (`awaitRecord`) until their event is
  indexed.
  Blobs (a doc's crdt state, uploaded files, an app's code) are read with
  `network.habitat.space.getBlob` as the group (`src/server/blobs.ts`). The app
  talks to pear only through the stream, `createSpace`, record writes and
  `getBlob`.

## Apps

Members deploy code to their group from the Apps page, and it runs at
`https://<slug>-<key>.<APP_RUN_DOMAIN>/` in a
[Dynamic Worker](https://developers.cloudflare.com/dynamic-workers/)
(the `LOADER` binding; Workers Paid plan only).

- **Records**: each app is its own group space (type
  `network.habitat.apps`, readable by members) holding one
  `network.habitat.apps.app` record written by the group: name, slug,
  entrypoint, version, compatibility date, who deployed it, and the code
  bundle as a blob (`{"modules": {<path>: <source>}}`). Deploying a new
  version uploads a new blob and rewrites the record, through the group's
  stream, so it's live when the call returns. The `apps` table is the
  stream's mirror of those records (`src/server/apps/sink.ts`).
- **Running** (`src/server/apps/runner.ts`): `entry.ts` sends every
  request for the run domain to the runner, never to the Habitat app. The
  runner looks the app up by its host label, and starts its isolate with
  `LOADER.get(<record uri>@<record cid>, ...)`, reading the bundle from
  pear only on a cold start.
- **Sandbox**: no network (`globalOutbound: null`), no bindings except
  `env.HABITAT` (the app's name, version, group and URL), and a CPU limit
  per request. Each app has its own origin. Uploads are plain JS modules
  plus JSON and text files; TypeScript and npm dependencies must be bundled
  first.

## Server code and Effect

Server functions live in `src/server/fns/` (`auth`, `groups`, `manage`,
`docs`, `files`, `mcp`). Each one is an `Effect` run by `runEffect`
(`src/server/effect/run.ts`), which gives every call:

- **Services** (`src/server/effect/services.ts`): `AppEnv`, `Database`,
  `OAuth`, and `Habitat` (xrpc calls as a DID, optionally proxied to a
  group). Promise code from chalk and drop is wrapped with `attempt`.
- **Typed errors** (`src/server/effect/errors.ts`): `NotSignedIn`,
  `SessionExpired`, `HabitatError`, `NotAMember`, `OrgNotConnected`,
  `Forbidden`, `NotFound`, `BadRequest`. `runEffect` turns them into a
  redirect to `/login`, a cleared session when the member's own session
  expired, or an error with a message safe to show. Defects are logged with
  their full cause and shown as a generic failure.
- **Logging and tracing**: every call is a span and log span named after
  the function, annotated with the member's DID; expected failures log at
  debug or warning, unexpected ones at error. Logs are JSON in production
  (Workers observability) and pretty in dev.

Server routes (`/api/upload`, `/api/download`, `/ws/$docId`) use
`runHandler`, which answers with a JSON error and status instead of
throwing; background work uses `runBackground`.

## Local setup

Create `.dev.vars` with

```
APP_SESSION_SECRET=<32+ characters>
APP_CREDENTIALS_KEY=<openssl rand -base64 32>
APP_BASE_URL=https://app.local.habitat.network
APP_HABITAT_URL=https://pear.local.habitat.network
APP_RUN_DOMAIN=apps.local.habitat.network
```

then `moon run app:dev` (port 5181; it applies D1 migrations locally
first). After changing `src/db/schema.ts`, run `pnpm db:generate`.

## Deploying

Create the D1 database (`wrangler d1 create app`, paste its id into
`wrangler.jsonc`), `wrangler secret put` the two secrets, apply migrations
(`wrangler d1 migrations apply app --remote`), then `pnpm deploy`.
`APP_BASE_URL`, `APP_HABITAT_URL` and `APP_RUN_DOMAIN` are `vars` in
`wrangler.jsonc`. Apps need a wildcard DNS record and a Workers route for
`*.<APP_RUN_DOMAIN>/*` pointing at this Worker.
Optional: `VITE_POSTHOG_KEY` and `VITE_POSTHOG_HOST` for analytics.

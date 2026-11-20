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
- **Management calls** (`community.opensocial.*`, `network.habitat.mcp.*`)
  are made as the signed-in member and proxied to the group's Habitat
  instance (`atproto-proxy: <org>#habitat`), so pear decides what each
  member may do. Pages only hide controls the member's roles don't grant.
- **Sync**: docs, comments and files are mirrored into D1 by the space
  syncer (`src/server/sync.ts`), driven by space notifications at
  `/xrpc/...` and a once-a-minute cron.

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
throwing; the cron uses `runBackground`.

## Local setup

Create `.dev.vars` with

```
APP_SESSION_SECRET=<32+ characters>
APP_CREDENTIALS_KEY=<openssl rand -base64 32>
APP_BASE_URL=https://app.local.habitat.network
APP_HABITAT_URL=https://pear.local.habitat.network
```

then `moon run app:dev` (port 5181; it applies D1 migrations locally
first). After changing `src/db/schema.ts`, run `pnpm db:generate`.

## Deploying

Create the D1 database (`wrangler d1 create app`, paste its id into
`wrangler.jsonc`), `wrangler secret put` the two secrets, apply migrations
(`wrangler d1 migrations apply app --remote`), then `pnpm deploy`.
`APP_BASE_URL` and `APP_HABITAT_URL` are `vars` in `wrangler.jsonc`.
Optional: `VITE_POSTHOG_KEY` and `VITE_POSTHOG_HOST` for analytics.

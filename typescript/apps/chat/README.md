# Chat

Channels and messages for an org, and the example app for the Habitat app
framework in `@habitat-network/habitat`.

Each channel is its own org space (type `network.habitat.chat`), open to the
roles on its `community.opensocial.access` record. The org writes one
`network.habitat.chat.channel` record that names the channel; members write
`network.habitat.chat.message` records into their own repos in the channel's
space, as themselves.

## What the framework does here

The whole app-specific data layer is two files:

- `src/habitat/app.ts` declares the slice: the space type, the two
  collections, and one index (a channel's messages by time).
- `src/habitat/functions.ts` holds the queries and mutations
  (`listChannels`, `listMessages`, `sendMessage`, `deleteMessage`).

Everything else comes from the framework:

- **Storage.** Synced records land in one generic `hab_records` table as
  JSON. The declared index is a partial expression index over that JSON, built
  by the runtime on first use, so changing indexes never needs a re-sync.
- **Permissions.** The framework folds the org's membership records and each
  channel's access record, which arrive on the same sync stream, into ACL
  tables. Every read through `ctx.db` is restricted to the spaces the viewer
  can read, so none of Chat's functions check membership themselves, and a
  role change takes effect as soon as it syncs.
- **Writes.** `ctx.spaces.put` writes to the Habitat instance as the signed-in
  member, where the channel's policy is enforced, then writes the record
  through to the slice so the request reads its own write.

`test/chat.test.ts` covers the permission behavior against a local D1.

## How it fits together

- **OAuth** (`src/server/oauth.ts`): the same as Drop. Chat is its own atproto
  OAuth client of the Habitat instance. Members sign in with their handle; an
  org admin connects an org by running the flow with the org's DID. Sessions
  live in D1, sealed with `CHAT_CREDENTIALS_KEY`.
- **Sync** (`src/server/sync.ts`): the same request-scoped `SpaceSyncer` as
  Drop, with the framework runtime's sink in place of a hand-written one.
  Connecting an org starts syncing its members space and every channel; the
  cron trigger runs due passes and picks up channels created elsewhere.
- **Channels** are created with the org's own session, so any member of a
  connected org can create one (the same trust Drop gives uploads). New
  channels are open to the `member` role.
- **Live updates**: the page polls the slice's version counter every two
  seconds and refetches when it moves. There is no push yet.

## Deploy to Cloudflare Workers

Same pipeline as Drop. `wrangler.jsonc` has a placeholder `database_id`:

```bash
pnpm exec wrangler d1 create chat          # put the id in wrangler.jsonc
pnpm exec wrangler d1 migrations apply chat --remote

pnpm exec wrangler secret put CHAT_SESSION_SECRET   # 32+ characters
pnpm exec wrangler secret put CHAT_CREDENTIALS_KEY  # openssl rand -base64 32

pnpm build
pnpm exec wrangler deploy
```

`CHAT_BASE_URL` must be where the Worker is actually served: it's the OAuth
client ID's origin and Chat's `did:web` service DID.

The drizzle migrations only cover OAuth and sync state. The `hab_*` slice
tables are created by the framework at runtime.

## Local development

`moon chat:dev` starts pear, Caddy, and the Worker on port 5180
(`https://chat.local.habitat.network`). Create `.dev.vars` first:

```
CHAT_SESSION_SECRET=dev-only-32-char-minimum-secret!!
CHAT_CREDENTIALS_KEY=<openssl rand -base64 32>
CHAT_BASE_URL=https://chat.local.habitat.network
CHAT_HABITAT_URL=https://pear.local.habitat.network
```

Regenerate the D1 migration after changing `src/db/schema.ts` with
`pnpm db:generate`, and `worker-configuration.d.ts` after a binding change
with `pnpm cf-typegen` (also run by `dev`/`build`/`test`).

## Analytics (PostHog)

Optional. Set `VITE_POSTHOG_KEY` (and `VITE_POSTHOG_HOST` for EU/self-hosted;
defaults to `https://us.i.posthog.com`) at build time. Unset, PostHog is a no-op.

# Drop

A shared file bucket for an org. Members sign in, pick an org, and drag
files anywhere onto the page. Each file becomes its own org space (type
`network.habitat.drop`, readable by the org's `member` role) holding one
`network.habitat.drop.file` record that names the file and references its
blob, so pear's search indexer can crawl it.

The file list is the output of `internal/spaceSync`: Drop is a consumer of
the spaces sync protocol, not a reader of its own writes.

## How it fits together

- **OAuth** (`src/server/oauth.ts`): Drop is its own atproto OAuth client of
  the Habitat instance (no sap). Members sign in with their handle. An org
  admin _connects_ an org by running the same flow with the org's DID, which
  pear routes through its opensocial admin approval. Sessions for members and
  orgs live in D1 (`oauth_sessions`), sealed with `DROP_CREDENTIALS_KEY`.
- **Sync** (`src/server/sync.ts`): there's no long-lived process. Each
  upload, org connect, inbound space notification, and download builds a
  `SpaceSyncer` for that request only and disposes it once its passes are
  done (in `waitUntil`). A cron trigger (every minute) runs whatever has come
  due in D1. All of the syncer's state lives in D1, so each fresh syncer
  picks up where the last left off.
- **Stores** (`src/db/`): drizzle/D1 implementations of the syncer's
  `SyncStore` (`syncStore.ts`) and of the OAuth client's state and session
  stores (`oauthStores.ts`).
- **Sink** (`src/server/sink.ts`): applies verified batches to the `files`
  table. Blobs aren't copied: downloads fetch them from the org's space on
  demand.
- **Live updates**: the file list polls every 10 seconds.

Uploads are capped at 500 KiB, which is pear's `uploadBlob` limit.

## Deploy to Cloudflare Workers

Same pipeline as chalk: `@cloudflare/vite-plugin` builds the Worker, and
`wrangler.jsonc`'s top-level `vars` are the deployed values (local dev
overrides them in `.dev.vars`). See chalk's README for why.

```bash
pnpm exec wrangler d1 create drop          # put the id in wrangler.jsonc
pnpm exec wrangler d1 migrations apply drop --remote

pnpm exec wrangler secret put DROP_SESSION_SECRET   # 32+ characters
pnpm exec wrangler secret put DROP_CREDENTIALS_KEY  # openssl rand -base64 32

pnpm build
pnpm exec wrangler deploy
```

`DROP_BASE_URL` must be where the Worker is actually served: it's the OAuth
client ID's origin and Drop's `did:web` service DID.

## Local development

`moon drop:dev` starts pear, Caddy, and the Worker on port 5178
(`https://drop.local.habitat.network`). Create `.dev.vars` first:

```
DROP_SESSION_SECRET=dev-only-32-char-minimum-secret!!
DROP_CREDENTIALS_KEY=<openssl rand -base64 32>
DROP_BASE_URL=https://drop.local.habitat.network
DROP_HABITAT_URL=https://pear.local.habitat.network
```

Regenerate the D1 migration after changing `src/db/schema.ts` with
`pnpm db:generate`, and `worker-configuration.d.ts` after a binding change
with `pnpm cf-typegen` (also run by `dev`/`build`/`test`).

# Brain

A chat app over an org's data. Members sign in with their handle and chat with
an assistant whose one tool, `search`, runs
`network.habitat.space.searchRecords` as the member, so it only ever sees what
they can read.

## How it fits together

- **A chat is a space.** Each chat session is its own space (type
  `network.habitat.brain`) owned by the member, created through
  `network.habitat.simplespace.createSpace`. Nothing is shared. The space holds
  one `network.habitat.brain.session` record (key `self`, the chat's title,
  taken from its first message) and one `network.habitat.brain.message` record
  per message (`role`, `content`, `createdAt`). Only text is stored; tool calls
  the assistant made are not.
- **Chat** (`src/routes/api.chat.ts`): the UI is TanStack AI's `useChat`
  against this route, with the chat's space URI as the `threadId`. The route
  writes the user's message to the space, streams the reply from the model,
  and writes the reply when it finishes. The model is Anthropic's
  `claude-sonnet-5-5` through `@tanstack/ai-anthropic`.
- **Search tool** (`src/server/search.ts`): `search({ query, org? })` over the
  records in one of the member's orgs. The system prompt lists the member's
  orgs; `org` can be left out when they belong to just one.
- **Sync** (`src/server/sync.ts`): the chat list and a chat's history come from
  `internal/spaceSync`, not from what the chat route wrote. Same shape as Drop:
  no long-lived process, a `SpaceSyncer` per request, state in D1
  (`DrizzleSyncStore`), and a cron trigger that runs whatever has come due.
- **Sink** (`src/server/sink.ts`): applies verified batches to the `chats` and
  `messages` tables.
- **OAuth** (`src/server/oauth.ts`): Brain is its own atproto OAuth client of
  the Habitat instance (no sap). Member sessions live in D1, sealed with
  `BRAIN_CREDENTIALS_KEY`, and the syncer mints its delegation tokens from them.

## Deploy to Cloudflare Workers

Same pipeline as Drop and Chalk (see chalk's README for why `wrangler.jsonc`'s
top-level `vars` are the deployed values).

```bash
pnpm exec wrangler d1 create brain         # put the id in wrangler.jsonc
pnpm exec wrangler d1 migrations apply brain --remote

pnpm exec wrangler secret put BRAIN_SESSION_SECRET   # 32+ characters
pnpm exec wrangler secret put BRAIN_CREDENTIALS_KEY  # openssl rand -base64 32
pnpm exec wrangler secret put ANTHROPIC_API_KEY

pnpm build
pnpm exec wrangler deploy
```

`BRAIN_BASE_URL` must be where the Worker is actually served: it's the OAuth
client ID's origin and Brain's `did:web` service DID.

## Local development

`moon brain:dev` starts pear, Caddy, and the Worker on port 5179
(`https://brain.local.habitat.network`). Create `.dev.vars` first:

```
BRAIN_SESSION_SECRET=dev-only-32-char-minimum-secret!!
BRAIN_CREDENTIALS_KEY=<openssl rand -base64 32>
BRAIN_BASE_URL=https://brain.local.habitat.network
BRAIN_HABITAT_URL=https://pear.local.habitat.network
ANTHROPIC_API_KEY=<your key>
```

Regenerate the D1 migration after changing `src/db/schema.ts` with
`pnpm db:generate`.

## Analytics (PostHog)

Optional. Set `VITE_POSTHOG_KEY` (and `VITE_POSTHOG_HOST`) at build time.

# Brain

A chat app over an org's data. Members sign in with their handle and chat with
an assistant whose one tool, `search`, runs
`network.habitat.space.searchRecords` as the member, so it only ever sees what
they can read.

## How it fits together

- **Chats live in D1.** The `chats` table has one row per chat (owned by the
  member who started it, titled from its first message) and `messages` holds
  each message's `role` and `content`. Only text is stored; tool calls the
  assistant made are not. A new chat is just a fresh UUID in the URL: its row is
  created with its first message.
- **Chat** (`src/routes/api.chat.ts`): the UI is TanStack AI's `useChat`
  against this route, with the chat's id as the `threadId`. The route
  writes the user's message to D1, streams the reply from the model, and
  writes the reply when it finishes. The model is Anthropic's
  `claude-sonnet-5-5` through `@tanstack/ai-anthropic`.
- **Search tool** (`src/server/search.ts`): `search({ query, org? })` over the
  records in one of the member's orgs. The system prompt lists the member's
  orgs; `org` can be left out when they belong to just one.
- **OAuth** (`src/server/oauth.ts`): Brain is its own atproto OAuth client of
  the Habitat instance (no sap). Member sessions live in D1, sealed with
  `BRAIN_CREDENTIALS_KEY`; the chat route restores them to list the member's orgs
  and search as them.

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

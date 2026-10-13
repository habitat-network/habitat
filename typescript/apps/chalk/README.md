Welcome to your new TanStack Start app!

# Getting Started

To run this application:

```bash
pnpm install
pnpm dev
```

# Building For Production

To build this application for production:

```bash
pnpm build
```

## Styling

This project uses [Tailwind CSS](https://tailwindcss.com/) for styling.

### Removing Tailwind CSS

If you prefer not to use Tailwind CSS:

1. Remove the demo pages in `src/routes/demo/`
2. Replace the Tailwind import in `src/styles.css` with your own styles
3. Remove `tailwindcss()` from the plugins array in `vite.config.ts`
4. Remove `@tailwindcss/vite` and `tailwindcss` from `package.json`

## Deploy to Cloudflare Workers

Chalk builds through `@cloudflare/vite-plugin` (not Nitro): a stateless
Worker serving routes, SSR, and server functions, with one Durable Object
class — `DocRoom` (one per document, owns its Yjs state) — plus a D1
database for the docs index.

Chalk talks to the Habitat instance directly, the same way Drop does — no
sap in between:

- **OAuth** (`src/server/oauth.ts`): chalk is its own atproto OAuth client.
  Members sign in with their handle or work email; connecting an org runs
  the same flow with the org's DID, which pear routes through its
  opensocial admin approval. Sessions for members and orgs live in D1
  (`oauth_sessions`), sealed with `CHALK_CREDENTIALS_KEY`, and every call
  chalk makes as someone goes through `HabitatClient`
  (`src/server/habitatClient.ts`).
- **Sync** (`src/server/sync.ts`): doc spaces and their comments spaces are
  synced with `internal/spaceSync`. Creating a space, signing in, and
  connecting an org watch the relevant spaces; space hosts then notify
  chalk of writes at `/xrpc/com.atproto.space.notifyWrite`, and a cron
  trigger (every minute) runs whatever has come due. All of the syncer's
  state lives in D1 (through `DrizzleSyncStore` from
  `internal/spaceSync/drizzle`, shared with Drop), so each short-lived
  syncer picks up where the last left off. Delegation tokens are minted as the space authority, falling
  back to members with a grant on the doc.
- **Sink** (`src/server/sink.ts`): applies verified batches to the D1
  mirror (docs, access grants, comments) and merges crdt records into each
  doc's `DocRoom`.

```bash
pnpm build
pnpm exec wrangler deploy
```

**`wrangler.jsonc`'s top-level `vars` are the real deployment's values, not
local dev's** — the reverse of the usual convention. `@cloudflare/
vite-plugin`'s `vite build` bakes the top-level config into
`dist/server/wrangler.json` regardless of `--env`, so Wrangler's named
environments (`wrangler deploy --env production`) don't work for selecting
per-environment vars in this build pipeline — confirmed by inspecting that
file after a build with an `env.production` override in place, and by a
`--dry-run` that reported the top-level values back regardless of `--env`.
Local dev instead overrides `vars` via `.dev.vars` (gitignored, wins over
`vars` for `wrangler dev`/`vite dev`) — see that file for the local values.

`CHALK_BASE_URL` must be where the Worker is actually served: it's the
OAuth client ID's origin (`/oauth/client-metadata.json`) and chalk's
`did:web` service DID (`/.well-known/did.json`), which space hosts deliver
notifications to.

First-time setup:

```bash
# Create the D1 database and update wrangler.jsonc's database_id with the
# id it prints (see d1_databases in wrangler.jsonc).
pnpm exec wrangler d1 create chalk
pnpm exec wrangler d1 migrations apply chalk --remote

# Both are wrangler secrets, not vars (see wrangler.jsonc):
pnpm exec wrangler secret put CHALK_SESSION_SECRET   # 32+ characters
pnpm exec wrangler secret put CHALK_CREDENTIALS_KEY  # openssl rand -base64 32
```

Update `wrangler.jsonc`'s `vars` (`CHALK_BASE_URL`, `CHALK_HABITAT_URL`)
for the real deployment's domain and Habitat instance, and its
`d1_databases[0].database_id` for the id `wrangler d1 create` printed.
Durable Object migrations (`migrations` in `wrangler.jsonc`) are applied
automatically by `wrangler deploy`. Without a custom domain, the Worker is
served from `https://<name>.<account subdomain>.workers.dev` — find the
account subdomain with `wrangler whoami` (Cloudflare dashboard → Workers &
Pages → your account) or the `GET /accounts/:id/workers/subdomain` API —
`CHALK_BASE_URL` needs to match wherever it actually ends up.

### Local development

`moon chalk:dev` starts pear, Caddy, and the Worker on port 5177
(`https://chalk.local.habitat.network`).

The local D1 database starts out empty and nothing in the Workers runtime
creates its schema, so `dev` applies the migrations in `drizzle/` on every
start (`pnpm db:migrate-local`, idempotent) before handing off to Vite.
Deleting `.wrangler/` resets that database; the next `dev` re-applies the
migrations, but any documents it held are gone.

Each `DocRoom` Durable Object carries its own separate storage — a single
`"doc"` key plus a small, prefix-grouped set of pending-flush keys — read
directly via `ctx.storage.get`/`put`/`list` (no schema, no migrations;
Cloudflare's own storage-access guidance recommends the key-value API over
`ctx.storage.sql` for exactly this shape: one current-state value plus
simple per-key entries). If you have local Durable Object state from
before this, its rooms silently start empty rather than erroring — the old
data lived in SQL tables this code no longer reads — so a document created
before this change won't show its content locally until edited again;
clear stale state with `rm -rf .wrangler/state/v3/do` if that's confusing.

Regenerate the D1 migration after changing `src/db/schema.ts`:

```bash
pnpm db:generate
```

One thing does need doing by hand the first time:

```
# .dev.vars (gitignored) — secrets, plus local overrides of wrangler.jsonc's vars
CHALK_SESSION_SECRET=dev-only-32-char-minimum-secret!!
CHALK_CREDENTIALS_KEY=<openssl rand -base64 32>
CHALK_BASE_URL=https://chalk.local.habitat.network
CHALK_HABITAT_URL=https://pear.local.habitat.network
```

Regenerate `worker-configuration.d.ts` (the typed `Env` global) after any
binding or var change: `pnpm cf-typegen` (also runs automatically before
`dev`/`build`/`test`).

## Setting up PostHog

1. Create a PostHog account at [posthog.com](https://posthog.com)
2. Get your Project API Key from [Project Settings](https://app.posthog.com/project/settings)
3. Set `VITE_POSTHOG_KEY` in your `.env.local`

### Optional Configuration

- `VITE_POSTHOG_HOST` - Set this if you're using PostHog Cloud EU (`https://eu.i.posthog.com`) or self-hosting

## Routing

This project uses [TanStack Router](https://tanstack.com/router) with file-based routing. Routes are managed as files in `src/routes`.

### Adding A Route

To add a new route to your application just add a new file in the `./src/routes` directory.

TanStack will automatically generate the content of the route file for you.

Now that you have two routes you can use a `Link` component to navigate between them.

### Adding Links

To use SPA (Single Page Application) navigation you will need to import the `Link` component from `@tanstack/react-router`.

```tsx
import { Link } from "@tanstack/react-router";
```

Then anywhere in your JSX you can use it like so:

```tsx
<Link to="/about">About</Link>
```

This will create a link that will navigate to the `/about` route.

More information on the `Link` component can be found in the [Link documentation](https://tanstack.com/router/v1/docs/framework/react/api/router/linkComponent).

### Using A Layout

In the File Based Routing setup the layout is located in `src/routes/__root.tsx`. Anything you add to the root route will appear in all the routes. The route content will appear in the JSX where you render `{children}` in the `shellComponent`.

Here is an example layout that includes a header:

```tsx
import { HeadContent, Scripts, createRootRoute } from "@tanstack/react-router";

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: "My App" },
    ],
  }),
  shellComponent: ({ children }) => (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body>
        <header>
          <nav>
            <Link to="/">Home</Link>
            <Link to="/about">About</Link>
          </nav>
        </header>
        {children}
        <Scripts />
      </body>
    </html>
  ),
});
```

More information on layouts can be found in the [Layouts documentation](https://tanstack.com/router/latest/docs/framework/react/guide/routing-concepts#layouts).

## Server Functions

TanStack Start provides server functions that allow you to write server-side code that seamlessly integrates with your client components.

```tsx
import { createServerFn } from "@tanstack/react-start";

const getServerTime = createServerFn({
  method: "GET",
}).handler(async () => {
  return new Date().toISOString();
});

// Use in a component
function MyComponent() {
  const [time, setTime] = useState("");

  useEffect(() => {
    getServerTime().then(setTime);
  }, []);

  return <div>Server time: {time}</div>;
}
```

## API Routes

You can create API routes by using the `server` property in your route definitions:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { json } from "@tanstack/react-start";

export const Route = createFileRoute("/api/hello")({
  server: {
    handlers: {
      GET: () => json({ message: "Hello, World!" }),
    },
  },
});
```

## Data Fetching

There are multiple ways to fetch data in your application. You can use TanStack Query to fetch data from a server. But you can also use the `loader` functionality built into TanStack Router to load the data for a route before it's rendered.

For example:

```tsx
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/people")({
  loader: async () => {
    const response = await fetch("https://swapi.dev/api/people");
    return response.json();
  },
  component: PeopleComponent,
});

function PeopleComponent() {
  const data = Route.useLoaderData();
  return (
    <ul>
      {data.results.map((person) => (
        <li key={person.name}>{person.name}</li>
      ))}
    </ul>
  );
}
```

Loaders simplify your data fetching logic dramatically. Check out more information in the [Loader documentation](https://tanstack.com/router/latest/docs/framework/react/guide/data-loading#loader-parameters).

# Learn More

You can learn more about all of the offerings from TanStack in the [TanStack documentation](https://tanstack.com).

For TanStack Start specific documentation, visit [TanStack Start](https://tanstack.com/start).

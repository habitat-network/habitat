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

> **This app is not deployable to real Workers yet.** sap reachability from
> a deployed Worker and `Habitat-Did` authentication between chalk and sap
> are unsolved (see `docs/superpowers/specs/2026-08-24-chalk-durable-objects-design.md`,
> "Out of scope" #1) — everything below assumes `CHALK_SAP_INTERNAL_URL`
> points somewhere the Worker can actually reach, which today only holds
> for local dev against a loopback `sap`.

Chalk builds through `@cloudflare/vite-plugin` (not Nitro): a stateless
Worker serving routes, SSR, and server functions, with two Durable Object
classes — `DocRoom` (one per document, owns its Yjs state) and `SapChannel`
(the singleton holding the WebSocket to sap's `/channel`) — plus a D1
database for the docs index.

```bash
pnpm build
pnpm exec wrangler deploy
```

First-time setup:

```bash
# Create the D1 database and update wrangler.jsonc's database_id with the
# id it prints (see d1_databases in wrangler.jsonc).
pnpm exec wrangler d1 create chalk
pnpm exec wrangler d1 migrations apply chalk --remote

# CHALK_SESSION_SECRET is a wrangler secret, not a var (see wrangler.jsonc).
# Locally, put it in .dev.vars (gitignored); in a deployed environment:
pnpm exec wrangler secret put CHALK_SESSION_SECRET
```

`wrangler.jsonc`'s `vars` (`CHALK_BASE_URL`, `CHALK_DOMAIN`,
`CHALK_SAP_INTERNAL_URL`, `CHALK_SAP_PUBLIC_URL`) and Durable Object/D1
bindings cover local dev already; update them for a real deployment's
domains and database id. Durable Object migrations (`migrations` in
`wrangler.jsonc`) are applied automatically by `wrangler deploy`.

### Local development

`moon chalk:dev` starts pear, sap, Caddy, and the Worker on port 5177.

The local D1 database starts out empty and nothing in the Workers runtime
creates its schema, so `dev` applies the migrations in `drizzle/` on every
start (`pnpm db:migrate-local`, idempotent) before handing off to Vite.
Deleting `.wrangler/` resets that database; the next `dev` re-applies the
migrations, but any documents it held are gone.

One thing does need doing by hand the first time:

```bash
# CHALK_SESSION_SECRET comes from .dev.vars (gitignored). Create it with:
echo 'CHALK_SESSION_SECRET=dev-only-32-char-minimum-secret!!' > .dev.vars
```

**Cron triggers do not fire on a schedule in local dev.** `SapChannel`'s
connection to sap's `/channel` is established by the `scheduled()` handler,
so in production the every-minute cron connects it (and reconnects it after
an eviction) on its own — locally nothing calls it. Kick it manually:

```bash
curl http://127.0.0.1:5177/cdn-cgi/handler/scheduled
```

Until you do, edits still sync between browser tabs (that path is
`DocRoom`-local) and still get written back to pear on the debounce, but
inbound outbox events from sap are not consumed.

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

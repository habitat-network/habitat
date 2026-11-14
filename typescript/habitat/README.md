# @habitat-network/habitat

TypeScript SDK for building on [Habitat](https://habitat.network), a data
ownership layer for organizations built on [AT Protocol](https://atproto.com)
primitives.

## Install

```bash
npm install @habitat-network/habitat
```

## Identity resolution

```ts
import { HabitatIdentityResolver } from "@habitat-network/habitat";

const resolver = new HabitatIdentityResolver();

const { did, handle, didDoc } = await resolver.resolve("acme.habitat.network");
```

It satisfies `IdentityResolver`, so it drops straight into an OAuth client:

```ts
import { BrowserOAuthClient } from "@atproto/oauth-client-browser";
import { HabitatIdentityResolver } from "@habitat-network/habitat";

const client = new BrowserOAuthClient({
  clientMetadata,
  identityResolver: new HabitatIdentityResolver(),
});
```

### Self-hosted instances

Pass a service URL to target your own Habitat instance. It defaults to
`https://pear.habitat.network`.

```ts
new HabitatIdentityResolver("https://pear.example.com");
```

## Habitat apps

A small framework for apps that keep a synced, permission-filtered copy of
the slice of an org's space data they need. An app declares its slice and
writes typed query and mutation functions; the framework stores the synced
records, derives who can read each space, and restricts every read to the
viewer's spaces.

```ts
import {
  createRuntime,
  defineApp,
  mutation,
  query,
} from "@habitat-network/habitat";

const app = defineApp({
  name: "tracker",
  spaceTypes: ["com.acme.tracker.project"],
  collections: {
    "com.acme.tracker.issue": {
      fields: { status: "$.status", updatedAt: "$.updatedAt" },
      indexes: [["space", "status", "updatedAt"]],
    },
  },
});

export const openIssues = query({
  args: z.object({ project: z.string() }),
  handler: (ctx, { project }) =>
    ctx.db
      .query("com.acme.tracker.issue")
      .in(project)
      .where("status", "=", "open")
      .orderBy("updatedAt", "desc")
      .collect(), // only ever rows from spaces ctx.viewer can read
});

export const close = mutation({
  args: z.object({ project: z.string(), rkey: z.string() }),
  handler: (ctx, { project, rkey }) =>
    ctx.spaces.put({
      space: project,
      collection: "com.acme.tracker.issue",
      rkey,
      record: { status: "closed", updatedAt: new Date().toISOString() },
    }),
});

const runtime = createRuntime(app, {
  db: env.DB, // a D1 binding, or anything with prepare() and batch()
  agent: (viewer) => sessionFor(viewer), // the member's own OAuth session
});
await runtime.run(openIssues, { viewer: memberDid, args: { project } });
```

How it works:

- **One generic table.** Every synced record is a row of JSON in
  `hab_records`, so syncing doesn't depend on the app's schema. Declared
  indexes become partial expression indexes over that JSON. `runtime.migrate()`
  (run automatically) builds new ones and drops stale ones without touching
  data.
- **Sync.** `runtime.sink` is the sink a space syncer applies verified
  batches to (it matches `internal/spaceSync`'s `PromiseSyncSink`).
- **Permissions.** The sink folds `community.opensocial.membership` and
  `community.opensocial.access` records into ACL tables, trusting only the
  ones the space's authority wrote. `ctx.db` joins against them on every
  read, mirroring pear's own check. A space with no access record, or an org
  whose members space isn't synced, is readable by nobody. Spaces that
  grant access through `network.habitat.relationship` records rather than
  opensocial roles aren't supported yet and resolve the same way.
- **Writes.** `ctx.spaces.put` and `delete` go to the Habitat instance as
  the viewer, which enforces the space's policy, then write through to the
  slice.
- **Change detection.** `runtime.version()` moves with every committed
  change, for clients that poll.
- **Escape hatch.** `ctx.unsafeDb` skips the viewer filter and only works when
  the definition sets `unsafeCrossSpaceReads: true`.

`typescript/apps/chat` is a complete example.

## License

Apache-2.0

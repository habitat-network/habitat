# Chalk on Durable Objects Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move chalk from a single Node process with in-memory sync state to Cloudflare Workers, with Durable Objects owning both the sap subscription and per-document CRDT state.

**Architecture:** A stateless Worker serves routes, SSR, and server functions. One singleton `SapChannel` DO holds the WebSocket to sap's `/channel` and routes each outbox message to the right per-document DO. One `DocRoom` DO per space URI owns that doc's `Y.Doc`, its snapshot in DO SQLite, its subscriber streams, and two alarm-driven debounces. A D1 database holds only the docs index.

**Tech Stack:** TanStack Start 1.168 (Nitro 3 build), Cloudflare Workers + Durable Objects + D1, drizzle-orm 0.45.2 (`drizzle-orm/d1` and `drizzle-orm/durable-sqlite`), yjs 13.6, vitest 4 with `@cloudflare/vitest-pool-workers`, wrangler.

**Spec:** `docs/superpowers/specs/2026-08-24-chalk-durable-objects-design.md`

## Global Constraints

- All work happens in `typescript/apps/chalk/`. Do not modify Go code, `pkg/sap/`, or `lexicons/`.
- Debounce policy, copied verbatim from today's code and unchanged by this refactor: idle `2000` ms, max-wait `10000` ms. Both the member-edit flush and the owner-republish flush use these values.
- Collections: `network.habitat.docs.crdt` (CRDT blob), `network.habitat.docs.markdown` (rendered), space type `network.habitat.docs`, rkey `self`.
- Yjs wire format is **V2 everywhere** (`applyUpdateV2`, `encodeStateAsUpdateV2`). Sending V1 bytes to a V2 decoder throws. Never mix.
- Any `Y.Doc` that will receive an *incremental* update must be seeded from the persisted merged state, never from a bare `new Y.Doc()`. Merging a diff into an empty doc reconstructs only that diff, and persisting the result destroys the document. This hazard is called out in three places in today's code; preserve those comments when moving them.
- sap reachability and `Habitat-Did` authentication are **explicitly out of scope** (see spec, "Out of scope" #1). Keep talking to whatever `CHALK_SAP_INTERNAL_URL` is configured. This branch is not deployable to real Workers until that is solved separately. Do not attempt to solve it here; do not remove the spec's note about it.
- Dev port stays `5177` so the existing root `Caddyfile` entry and `CHALK_BASE_URL` keep working.
- Do not create files in the repo root (see `CLAUDE.md`).
- Run tests with `pnpm --filter chalk test` (vitest) from the repo root, or `pnpm test` inside `typescript/apps/chalk`.

---

### Task 1: Prove the Workers deployment shape (spike)

The single riskiest unknown. Nitro's `cloudflare_durable` preset exports its own `$DurableObject`; whether and how custom DO classes can be added to that build output is unverified. Everything downstream assumes a conventional `env.DOC` binding, so settle it before moving any chalk logic.

**Files:**
- Create: `typescript/apps/chalk/wrangler.jsonc`
- Create: `typescript/apps/chalk/src/server/rooms/ping.ts`
- Modify: `typescript/apps/chalk/vite.config.ts`
- Modify: `typescript/apps/chalk/package.json`
- Create: `typescript/apps/chalk/vitest.config.ts`
- Test: `typescript/apps/chalk/test/ping.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: a working `Env` type with a `DOC`-style DurableObjectNamespace binding, a verified way to reach `env` from inside a `createServerFn` handler, and a green `wrangler dev` on port 5177. Every later task depends on the binding-access idiom this task establishes.

- [ ] **Step 1: Install the Cloudflare toolchain**

```bash
cd typescript/apps/chalk
pnpm add -D wrangler @cloudflare/workers-types @cloudflare/vitest-pool-workers
```

- [ ] **Step 2: Write `wrangler.jsonc` with a single test DO**

```jsonc
{
  "$schema": "node_modules/wrangler/config-schema.json",
  "name": "chalk",
  "compatibility_date": "2026-08-01",
  "compatibility_flags": ["nodejs_compat"],
  "durable_objects": {
    "bindings": [{ "name": "PING", "class_name": "PingRoom" }]
  },
  "migrations": [{ "tag": "v1", "new_sqlite_classes": ["PingRoom"] }],
  "dev": { "port": 5177 }
}
```

- [ ] **Step 3: Write the trivial DO**

```ts
// src/server/rooms/ping.ts
import { DurableObject } from "cloudflare:workers";

export class PingRoom extends DurableObject {
  async bump(): Promise<number> {
    const n = ((await this.ctx.storage.get<number>("n")) ?? 0) + 1;
    await this.ctx.storage.put("n", n);
    return n;
  }
}
```

- [ ] **Step 4: Write `vitest.config.ts` using the workers pool**

```ts
import { defineWorkersConfig } from "@cloudflare/vitest-pool-workers/config";

export default defineWorkersConfig({
  test: {
    poolOptions: {
      workers: {
        wrangler: { configPath: "./wrangler.jsonc" },
      },
    },
  },
});
```

- [ ] **Step 5: Write the failing test**

```ts
// test/ping.test.ts
import { env, runInDurableObject } from "cloudflare:test";
import { expect, it } from "vitest";
import type { PingRoom } from "../src/server/rooms/ping";

it("persists state across calls to the same id", async () => {
  const id = env.PING.idFromName("a");
  const stub = env.PING.get(id);
  await runInDurableObject(stub, (room: PingRoom) => room.bump());
  const second = await runInDurableObject(stub, (room: PingRoom) => room.bump());
  expect(second).toBe(2);
});

it("gives different ids independent state", async () => {
  const other = env.PING.get(env.PING.idFromName("b"));
  const n = await runInDurableObject(other, (room: PingRoom) => room.bump());
  expect(n).toBe(1);
});
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `pnpm --filter chalk test test/ping.test.ts`
Expected: FAIL — the workers pool cannot resolve the `PING` binding, or the module graph rejects `cloudflare:workers`, until config is right. Iterate on steps 2/4 until it passes.

- [ ] **Step 7: Make the test pass**

Adjust `wrangler.jsonc` / `vitest.config.ts` until both tests pass. Expected: PASS.

- [ ] **Step 8: Point the Vite build at Cloudflare and export the DO**

In `vite.config.ts`, set the Nitro preset and keep the existing plugin order (`tanstackStart()` before `viteReact()`):

```ts
tanstackStart({
  target: "cloudflare-module",
}),
```

Then verify a production build emits a Worker that **exports `PingRoom`**. This is the crux of the spike. In order of preference, try:
1. a `nitro.options.cloudflare.wrangler` passthrough plus a re-export from the app's server entry;
2. Nitro's `cloudflare:durable:init` hook with the `cloudflare_durable` preset, if custom classes can ride alongside `$DurableObject`;
3. a hand-written wrapper Worker entry that imports Nitro's built handler as its default export and re-exports `PingRoom` beside it.

Run: `pnpm --filter chalk build && grep -c "PingRoom" .output/server/index.mjs`
Expected: a non-zero count.

- [ ] **Step 9: Verify a server function can reach the binding**

Add a temporary `ping` server fn in `src/server/functions.ts` that calls `env.PING.get(env.PING.idFromName("a")).bump()`, and confirm it returns an incrementing number under `wrangler dev` on port 5177. Record in the plan file (edit this task's notes) **exactly how `env` was obtained inside the handler** — that idiom is used by every later task.

- [ ] **Step 10: Commit**

```bash
git add typescript/apps/chalk
git commit -m "[Chalk] Prove Workers + Durable Objects deployment shape"
```

- [ ] **Step 11: Gate**

If step 8 could not be made to work by any of the three routes, **stop and report** rather than proceeding. The whole plan rests on it, and the fallback (a separately-deployed Workers service, keeping the Node server) is a different design that needs a new decision from the user.

---

### Task 2: Docs index on D1 via drizzle

**Files:**
- Modify: `typescript/apps/chalk/src/db/schema.ts`
- Modify: `typescript/apps/chalk/src/db/index.ts`
- Create: `typescript/apps/chalk/drizzle.config.ts`
- Modify: `typescript/apps/chalk/wrangler.jsonc`
- Test: `typescript/apps/chalk/test/docsIndex.test.ts`

**Interfaces:**
- Consumes: Task 1's `Env` and binding idiom.
- Produces:
  - `docs` table (drizzle): `spaceUri: text primary key`, `docId: text not null unique`, `ownerDid: text not null`, `title: text not null`, `updatedAt: integer not null`
  - `getDb(env: Env)` returning a drizzle D1 instance
  - `upsertDoc(db, { spaceUri, docId, ownerDid, title }): Promise<void>`
  - `docsForOwner(db, ownerDid): Promise<DocSummary[]>`
  - `docByUri(db, spaceUri): Promise<DocSummary | undefined>`
  - `DocSummary` keeps its existing shape: `{ docId, uri, ownerDid, title }`

- [ ] **Step 1: Write the failing test**

```ts
// test/docsIndex.test.ts
import { env } from "cloudflare:test";
import { beforeEach, expect, it } from "vitest";
import { getDb, upsertDoc, docsForOwner, docByUri } from "../src/db";

const URI = "at://did:web:alice.example/space/network.habitat.docs/abc";

beforeEach(async () => {
  await env.DB.exec("DELETE FROM docs");
});

it("returns a member's docs newest first", async () => {
  const db = getDb(env);
  await upsertDoc(db, { spaceUri: URI, docId: URI, ownerDid: "did:web:alice.example", title: "Untitled" });
  const rows = await docsForOwner(db, "did:web:alice.example");
  expect(rows).toEqual([
    { docId: URI, uri: URI, ownerDid: "did:web:alice.example", title: "Untitled" },
  ]);
});

it("upserts on conflict rather than duplicating", async () => {
  const db = getDb(env);
  const doc = { spaceUri: URI, docId: URI, ownerDid: "did:web:alice.example", title: "Untitled" };
  await upsertDoc(db, doc);
  await upsertDoc(db, { ...doc, title: "Renamed" });
  expect(await docsForOwner(db, "did:web:alice.example")).toHaveLength(1);
  expect((await docByUri(db, URI))?.title).toBe("Renamed");
});

it("returns undefined for an unknown uri", async () => {
  expect(await docByUri(getDb(env), "at://nope/space/x/y")).toBeUndefined();
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter chalk test test/docsIndex.test.ts`
Expected: FAIL — `getDb` is not exported from `../src/db`.

- [ ] **Step 3: Write the schema**

```ts
// src/db/schema.ts
import { sqliteTable, text, integer, index } from "drizzle-orm/sqlite-core";

export const docs = sqliteTable(
  "docs",
  {
    spaceUri: text("space_uri").primaryKey(),
    docId: text("doc_id").notNull().unique(),
    ownerDid: text("owner_did").notNull(),
    title: text("title").notNull(),
    updatedAt: integer("updated_at").notNull(),
  },
  (t) => [index("docs_owner_updated").on(t.ownerDid, t.updatedAt)],
);
```

- [ ] **Step 4: Write the accessors**

```ts
// src/db/index.ts
import { drizzle } from "drizzle-orm/d1";
import { desc, eq } from "drizzle-orm";
import { docs } from "./schema";

export interface DocSummary {
  docId: string;
  uri: string;
  ownerDid: string;
  title: string;
}

export function getDb(env: { DB: D1Database }) {
  return drizzle(env.DB, { schema: { docs } });
}

export type Db = ReturnType<typeof getDb>;

export async function upsertDoc(
  db: Db,
  doc: { spaceUri: string; docId: string; ownerDid: string; title: string },
): Promise<void> {
  const row = { ...doc, updatedAt: Date.now() };
  await db
    .insert(docs)
    .values(row)
    .onConflictDoUpdate({
      target: docs.spaceUri,
      set: { docId: row.docId, ownerDid: row.ownerDid, title: row.title, updatedAt: row.updatedAt },
    });
}

function toSummary(r: typeof docs.$inferSelect): DocSummary {
  return { docId: r.docId, uri: r.spaceUri, ownerDid: r.ownerDid, title: r.title };
}

export async function docsForOwner(db: Db, ownerDid: string): Promise<DocSummary[]> {
  const rows = await db.select().from(docs).where(eq(docs.ownerDid, ownerDid)).orderBy(desc(docs.updatedAt));
  return rows.map(toSummary);
}

export async function docByUri(db: Db, spaceUri: string): Promise<DocSummary | undefined> {
  const [row] = await db.select().from(docs).where(eq(docs.spaceUri, spaceUri)).limit(1);
  return row ? toSummary(row) : undefined;
}
```

- [ ] **Step 5: Add the D1 binding and generate the migration**

Add to `wrangler.jsonc`:

```jsonc
"d1_databases": [
  { "binding": "DB", "database_name": "chalk", "database_id": "local", "migrations_dir": "drizzle" }
]
```

Write `drizzle.config.ts`:

```ts
import { defineConfig } from "drizzle-kit";

export default defineConfig({
  schema: "./src/db/schema.ts",
  out: "./drizzle",
  dialect: "sqlite",
  driver: "d1-http",
});
```

Run: `pnpm exec drizzle-kit generate` — this writes a numbered `.sql` file into `./drizzle`, which is the `migrations_dir` wrangler reads. No further config is needed; wrangler tracks applied migrations itself in a `d1_migrations` table.

Apply locally: `pnpm exec wrangler d1 migrations apply chalk --local`

Create the local database first if wrangler reports it missing: `pnpm exec wrangler d1 create chalk`, then replace `"database_id": "local"` with the id it prints.

- [ ] **Step 6: Run test to verify it passes**

Run: `pnpm --filter chalk test test/docsIndex.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 7: Rewire `listDocs` and `createDoc`**

In `src/server/functions.ts`, replace `store.docsForOwner(did)` with `docsForOwner(getDb(env), did)` and `store.upsertDoc({...})` with `upsertDoc(getDb(env), {...})`, using Task 1's `env` idiom. Leave every sap call in `createDoc` exactly as-is, including the `trackSpace` call and its comment.

`createDoc` must also seed the room's identity, so that alarm-driven republish knows the owner before any caller supplies it. Add this after the `upsertDoc` call (the method lands in Task 3; until then, leave the line commented with a `TODO(Task 3)` and uncomment it there):

```ts
await env.DOC.get(env.DOC.idFromName(created.uri))
  .applyEdit({ spaceUri: created.uri, ownerDid: did }, did, new Uint8Array(0));
```

An empty update is a valid no-op merge in Yjs, so this records identity without altering the document.

- [ ] **Step 8: Verify and commit**

Run: `pnpm --filter chalk test`
Expected: PASS. `docStore.test.ts` still passes at this point — `DocStore` is still present for CRDT state and is removed in Task 9.

```bash
git add typescript/apps/chalk
git commit -m "[Chalk] Move docs index to D1 via drizzle"
```

---

### Task 3: `DocRoom` DO — identity, storage, and merge

The heart of the refactor. No streaming, no debounce, no sap yet — just "a DO that owns one doc's `Y.Doc` and persists it."

**Files:**
- Create: `typescript/apps/chalk/src/server/rooms/docRoom.ts`
- Create: `typescript/apps/chalk/src/server/rooms/docRoomSchema.ts`
- Modify: `typescript/apps/chalk/wrangler.jsonc`
- Test: `typescript/apps/chalk/test/docRoom.test.ts`

**Interfaces:**
- Consumes: Task 1's binding idiom.
- Produces:
  - `interface DocIdentity { spaceUri: string; ownerDid?: string }`
  - `class DocRoom extends DurableObject<Env>` with `applyEdit(id: DocIdentity, memberDid: string, update: Uint8Array): Promise<void>` and `applyRemote(id: DocIdentity, cid: string): Promise<void>`
  - a private `mergeUpdate(bytes: Uint8Array): void` both call
  - `snapshot(): Promise<Uint8Array>` returning `Y.encodeStateAsUpdateV2` of current state
  - binding name `DOC`

- [ ] **Step 1: Write the failing test**

```ts
// test/docRoom.test.ts
import { env, runInDurableObject } from "cloudflare:test";
import { expect, it } from "vitest";
import * as Y from "yjs";
import type { DocRoom } from "../src/server/rooms/docRoom";

const URI = "at://did:web:alice.example/space/network.habitat.docs/abc";
const ID = { spaceUri: URI, ownerDid: "did:web:alice.example" };

function updateFrom(fn: (d: Y.Doc) => void): Uint8Array {
  const d = new Y.Doc();
  fn(d);
  return Y.encodeStateAsUpdateV2(d);
}

function textOf(bytes: Uint8Array): string {
  const d = new Y.Doc();
  Y.applyUpdateV2(d, bytes);
  return d.getText("body").toString();
}

it("merges an update and exposes it in the snapshot", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI));
  const update = updateFrom((d) => d.getText("body").insert(0, "hello"));
  await runInDurableObject(stub, (r: DocRoom) => r.applyEdit(ID, "did:web:bob.example", update));
  const snap = await runInDurableObject(stub, (r: DocRoom) => r.snapshot());
  expect(textOf(snap)).toBe("hello");
});

it("merges concurrent updates from two members", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI + "-2"));
  const id = { spaceUri: URI + "-2", ownerDid: ID.ownerDid };
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(id, "did:web:bob.example", updateFrom((d) => d.getText("body").insert(0, "a"))),
  );
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(id, "did:web:carol.example", updateFrom((d) => d.getText("body").insert(0, "b"))),
  );
  expect(textOf(await runInDurableObject(stub, (r: DocRoom) => r.snapshot()))).toHaveLength(2);
});

it("re-applying the same update is a no-op", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI + "-3"));
  const id = { spaceUri: URI + "-3", ownerDid: ID.ownerDid };
  const update = updateFrom((d) => d.getText("body").insert(0, "xy"));
  await runInDurableObject(stub, (r: DocRoom) => r.applyEdit(id, "did:web:bob.example", update));
  await runInDurableObject(stub, (r: DocRoom) => r.applyEdit(id, "did:web:bob.example", update));
  expect(textOf(await runInDurableObject(stub, (r: DocRoom) => r.snapshot()))).toBe("xy");
});

it("remembers its identity so alarm-driven work can use it", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI + "-4"));
  const id = { spaceUri: URI + "-4", ownerDid: "did:web:dave.example" };
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(id, "did:web:bob.example", updateFrom((d) => d.getText("body").insert(0, "z"))),
  );
  await runInDurableObject(stub, async (r: DocRoom) => {
    expect(await r.identity()).toEqual(id);
  });
});

it("keeps a known ownerDid when a later caller omits it", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI + "-5"));
  const uri = URI + "-5";
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit({ spaceUri: uri, ownerDid: "did:web:dave.example" }, "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "1"))),
  );
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit({ spaceUri: uri }, "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "2"))),
  );
  await runInDurableObject(stub, async (r: DocRoom) => {
    expect((await r.identity()).ownerDid).toBe("did:web:dave.example");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter chalk test test/docRoom.test.ts`
Expected: FAIL — module `../src/server/rooms/docRoom` not found.

- [ ] **Step 3: Write the DO-side schema**

```ts
// src/server/rooms/docRoomSchema.ts
import { sqliteTable, text, integer, blob } from "drizzle-orm/sqlite-core";

// One row, id = 0: this room's merged CRDT snapshot and its identity. A DO
// cannot read back the name it was addressed by, so spaceUri/ownerDid are
// recorded here on first sight and reused for alarm-driven work, when no
// caller is present to supply them.
export const docState = sqliteTable("doc_state", {
  id: integer("id").primaryKey(),
  spaceUri: text("space_uri").notNull(),
  ownerDid: text("owner_did"),
  state: blob("state", { mode: "buffer" }),
  updatedAt: integer("updated_at").notNull(),
});
```

- [ ] **Step 4: Write the DO**

```ts
// src/server/rooms/docRoom.ts
import { DurableObject } from "cloudflare:workers";
import { drizzle, type DrizzleSqliteDODatabase } from "drizzle-orm/durable-sqlite";
import { eq } from "drizzle-orm";
import * as Y from "yjs";
import { docState } from "./docRoomSchema";

export interface DocIdentity {
  spaceUri: string;
  // Optional: a doc shared with this member may not be in this deployment's
  // D1 index, and the member-edit flush writes to the member's own repo and
  // never needs the owner. The owner-republish flush is skipped while this
  // is unknown, mirroring today's `republishCanonical`'s `if (!doc) return;`.
  ownerDid?: string;
}

export class DocRoom extends DurableObject<Env> {
  private db: DrizzleSqliteDODatabase;
  private ydoc = new Y.Doc();
  private id: DocIdentity | undefined;

  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    this.db = drizzle(ctx.storage);
    ctx.blockConcurrencyWhile(async () => {
      this.db.run(`CREATE TABLE IF NOT EXISTS doc_state (
        id INTEGER PRIMARY KEY, space_uri TEXT NOT NULL,
        owner_did TEXT, state BLOB, updated_at INTEGER NOT NULL)`);
      const [row] = await this.db.select().from(docState).where(eq(docState.id, 0)).limit(1);
      if (!row) return;
      this.id = { spaceUri: row.spaceUri, ownerDid: row.ownerDid ?? undefined };
      if (row.state) Y.applyUpdateV2(this.ydoc, new Uint8Array(row.state));
    });
  }

  async identity(): Promise<DocIdentity> {
    if (!this.id) throw new Error("DocRoom has no identity yet");
    return this.id;
  }

  async snapshot(): Promise<Uint8Array> {
    return Y.encodeStateAsUpdateV2(this.ydoc);
  }

  async applyEdit(id: DocIdentity, _memberDid: string, update: Uint8Array): Promise<void> {
    await this.rememberIdentity(id);
    await this.mergeUpdate(update);
  }

  async applyRemote(id: DocIdentity, _cid: string): Promise<void> {
    await this.rememberIdentity(id);
    // Blob dereference lands in Task 7.
  }

  // rememberIdentity records spaceUri/ownerDid the first time any caller
  // supplies them, and never downgrades a known ownerDid back to undefined.
  private async rememberIdentity(id: DocIdentity): Promise<void> {
    const next: DocIdentity = {
      spaceUri: id.spaceUri,
      ownerDid: id.ownerDid ?? this.id?.ownerDid,
    };
    if (this.id?.spaceUri === next.spaceUri && this.id?.ownerDid === next.ownerDid) return;
    this.id = next;
    await this.persist();
  }

  // mergeUpdate applies a Yjs update to the in-memory doc and persists the
  // merged result. `this.ydoc` is loaded from storage in the constructor,
  // never a bare `new Y.Doc()` at merge time: `update` is an *incremental*
  // diff relative to the sender's state, so merging it into an empty doc
  // would reconstruct only that fragment and persisting it would destroy
  // everything that came before.
  protected async mergeUpdate(update: Uint8Array): Promise<void> {
    Y.applyUpdateV2(this.ydoc, update);
    await this.persist();
  }

  private async persist(): Promise<void> {
    if (!this.id) return;
    const row = {
      id: 0,
      spaceUri: this.id.spaceUri,
      ownerDid: this.id.ownerDid ?? null,
      state: Buffer.from(Y.encodeStateAsUpdateV2(this.ydoc)),
      updatedAt: Date.now(),
    };
    await this.db.insert(docState).values(row).onConflictDoUpdate({ target: docState.id, set: row });
  }
}
```

- [ ] **Step 5: Add the binding**

In `wrangler.jsonc`, add `{ "name": "DOC", "class_name": "DocRoom" }` to `durable_objects.bindings` and `"DocRoom"` to the `new_sqlite_classes` migration. Export `DocRoom` from the Worker entry using the mechanism Task 1 established.

- [ ] **Step 6: Run test to verify it passes**

Run: `pnpm --filter chalk test test/docRoom.test.ts`
Expected: PASS (5 tests).

- [ ] **Step 7: Commit**

```bash
git add typescript/apps/chalk
git commit -m "[Chalk] Add DocRoom durable object with persisted CRDT state"
```

---

### Task 4: `DocRoom` subscriptions and `subscribeDoc`

**Files:**
- Modify: `typescript/apps/chalk/src/server/rooms/docRoom.ts`
- Modify: `typescript/apps/chalk/src/server/functions.ts`
- Test: `typescript/apps/chalk/test/docRoomSubscribe.test.ts`

**Interfaces:**
- Consumes: `DocRoom`, `DocIdentity`, `mergeUpdate` from Task 3.
- Produces: `DocRoom.fetch(req)` handling `GET /subscribe`, returning a `Response` whose body is a `ReadableStream` of length-prefixed Yjs V2 update frames; the first frame is the current snapshot. `subscribeDoc` keeps its existing signature: `AsyncGenerator<{ state: Uint8Array }>`.

**Framing:** a stream body is a byte stream with no message boundaries, so each frame is a 4-byte big-endian length followed by that many bytes. Today's in-process pubsub yielded whole `Y.Doc`s and never needed this.

- [ ] **Step 1: Write the failing test**

```ts
// test/docRoomSubscribe.test.ts
import { env, runInDurableObject } from "cloudflare:test";
import { expect, it } from "vitest";
import * as Y from "yjs";
import type { DocRoom } from "../src/server/rooms/docRoom";
import { readFrames } from "../src/server/rooms/frames";

const URI = "at://did:web:alice.example/space/network.habitat.docs/sub";
const ID = { spaceUri: URI, ownerDid: "did:web:alice.example" };

function updateFrom(fn: (d: Y.Doc) => void): Uint8Array {
  const d = new Y.Doc();
  fn(d);
  return Y.encodeStateAsUpdateV2(d);
}

it("sends the current snapshot as the first frame", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(ID, "did:web:bob.example", updateFrom((d) => d.getText("body").insert(0, "hi"))),
  );
  const res = await stub.fetch("https://do/subscribe");
  const frames = readFrames(res.body!);
  const first = (await frames.next()).value as Uint8Array;
  const d = new Y.Doc();
  Y.applyUpdateV2(d, first);
  expect(d.getText("body").toString()).toBe("hi");
});

it("pushes subsequent merges to a live subscriber", async () => {
  const uri = URI + "-2";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  const id = { spaceUri: uri, ownerDid: ID.ownerDid };
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(id, "did:web:bob.example", updateFrom((d) => d.getText("body").insert(0, "a"))),
  );
  const res = await stub.fetch("https://do/subscribe");
  const frames = readFrames(res.body!);
  await frames.next(); // snapshot
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(id, "did:web:carol.example", updateFrom((d) => d.getText("body").insert(0, "b"))),
  );
  const next = (await frames.next()).value as Uint8Array;
  const d = new Y.Doc();
  Y.applyUpdateV2(d, next);
  expect(d.getText("body").toString()).toHaveLength(2);
});

it("drops a subscriber whose stream is cancelled without failing merges", async () => {
  const uri = URI + "-3";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  const res = await stub.fetch("https://do/subscribe");
  await res.body!.cancel();
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit({ spaceUri: uri }, "did:web:bob.example",
      updateFrom((d) => d.getText("body").insert(0, "c"))),
  );
  await runInDurableObject(stub, async (r: DocRoom) => {
    expect(await r.subscriberCount()).toBe(0);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter chalk test test/docRoomSubscribe.test.ts`
Expected: FAIL — `../src/server/rooms/frames` not found.

- [ ] **Step 3: Write the framing helpers**

```ts
// src/server/rooms/frames.ts

// A streamed Response body has no message boundaries, so each Yjs update is
// written as a 4-byte big-endian length followed by that many bytes.
export function frame(bytes: Uint8Array): Uint8Array {
  const out = new Uint8Array(4 + bytes.byteLength);
  new DataView(out.buffer).setUint32(0, bytes.byteLength, false);
  out.set(bytes, 4);
  return out;
}

export async function* readFrames(body: ReadableStream<Uint8Array>): AsyncGenerator<Uint8Array> {
  const reader = body.getReader();
  let buf = new Uint8Array(0);
  while (true) {
    while (buf.byteLength >= 4) {
      const len = new DataView(buf.buffer, buf.byteOffset, 4).getUint32(0, false);
      if (buf.byteLength < 4 + len) break;
      yield buf.slice(4, 4 + len);
      buf = buf.slice(4 + len);
    }
    const { done, value } = await reader.read();
    if (done) return;
    const merged = new Uint8Array(buf.byteLength + value.byteLength);
    merged.set(buf);
    merged.set(value, buf.byteLength);
    buf = merged;
  }
}
```

- [ ] **Step 4: Add subscription support to `DocRoom`**

Add to the class:

```ts
  private subscribers = new Set<WritableStreamDefaultWriter<Uint8Array>>();

  async subscriberCount(): Promise<number> {
    return this.subscribers.size;
  }

  async fetch(req: Request): Promise<Response> {
    if (new URL(req.url).pathname !== "/subscribe") {
      return new Response("not found", { status: 404 });
    }
    const { readable, writable } = new TransformStream<Uint8Array, Uint8Array>();
    const writer = writable.getWriter();
    this.subscribers.add(writer);
    // Emitting the snapshot here, after registration, closes a race today's
    // code has: `store.mergedState` and `pubsub.subscribe` were two separate
    // steps, so a merge landing between them was lost.
    void writer.write(frame(Y.encodeStateAsUpdateV2(this.ydoc))).catch(() => this.drop(writer));
    return new Response(readable);
  }

  private drop(writer: WritableStreamDefaultWriter<Uint8Array>): void {
    this.subscribers.delete(writer);
    void writer.close().catch(() => {});
  }

  private broadcast(): void {
    const bytes = frame(Y.encodeStateAsUpdateV2(this.ydoc));
    for (const writer of this.subscribers) {
      void writer.write(bytes).catch(() => this.drop(writer));
    }
  }
```

and call `this.broadcast()` at the end of `mergeUpdate`, after `persist()`. Import `frame` from `./frames` in `docRoom.ts`, and `readFrames` from `./rooms/frames` in `functions.ts`.

- [ ] **Step 5: Run test to verify it passes**

Run: `pnpm --filter chalk test test/docRoomSubscribe.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 6: Rewire `subscribeDoc`**

Replace the handler body in `src/server/functions.ts`, keeping the existing doc-comment about why `docId` is the full space URI:

```ts
export const subscribeDoc = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(async function* ({ data }): AsyncGenerator<{ state: Uint8Array }> {
    await requireSession();
    const stub = env.DOC.get(env.DOC.idFromName(data.docId));
    const res = await stub.fetch("https://do/subscribe");
    for await (const state of readFrames(res.body!)) {
      yield { state };
    }
  });
```

- [ ] **Step 7: Verify end to end and commit**

Run `moon chalk:dev`, open a doc in two browser tabs, type in one, confirm the other updates.
Run: `pnpm --filter chalk test`
Expected: PASS.

```bash
git add typescript/apps/chalk
git commit -m "[Chalk] Stream doc updates from DocRoom instead of in-process pubsub"
```

---

### Task 5: Alarm-driven member-edit flush

Replaces `DebounceQueue`'s `setTimeout` timers, which do not survive DO eviction. A DO has exactly one alarm, so deadlines live in storage and the alarm is always set to the earliest.

**Files:**
- Modify: `typescript/apps/chalk/src/server/rooms/docRoom.ts`
- Modify: `typescript/apps/chalk/src/server/rooms/docRoomSchema.ts`
- Modify: `typescript/apps/chalk/src/server/sapClient.ts`
- Modify: `typescript/apps/chalk/src/server/functions.ts`
- Test: `typescript/apps/chalk/test/docRoomFlush.test.ts`

**Interfaces:**
- Consumes: `DocRoom` from Tasks 3-4; `SapClient` from the existing code.
- Produces: `DocRoom.alarm()`; a `pending_flush` table; `SapClient` constructed as `new SapClient(env, did)` rather than reading `process.env`.

- [ ] **Step 1: Change `SapClient` to take `env`**

Replace the module-level `sapInternalUrl()` with a constructor argument. `process.env` does not exist on workerd, so this is required, not cosmetic:

```ts
export class SapClient {
  constructor(private env: Env, private did: string) {}
  private get base(): string {
    const url = this.env.CHALK_SAP_INTERNAL_URL;
    if (!url) throw new Error("CHALK_SAP_INTERNAL_URL is not set");
    return url;
  }
  // ... every `sapInternalUrl()` call site becomes `this.base`
}
```

`startLogin` becomes `startLogin(env, handle)` with the same body, reading `env.CHALK_BASE_URL`. Update its caller in `src/routes/login.tsx`.

- [ ] **Step 2: Run the existing sapClient tests**

Run: `pnpm --filter chalk test test/sapClient.test.ts`
Expected: FAIL to compile until the tests pass an `env` stub. Update them to `new SapClient({ CHALK_SAP_INTERNAL_URL: "http://sap.test" } as Env, "did:web:alice.example")`. Then: PASS.

- [ ] **Step 3: Write the failing flush test**

```ts
// test/docRoomFlush.test.ts
import { env, runInDurableObject, runDurableObjectAlarm } from "cloudflare:test";
import { beforeEach, expect, it, vi } from "vitest";
import * as Y from "yjs";
import type { DocRoom } from "../src/server/rooms/docRoom";

const URI = "at://did:web:alice.example/space/network.habitat.docs/flush";
const ID = { spaceUri: URI, ownerDid: "did:web:alice.example" };

const fetchMock = vi.fn();
beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockResolvedValue(new Response(JSON.stringify({ blob: { ref: { $link: "cid1" } }, cid: "cid1" })));
  vi.stubGlobal("fetch", fetchMock);
});

function updateFrom(fn: (d: Y.Doc) => void): Uint8Array {
  const d = new Y.Doc();
  fn(d);
  return Y.encodeStateAsUpdateV2(d);
}

it("does not write to sap before the debounce fires", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit(ID, "did:web:bob.example", updateFrom((d) => d.getText("body").insert(0, "x"))),
  );
  expect(fetchMock).not.toHaveBeenCalled();
});

it("schedules an alarm on the first edit", async () => {
  const uri = URI + "-2";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit({ spaceUri: uri }, "did:web:bob.example", updateFrom((d) => d.getText("body").insert(0, "x"))),
  );
  await runInDurableObject(stub, async (r: DocRoom, state) => {
    expect(await state.storage.getAlarm()).not.toBeNull();
  });
});

it("uploads a blob and putRecords to the member's own repo when the alarm fires", async () => {
  const uri = URI + "-3";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit({ spaceUri: uri }, "did:web:bob.example", updateFrom((d) => d.getText("body").insert(0, "x"))),
  );
  await runDurableObjectAlarm(stub);
  const urls = fetchMock.mock.calls.map((c) => String(c[0]));
  expect(urls.some((u) => u.includes("network.habitat.repo.uploadBlob"))).toBe(true);
  const put = fetchMock.mock.calls.find((c) => String(c[0]).includes("space.putRecord"));
  expect(JSON.parse(String(put![1].body))).toMatchObject({
    space: uri,
    repo: "did:web:bob.example",
    collection: "network.habitat.docs.crdt",
    rkey: "self",
  });
});

it("flushes each member to their own repo separately", async () => {
  const uri = URI + "-4";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  for (const did of ["did:web:bob.example", "did:web:carol.example"]) {
    await runInDurableObject(stub, (r: DocRoom) =>
      r.applyEdit({ spaceUri: uri }, did, updateFrom((d) => d.getText("body").insert(0, did[8]!))),
    );
  }
  await runDurableObjectAlarm(stub);
  const repos = fetchMock.mock.calls
    .filter((c) => String(c[0]).includes("space.putRecord"))
    .map((c) => JSON.parse(String(c[1].body)).repo);
  expect(new Set(repos)).toEqual(new Set(["did:web:bob.example", "did:web:carol.example"]));
});
```

- [ ] **Step 4: Run test to verify it fails**

Run: `pnpm --filter chalk test test/docRoomFlush.test.ts`
Expected: FAIL — no alarm is ever scheduled.

- [ ] **Step 5: Add the pending-flush table**

```ts
// append to src/server/rooms/docRoomSchema.ts
export const pendingFlush = sqliteTable("pending_flush", {
  // kind is "member" or "owner"; memberDid is "" for the owner flush, so
  // (kind, memberDid) is a usable composite key.
  kind: text("kind").notNull(),
  memberDid: text("member_did").notNull(),
  firstPushAt: integer("first_push_at").notNull(),
  idleDeadline: integer("idle_deadline").notNull(),
}, (t) => [primaryKey({ columns: [t.kind, t.memberDid] })]);
```

Import `primaryKey` from `drizzle-orm/sqlite-core` and add the matching `CREATE TABLE IF NOT EXISTS pending_flush (kind TEXT NOT NULL, member_did TEXT NOT NULL, first_push_at INTEGER NOT NULL, idle_deadline INTEGER NOT NULL, PRIMARY KEY (kind, member_did))` to the constructor's `blockConcurrencyWhile`.

- [ ] **Step 6: Implement the debounce**

```ts
  // Policy carried over verbatim from today's DebounceQueue: flush 2s after
  // the last push, but never let more than 10s of edits go unwritten.
  private static readonly IDLE_MS = 2000;
  private static readonly MAX_WAIT_MS = 10000;

  private async schedule(kind: "member" | "owner", memberDid: string): Promise<void> {
    const now = Date.now();
    const [existing] = await this.db.select().from(pendingFlush)
      .where(and(eq(pendingFlush.kind, kind), eq(pendingFlush.memberDid, memberDid))).limit(1);
    await this.db.insert(pendingFlush).values({
      kind, memberDid,
      firstPushAt: existing?.firstPushAt ?? now,
      idleDeadline: now + DocRoom.IDLE_MS,
    }).onConflictDoUpdate({
      target: [pendingFlush.kind, pendingFlush.memberDid],
      set: { idleDeadline: now + DocRoom.IDLE_MS },
    });
    await this.rearm();
  }

  // A DO has exactly one alarm, so it is always set to the earliest deadline
  // across every pending row: min(idleDeadline, firstPushAt + MAX_WAIT_MS).
  private async rearm(): Promise<void> {
    const rows = await this.db.select().from(pendingFlush);
    if (rows.length === 0) return;
    const next = Math.min(
      ...rows.map((r) => Math.min(r.idleDeadline, r.firstPushAt + DocRoom.MAX_WAIT_MS)),
    );
    await this.ctx.storage.setAlarm(next);
  }

  async alarm(): Promise<void> {
    const now = Date.now();
    const rows = await this.db.select().from(pendingFlush);
    for (const row of rows) {
      const due = Math.min(row.idleDeadline, row.firstPushAt + DocRoom.MAX_WAIT_MS);
      if (due > now) continue;
      await this.db.delete(pendingFlush)
        .where(and(eq(pendingFlush.kind, row.kind), eq(pendingFlush.memberDid, row.memberDid)));
      if (row.kind === "member") await this.flushMember(row.memberDid);
      // "owner" is handled in Task 6.
    }
    await this.rearm();
  }

  // flushMember writes the merged state to this member's own repo. It uploads
  // `this.ydoc` — the full merged snapshot, not the queued diffs — because
  // each queued update is an incremental diff, meaningful only relative to the
  // state it was generated against; uploading diffs alone would publish a blob
  // missing everything that predates this batch.
  private async flushMember(memberDid: string): Promise<void> {
    if (!this.id) return;
    const client = new SapClient(this.env, memberDid);
    const blob = await client.uploadBlob(Y.encodeStateAsUpdateV2(this.ydoc), "application/octet-stream");
    await client.call("network.habitat.space.putRecord", "POST", {
      space: this.id.spaceUri,
      repo: memberDid,
      collection: CRDT_COLLECTION,
      rkey: SELF,
      record: { blob: blob.blob },
    });
  }
```

Call `await this.schedule("member", memberDid)` at the end of `applyEdit`. Define `const CRDT_COLLECTION = "network.habitat.docs.crdt"` and `const SELF = "self"` at module top. Import `and, eq` from `drizzle-orm`.

Note this drops today's separate queue of update *bytes*: because the room already holds the merged doc, uploading `this.ydoc` is both simpler and strictly more correct than replaying stored diffs.

- [ ] **Step 7: Run test to verify it passes**

Run: `pnpm --filter chalk test test/docRoomFlush.test.ts`
Expected: PASS (4 tests).

- [ ] **Step 8: Rewire `sendEdit`**

```ts
export const sendEdit = createServerFn({ method: "POST" })
  .validator((input: { docId: string; update: Uint8Array }) => input)
  .handler(async ({ data }) => {
    const { did } = await requireSession();
    // One RPC, not two: the immediate fanout and the debounced repo
    // writeback both live in the same object now.
    await env.DOC.get(env.DOC.idFromName(data.docId))
      .applyEdit({ spaceUri: data.docId }, did, data.update);
  });
```

- [ ] **Step 9: Verify and commit**

Run: `pnpm --filter chalk test`
Expected: PASS.

```bash
git add typescript/apps/chalk
git commit -m "[Chalk] Replace setTimeout debounce with DO alarms for member edits"
```

---

### Task 6: Owner-canonical republish

**Files:**
- Modify: `typescript/apps/chalk/src/server/rooms/docRoom.ts`
- Test: `typescript/apps/chalk/test/docRoomRepublish.test.ts`

**Interfaces:**
- Consumes: `schedule`/`alarm`/`flushMember` from Task 5; `renderDoc` from `src/render.ts`; `upsertDoc`/`getDb` from Task 2.
- Produces: `DocRoom`'s `"owner"` flush branch, writing both `network.habitat.docs.crdt` and `network.habitat.docs.markdown` under the owner's repo and updating the D1 title.

- [ ] **Step 1: Write the failing test**

```ts
// test/docRoomRepublish.test.ts
import { env, runInDurableObject, runDurableObjectAlarm } from "cloudflare:test";
import { beforeEach, expect, it, vi } from "vitest";
import * as Y from "yjs";
import type { DocRoom } from "../src/server/rooms/docRoom";
import { getDb, docByUri, upsertDoc } from "../src/db";

const URI = "at://did:web:alice.example/space/network.habitat.docs/pub";

const fetchMock = vi.fn();
beforeEach(async () => {
  fetchMock.mockReset();
  fetchMock.mockResolvedValue(new Response(JSON.stringify({ blob: { ref: { $link: "c" } }, cid: "c" })));
  vi.stubGlobal("fetch", fetchMock);
  await env.DB.exec("DELETE FROM docs");
});

function headingUpdate(text: string): Uint8Array {
  const d = new Y.Doc();
  d.getText("body").insert(0, text);
  return Y.encodeStateAsUpdateV2(d);
}

it("republishes crdt and markdown under the owner's repo", async () => {
  const stub = env.DOC.get(env.DOC.idFromName(URI));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit({ spaceUri: URI, ownerDid: "did:web:alice.example" }, "did:web:bob.example", headingUpdate("Title")),
  );
  await runDurableObjectAlarm(stub);
  const puts = fetchMock.mock.calls
    .filter((c) => String(c[0]).includes("space.putRecord"))
    .map((c) => JSON.parse(String(c[1].body)));
  const ownerPuts = puts.filter((p) => p.repo === "did:web:alice.example");
  expect(ownerPuts.map((p) => p.collection).sort()).toEqual([
    "network.habitat.docs.crdt",
    "network.habitat.docs.markdown",
  ]);
});

it("skips republish when the owner is unknown", async () => {
  const uri = URI + "-2";
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit({ spaceUri: uri }, "did:web:bob.example", headingUpdate("x")),
  );
  await runDurableObjectAlarm(stub);
  const repos = fetchMock.mock.calls
    .filter((c) => String(c[0]).includes("space.putRecord"))
    .map((c) => JSON.parse(String(c[1].body)).repo);
  expect(repos).toEqual(["did:web:bob.example"]);
});

it("writes the rendered title back to the D1 index", async () => {
  const uri = URI + "-3";
  await upsertDoc(getDb(env), {
    spaceUri: uri, docId: uri, ownerDid: "did:web:alice.example", title: "Untitled",
  });
  const stub = env.DOC.get(env.DOC.idFromName(uri));
  await runInDurableObject(stub, (r: DocRoom) =>
    r.applyEdit({ spaceUri: uri, ownerDid: "did:web:alice.example" }, "did:web:bob.example", headingUpdate("Hello")),
  );
  await runDurableObjectAlarm(stub);
  expect((await docByUri(getDb(env), uri))?.title).not.toBe("Untitled");
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter chalk test test/docRoomRepublish.test.ts`
Expected: FAIL — only the member put is issued; no owner put exists.

- [ ] **Step 3: Implement the owner branch**

```ts
  // republishCanonical writes the merged markdown + CRDT snapshot back under
  // the doc owner's own repo. This is itself a network.habitat.docs.crdt
  // write, so it comes back through SapChannel looking like a fresh delta.
  // It does not loop: pear's PutRecord skips advancing the record (and the
  // notifyWrite it would cause) when a write does not change the record's
  // CID (see internal/spaces/store.go), so a no-op republish produces no
  // outbox event.
  private async republishCanonical(): Promise<void> {
    const ownerDid = this.id?.ownerDid;
    if (!this.id || !ownerDid) return;
    const rendered = renderDoc(this.ydoc);
    const client = new SapClient(this.env, ownerDid);
    const blob = await client.uploadBlob(
      Y.encodeStateAsUpdateV2(this.ydoc), "application/octet-stream",
    );
    await client.call("network.habitat.space.putRecord", "POST", {
      space: this.id.spaceUri, repo: ownerDid,
      collection: CRDT_COLLECTION, rkey: SELF, record: { blob: blob.blob },
    });
    await client.call("network.habitat.space.putRecord", "POST", {
      space: this.id.spaceUri, repo: ownerDid,
      collection: MARKDOWN_COLLECTION, rkey: SELF,
      record: { title: rendered.title, content: rendered.markdown },
    });
    const existing = await docByUri(getDb(this.env), this.id.spaceUri);
    await upsertDoc(getDb(this.env), {
      spaceUri: this.id.spaceUri,
      docId: existing?.docId ?? this.id.spaceUri,
      ownerDid,
      title: rendered.title,
    });
  }
```

Add `const MARKDOWN_COLLECTION = "network.habitat.docs.markdown";` at module top, import `renderDoc` from `../../render` and `getDb, docByUri, upsertDoc` from `../../db`. In `alarm()`, replace the `// "owner" is handled in Task 6.` comment with `if (row.kind === "owner") await this.republishCanonical();`. In `mergeUpdate`, after `broadcast()`, call `await this.schedule("owner", "")`.

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm --filter chalk test test/docRoomRepublish.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add typescript/apps/chalk
git commit -m "[Chalk] Republish owner-canonical snapshot from DocRoom alarm"
```

---

### Task 7: `SapChannel` DO — connect, route, ack

**Files:**
- Create: `typescript/apps/chalk/src/server/rooms/sapChannel.ts`
- Create: `typescript/apps/chalk/src/server/spaceUri.ts`
- Modify: `typescript/apps/chalk/src/server/rooms/docRoom.ts`
- Modify: `typescript/apps/chalk/wrangler.jsonc`
- Test: `typescript/apps/chalk/test/spaceUri.test.ts`, `typescript/apps/chalk/test/sapChannel.test.ts`

**Interfaces:**
- Consumes: `DocRoom.applyRemote` (Task 3), `docByUri` (Task 2), `SapClient.getBlob` (Task 5's `env` form).
- Produces:
  - `parseSpaceRecordUri(uri): ParsedSpaceRecordUri | undefined` moved verbatim from `docSync.ts` into `src/server/spaceUri.ts`, along with `OutboxMessage`
  - `class SapChannel extends DurableObject<Env>` with `ensureConnected(): Promise<void>` and `handleOutboxMessage(msg: OutboxMessage): Promise<void>`
  - binding name `SAP`

- [ ] **Step 1: Move `parseSpaceRecordUri` and its tests**

Move `parseSpaceRecordUri`, `ParsedSpaceRecordUri`, and `OutboxMessage` from `src/server/docSync.ts` into `src/server/spaceUri.ts` unchanged, comments included. Move the corresponding cases out of `test/docSync.test.ts` into `test/spaceUri.test.ts`, changing only the import path.

Run: `pnpm --filter chalk test test/spaceUri.test.ts`
Expected: PASS — this is a pure move, so it should be green immediately.

- [ ] **Step 2: Write the failing routing test**

```ts
// test/sapChannel.test.ts
import { env, runInDurableObject } from "cloudflare:test";
import { beforeEach, expect, it, vi } from "vitest";
import type { SapChannel } from "../src/server/rooms/sapChannel";
import { getDb, upsertDoc } from "../src/db";

const OWNER = "did:web:alice.example";
const URI = `at://${OWNER}/space/network.habitat.docs/abc`;
const RECORD = `${URI}/did:web:bob.example/network.habitat.docs.crdt/self`;

const fetchMock = vi.fn();
beforeEach(async () => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  await env.DB.exec("DELETE FROM docs");
  await upsertDoc(getDb(env), { spaceUri: URI, docId: URI, ownerDid: OWNER, title: "Untitled" });
});

function msg(uri: string, cid: string | undefined) {
  return { id: 1, uri, value: cid ? { blob: { ref: { $link: cid } } } : {} };
}

it("routes a crdt record to its doc room", async () => {
  const stub = env.SAP.get(env.SAP.idFromName("default"));
  fetchMock.mockResolvedValue(new Response(new Uint8Array([0, 0])));
  await runInDurableObject(stub, (c: SapChannel) => c.handleOutboxMessage(msg(RECORD, "cid1")));
  expect(fetchMock.mock.calls.some((c) => String(c[0]).includes("space.getBlob"))).toBe(true);
});

it("ignores a record in a different collection", async () => {
  const stub = env.SAP.get(env.SAP.idFromName("default"));
  const other = `${URI}/did:web:bob.example/network.habitat.docs.markdown/self`;
  await runInDurableObject(stub, (c: SapChannel) => c.handleOutboxMessage(msg(other, "cid1")));
  expect(fetchMock).not.toHaveBeenCalled();
});

it("ignores a doc absent from the index", async () => {
  const stub = env.SAP.get(env.SAP.idFromName("default"));
  const unknown = `at://${OWNER}/space/network.habitat.docs/zzz/did:web:bob.example/network.habitat.docs.crdt/self`;
  await runInDurableObject(stub, (c: SapChannel) => c.handleOutboxMessage(msg(unknown, "cid1")));
  expect(fetchMock).not.toHaveBeenCalled();
});

it("ignores a record with no blob reference", async () => {
  const stub = env.SAP.get(env.SAP.idFromName("default"));
  await runInDurableObject(stub, (c: SapChannel) => c.handleOutboxMessage(msg(RECORD, undefined)));
  expect(fetchMock).not.toHaveBeenCalled();
});

it("ignores a malformed uri", async () => {
  const stub = env.SAP.get(env.SAP.idFromName("default"));
  await runInDurableObject(stub, (c: SapChannel) => c.handleOutboxMessage(msg("not-a-uri", "cid1")));
  expect(fetchMock).not.toHaveBeenCalled();
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `pnpm --filter chalk test test/sapChannel.test.ts`
Expected: FAIL — `../src/server/rooms/sapChannel` not found.

- [ ] **Step 4: Complete `DocRoom.applyRemote`**

```ts
  async applyRemote(id: DocIdentity, cid: string): Promise<void> {
    await this.rememberIdentity(id);
    // A putRecord'd network.habitat.docs.crdt record carries a blob
    // *reference*, not the update's bytes inline, so the bytes have to be
    // fetched separately via getBlob.
    const ownerDid = this.id?.ownerDid;
    if (!ownerDid) return;
    const bytes = await new SapClient(this.env, ownerDid).getBlob(id.spaceUri, cid);
    await this.mergeUpdate(bytes);
  }
```

- [ ] **Step 5: Write `SapChannel`**

```ts
// src/server/rooms/sapChannel.ts
import { DurableObject } from "cloudflare:workers";
import { docByUri, getDb } from "../../db";
import { parseSpaceRecordUri, type OutboxMessage } from "../spaceUri";

const CRDT_COLLECTION = "network.habitat.docs.crdt";
const RECONNECT_DELAY_MS = 2000;

export class SapChannel extends DurableObject<Env> {
  private ws: WebSocket | undefined;

  // ensureConnected is idempotent: it is called both by the cron trigger and
  // by the reconnect alarm, and no-ops while a socket is live.
  async ensureConnected(): Promise<void> {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) return;
    const base = this.env.CHALK_SAP_INTERNAL_URL;
    if (!base) throw new Error("CHALK_SAP_INTERNAL_URL is not set");
    const res = await fetch(`${base.replace(/^http/, "ws")}/channel`, {
      headers: { Upgrade: "websocket" },
    });
    const ws = res.webSocket;
    if (!ws) throw new Error("sap did not upgrade to a websocket");
    ws.accept();
    this.ws = ws;
    ws.addEventListener("message", (ev) => {
      void this.onMessage(ws, typeof ev.data === "string" ? ev.data : String(ev.data));
    });
    ws.addEventListener("close", () => {
      if (this.ws === ws) this.ws = undefined;
      void this.ctx.storage.setAlarm(Date.now() + RECONNECT_DELAY_MS);
    });
  }

  async alarm(): Promise<void> {
    await this.ensureConnected();
  }

  private async onMessage(ws: WebSocket, data: string): Promise<void> {
    let msg: OutboxMessage;
    try {
      msg = JSON.parse(data) as OutboxMessage;
    } catch (err) {
      console.error("[sapChannel] malformed message", data, err);
      return;
    }
    try {
      await this.handleOutboxMessage(msg);
    } catch (err) {
      // Do not ack: sap redelivers, and re-applying a Yjs update already
      // merged is a no-op, so at-least-once is safe here.
      console.error("[sapChannel] handle message", err);
      return;
    }
    if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ id: msg.id }));
  }

  // handleOutboxMessage routes one outbox event. Messages this deliberately
  // ignores (wrong collection, unknown doc, missing blob ref) return normally
  // so they are acked immediately — one uninteresting message must not be
  // able to wedge the outbox.
  async handleOutboxMessage(msg: OutboxMessage): Promise<void> {
    const parsed = parseSpaceRecordUri(msg.uri);
    if (!parsed || parsed.collection !== CRDT_COLLECTION) return;
    const value = (msg.value ?? {}) as { blob?: { ref?: { $link?: string } } };
    const cid = value.blob?.ref?.$link;
    if (!cid) return;
    const doc = await docByUri(getDb(this.env), parsed.spaceUri);
    if (!doc) return; // this deployment does not know this doc
    const stub = this.env.DOC.get(this.env.DOC.idFromName(parsed.spaceUri));
    await stub.applyRemote({ spaceUri: parsed.spaceUri, ownerDid: doc.ownerDid }, cid);
  }
}
```

- [ ] **Step 6: Add the binding**

Add `{ "name": "SAP", "class_name": "SapChannel" }` to `durable_objects.bindings` and `"SapChannel"` to `new_sqlite_classes`. Export the class from the Worker entry.

- [ ] **Step 7: Run test to verify it passes**

Run: `pnpm --filter chalk test test/sapChannel.test.ts test/spaceUri.test.ts`
Expected: PASS (5 + moved cases).

- [ ] **Step 8: Commit**

```bash
git add typescript/apps/chalk
git commit -m "[Chalk] Add SapChannel durable object routing outbox events to doc rooms"
```

---

### Task 8: `SapChannel` lifecycle via cron

Workers have no module-load moment to start a subscription from, and a DO whose socket has closed can be evicted with nothing left to wake it.

**Files:**
- Modify: `typescript/apps/chalk/wrangler.jsonc`
- Modify: the Worker entry established in Task 1
- Test: `typescript/apps/chalk/test/sapChannelLifecycle.test.ts`

**Interfaces:**
- Consumes: `SapChannel.ensureConnected` from Task 7.
- Produces: a `scheduled` handler calling `ensureConnected()` on `SAP.idFromName("default")`.

- [ ] **Step 1: Write the failing test**

```ts
// test/sapChannelLifecycle.test.ts
import { env, runInDurableObject } from "cloudflare:test";
import { beforeEach, expect, it, vi } from "vitest";
import type { SapChannel } from "../src/server/rooms/sapChannel";

const fetchMock = vi.fn();
beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

function fakeSocket() {
  return {
    readyState: WebSocket.OPEN,
    accept: vi.fn(),
    addEventListener: vi.fn(),
    send: vi.fn(),
  } as unknown as WebSocket;
}

it("connects once and no-ops while the socket is open", async () => {
  const stub = env.SAP.get(env.SAP.idFromName("default"));
  const res = new Response(null, { status: 101 });
  Object.defineProperty(res, "webSocket", { value: fakeSocket() });
  fetchMock.mockResolvedValue(res);
  await runInDurableObject(stub, async (c: SapChannel) => {
    await c.ensureConnected();
    await c.ensureConnected();
  });
  expect(fetchMock).toHaveBeenCalledTimes(1);
});

it("targets sap's /channel over ws", async () => {
  const stub = env.SAP.get(env.SAP.idFromName("lifecycle-2"));
  const res = new Response(null, { status: 101 });
  Object.defineProperty(res, "webSocket", { value: fakeSocket() });
  fetchMock.mockResolvedValue(res);
  await runInDurableObject(stub, (c: SapChannel) => c.ensureConnected());
  expect(String(fetchMock.mock.calls[0]![0])).toMatch(/^ws:\/\/.*\/channel$/);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter chalk test test/sapChannelLifecycle.test.ts`
Expected: FAIL if `ensureConnected` reconnects unconditionally; PASS on the second test. Fix the guard until both pass.

- [ ] **Step 3: Add the cron trigger**

In `wrangler.jsonc`:

```jsonc
"triggers": { "crons": ["* * * * *"] }
```

- [ ] **Step 4: Add the scheduled handler**

In the Worker entry, beside the Nitro default export:

```ts
export async function scheduled(_c: ScheduledController, env: Env, ctx: ExecutionContext) {
  // The cron is what guarantees recovery from eviction; SapChannel's own
  // close-alarm only makes ordinary reconnects fast rather than up to a
  // minute. Both are idempotent.
  ctx.waitUntil(env.SAP.get(env.SAP.idFromName("default")).ensureConnected());
}
```

If Task 1's mechanism made the entry a Nitro-owned module, use Nitro's `cloudflare:scheduled` hook instead, which the preset exposes (`nitro/dist/types` lists it) — the body is the same.

- [ ] **Step 5: Verify against a live sap**

Run `moon chalk:dev` (which starts `sap:dev` and `pear:dev`). Confirm the log shows a connection to `/channel` within a minute of boot, and that killing sap produces reconnect attempts rather than silence.

- [ ] **Step 6: Commit**

```bash
git add typescript/apps/chalk
git commit -m "[Chalk] Keep SapChannel connected via cron trigger and reconnect alarm"
```

---

### Task 9: Delete the Node-era machinery and finish the migration

**Files:**
- Delete: `src/server/docSync.ts`, `src/server/pubsub.ts`, `src/server/debounceQueue.ts`, `src/server/docStore.ts`
- Delete: `test/docSync.test.ts`, `test/pubsub.test.ts`, `test/debounceQueue.test.ts`, `test/docStore.test.ts`, `test/ping.test.ts`, `src/server/rooms/ping.ts`
- Modify: `src/server/functions.server.ts`, `typescript/apps/chalk/moon.yml`, `typescript/apps/chalk/README.md`, `typescript/apps/chalk/.gitignore`
- Modify: `typescript/apps/chalk/test/functions.server.test.ts`

**Interfaces:**
- Consumes: everything from Tasks 1-8.
- Produces: no module-global singletons anywhere in chalk.

- [ ] **Step 1: Delete the superseded modules and their tests**

```bash
cd typescript/apps/chalk
git rm src/server/docSync.ts src/server/pubsub.ts src/server/debounceQueue.ts src/server/docStore.ts
git rm test/docSync.test.ts test/pubsub.test.ts test/debounceQueue.test.ts test/docStore.test.ts
git rm test/ping.test.ts src/server/rooms/ping.ts
```

`pubsub.ts` is replaced by `DocRoom`'s subscriber set; `debounceQueue.ts` by `DocRoom`'s alarm; `docStore.ts` by D1 plus `doc_state`; `docSync.ts` by `SapChannel` plus `DocRoom`. `DocSync`'s `private queue: Promise<void>` has no replacement and needs none — a DO instance is single-threaded, so per-doc ordering is a platform guarantee.

Also remove the `PING` binding and its migration entry from `wrangler.jsonc`.

- [ ] **Step 2: Gut `functions.server.ts`**

Delete `createSingletons`, the `ChalkSingletons` interface, the `declare global` block, the `globalThis.__chalkSingletons ??=` export, and the `import.meta.hot.dispose` block — including their long comments, which describe a Vite-in-one-Node-process problem that no longer exists. Keep `requireSession`, `DOCS_SPACE_TYPE`, and `CRDT_COLLECTION`. Delete `memberEditKey`/`splitMemberEditKey`: the (docId, member) composite key existed because one process keyed a global queue by both, and `DocRoom` is already scoped to one doc.

- [ ] **Step 3: Update `functions.server.test.ts`**

Drop the cases covering `memberEditKey` and the singleton wiring; keep the `requireSession` cases.

Run: `pnpm --filter chalk test`
Expected: PASS.

- [ ] **Step 4: Update `moon.yml`**

Remove `NODE_OPTIONS: "--use-system-ca"` (workerd does not consult the system CA store, and sap traffic goes to plain `http://127.0.0.1:2581`, not through Caddy) and `CHALK_DB` (no sqlite file any more). Move `CHALK_BASE_URL`, `CHALK_DOMAIN`, `CHALK_SAP_INTERNAL_URL`, and `CHALK_SAP_PUBLIC_URL` into `wrangler.jsonc`'s `vars`. Keep `SERVER_PORT: "5177"` and the `deps` on `pear:dev`, `sap:dev`, `root:caddy` unchanged.

`CHALK_SESSION_SECRET` becomes a wrangler secret (`.dev.vars` locally); add `.dev.vars` to `.gitignore` and never commit it.

- [ ] **Step 5: Update the README**

Replace the "self-contained Node server / push `dist/`" deployment section with the Workers deployment: `wrangler deploy`, the D1 database, and the DO migrations. Add a prominent note that this app is **not deployable to real Workers until sap reachability and `Habitat-Did` authentication are solved** (spec, "Out of scope" #1).

- [ ] **Step 6: Full verification**

Run each and confirm output before claiming done:

```bash
pnpm --filter chalk test
pnpm --filter chalk build
moon chalk:lint-check
```

Then `moon chalk:dev` and exercise by hand: log in, create a doc, type, open the same doc in a second tab and confirm it updates, reload and confirm content survives, wait ~10s and confirm a `putRecord` reaches pear.

- [ ] **Step 7: Commit**

```bash
git add -A typescript/apps/chalk
git commit -m "[Chalk] Remove Node-era sync singletons superseded by durable objects"
```

---

## Notes for the executor

- **Task 1 is a gate.** If the custom-DO export cannot be made to work, stop and report; do not improvise a different architecture.
- Tasks 2-8 each leave the app working. Task 9 is the only one that deletes.
- Where a comment is moved from old code to new, move it rather than paraphrasing. The comments about V2 wire format, incremental-vs-snapshot seeding, and the no-op-republish loop each document a real hazard that was hit once already.

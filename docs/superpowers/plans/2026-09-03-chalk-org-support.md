# Chalk Org Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a chalk member connect an opensocial org and create/list docs under it, alongside the existing personal mode, with docs org-wide visible (no per-doc sharing within an org yet).

**Architecture:** Pure TypeScript change in `typescript/apps/chalk/` — no Go/backend changes. Reuses sap's existing generic `/session/add` (passing an org DID as the "handle") to drive the opensocial admin sign-in flow built earlier, and pear's existing `Atproto-Proxy` service-forwarding to let a member act "into" an org's space without sap ever holding a separate org session.

**Tech Stack:** TypeScript, TanStack Start (Cloudflare Workers), Drizzle ORM + D1, Vitest + `@cloudflare/vitest-pool-workers` + msw.

**Spec:** `docs/superpowers/specs/2026-09-03-chalk-org-support-design.md`

## Global Constraints

- No Go/backend changes — everything here is `typescript/apps/chalk/`.
- Org docs are visible to every member of the org, with no per-doc sharing (no `doc_access` rows for org docs).
- No new env vars (no fixed "personal space authority" — dropped in the spec's later revision).
- Follow the existing separation: `functions.ts` holds only thin `createServerFn` wrappers; testable logic lives in `functions.server.ts` (plain functions) or `db/index.ts`, per the existing pattern (`hasDocAccess`/`docRole`/`docsForAccessor` are already split out this way).
- Run `pnpm --filter chalk db:generate` after any `schema.ts` change to produce the matching migration SQL, and `pnpm --filter chalk db:migrate-local` before running tests locally (the `test`/`dev` scripts already do this).
- Test with `pnpm --filter chalk test` (vitest, workers pool, D1 auto-migrated per `vitest.config.ts`).

---

## File Structure

- `typescript/apps/chalk/src/db/schema.ts` — modify: add `isOrg` to `docs`, add `connectedOrgs` table.
- `typescript/apps/chalk/src/db/index.ts` — modify: `DocSummary` gets `isOrg`; `docsForAccessor` branches by org mode; add `upsertConnectedOrg`.
- `typescript/apps/chalk/test/docsIndex.test.ts` — modify: cover the new branch and `isOrg` field.
- `typescript/apps/chalk/src/server/session.ts` — modify: `ChalkSessionData` gets `currentOrg?: string`.
- `typescript/apps/chalk/test/session.test.ts` — no change needed (generic `useSession` wrapper, doesn't need to know about `currentOrg`).
- `typescript/apps/chalk/src/server/sapClient.ts` — modify: `SapClient.call()` takes an optional `atprotoProxy` DID to set the `Atproto-Proxy` header.
- `typescript/apps/chalk/test/sapClient.test.ts` — modify: cover the new header.
- `typescript/apps/chalk/src/server/functions.server.ts` — modify: add `createDocSpace` (the mode-branching space-creation logic) and `fetchOrgName` (member-scoped, `Atproto-Proxy`'d profile read).
- `typescript/apps/chalk/test/functions.server.test.ts` — modify: cover both new functions.
- `typescript/apps/chalk/src/server/functions.ts` — modify: `createDoc` uses `createDocSpace` and skips `upsertDocAccess` in org mode; `listDocs` resolves the session's `currentOrg`; add `listMyOrgs`, `startOrgConnect`, `getDoc`.
- `typescript/apps/chalk/src/routes/orgs.tsx` — create: the org-picker page.
- `typescript/apps/chalk/src/routes/session.org-callback.tsx` — create: the org-connect callback (parallel to `session.callback.tsx`).
- `typescript/apps/chalk/src/routes/_requireAuth.tsx` — modify: sidebar shows current mode (Personal/org name) and a link to `/orgs`.
- `typescript/apps/chalk/src/routes/_requireAuth/$uri.tsx` — modify: hide `ShareDialog` for org docs.

---

### Task 1: `isOrg` column and `connectedOrgs` table

**Files:**
- Modify: `typescript/apps/chalk/src/db/schema.ts`
- Modify: `typescript/apps/chalk/src/db/index.ts`
- Modify: `typescript/apps/chalk/test/docsIndex.test.ts`

**Interfaces:**
- Produces: `docs.isOrg` (boolean column), `connectedOrgs` table (`memberDid`, `orgDid`, `orgName`, `connectedAt`), `DocSummary.isOrg: boolean`, `upsertConnectedOrg(db, {memberDid, orgDid, orgName}): Promise<void>`.

- [ ] **Step 1: Write the failing test** — add to `typescript/apps/chalk/test/docsIndex.test.ts`, right after the existing `it("upserts on conflict...")` block:

```typescript
it("stamps isOrg on the row and reflects it back", async () => {
  const db = getDb(env);
  await upsertDoc(db, {
    spaceUri: URI,
    docId: URI,
    ownerDid: "did:web:org.example",
    title: "Untitled",
    isOrg: true,
  });
  expect((await docByUri(db, URI))?.isOrg).toBe(true);
});

it("defaults isOrg to false when not given", async () => {
  const db = getDb(env);
  await upsertDoc(db, {
    spaceUri: URI,
    docId: URI,
    ownerDid: ALICE,
    title: "Untitled",
  });
  expect((await docByUri(db, URI))?.isOrg).toBe(false);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter chalk test -- docsIndex`
Expected: FAIL — `isOrg` doesn't exist on the `upsertDoc` doc argument / isn't returned by `docByUri`.

- [ ] **Step 3: Add the column and table to the schema**

In `typescript/apps/chalk/src/db/schema.ts`, add `isOrg` to `docs` and a new `connectedOrgs` table:

```typescript
export const docs = sqliteTable(
  "docs",
  {
    spaceUri: text("space_uri").primaryKey(),
    docId: text("doc_id").notNull().unique(),
    ownerDid: text("owner_did").notNull(),
    title: text("title").notNull(),
    updatedAt: integer("updated_at").notNull(),
    isOrg: integer("is_org", { mode: "boolean" }).notNull().default(false),
  },
  (t) => [index("docs_owner_updated").on(t.ownerDid, t.updatedAt)],
);
```

Add this new table after `docAccess`:

```typescript
// connectedOrgs records that a member has successfully connected an org
// (see session.org-callback.tsx) — chalk's own audit trail, separate from
// the live "orgs I'm a member of" list, which is always fetched fresh from
// pear rather than cached here.
export const connectedOrgs = sqliteTable(
  "connected_orgs",
  {
    memberDid: text("member_did").notNull(),
    orgDid: text("org_did").notNull(),
    orgName: text("org_name").notNull(),
    connectedAt: integer("connected_at").notNull(),
  },
  (t) => [primaryKey({ columns: [t.memberDid, t.orgDid] })],
);
```

- [ ] **Step 4: Generate and apply the migration**

Run: `pnpm --filter chalk db:generate`
Expected: a new file appears under `typescript/apps/chalk/drizzle/`, containing an `ALTER TABLE docs ADD isOrg` and a `CREATE TABLE connected_orgs` statement. Inspect it — it should not touch `doc_access` or existing `docs` columns.

Run: `pnpm --filter chalk db:migrate-local`
Expected: applies cleanly, no errors.

- [ ] **Step 5: Update `db/index.ts`'s types and functions**

In `typescript/apps/chalk/src/db/index.ts`:

```typescript
import { drizzle } from "drizzle-orm/d1";
import { desc, eq } from "drizzle-orm";
import { docs, docAccess, connectedOrgs } from "./schema";

export interface DocSummary {
  docId: string;
  uri: string;
  ownerDid: string;
  title: string;
  isOrg: boolean;
}

export function getDb(env: { DB: D1Database }) {
  return drizzle(env.DB, { schema: { docs, docAccess, connectedOrgs } });
}

export type Db = ReturnType<typeof getDb>;

export async function upsertDoc(
  db: Db,
  doc: {
    spaceUri: string;
    docId: string;
    ownerDid: string;
    title: string;
    isOrg?: boolean;
  },
): Promise<void> {
  const row = { ...doc, isOrg: doc.isOrg ?? false, updatedAt: Date.now() };
  await db
    .insert(docs)
    .values(row)
    .onConflictDoUpdate({
      target: docs.spaceUri,
      set: {
        docId: row.docId,
        ownerDid: row.ownerDid,
        title: row.title,
        isOrg: row.isOrg,
        updatedAt: row.updatedAt,
      },
    });
}

function toSummary(r: typeof docs.$inferSelect): DocSummary {
  return {
    docId: r.docId,
    uri: r.spaceUri,
    ownerDid: r.ownerDid,
    title: r.title,
    isOrg: r.isOrg,
  };
}
```

Leave `docsForAccessor` as-is for now (Task 2 changes it) but update its `.select({...})` projection to also select `docs.isOrg`, and `toSummary`'s call sites already cover `docByUri`. Update the `docsForAccessor` select block:

```typescript
export async function docsForAccessor(
  db: Db,
  subjectDid: string,
): Promise<DocSummary[]> {
  const rows = await db
    .select({
      spaceUri: docs.spaceUri,
      docId: docs.docId,
      ownerDid: docs.ownerDid,
      title: docs.title,
      updatedAt: docs.updatedAt,
      isOrg: docs.isOrg,
    })
    .from(docs)
    .innerJoin(docAccess, eq(docs.spaceUri, docAccess.spaceUri))
    .where(eq(docAccess.subjectDid, subjectDid))
    .orderBy(desc(docs.updatedAt));
  return rows.map(toSummary);
}
```

Add `upsertConnectedOrg` at the end of the file:

```typescript
// upsertConnectedOrg records that memberDid successfully connected orgDid
// (see session.org-callback.tsx) — an audit trail, not consulted by any
// read path in this plan.
export async function upsertConnectedOrg(
  db: Db,
  connection: { memberDid: string; orgDid: string; orgName: string },
): Promise<void> {
  const row = { ...connection, connectedAt: Date.now() };
  await db
    .insert(connectedOrgs)
    .values(row)
    .onConflictDoUpdate({
      target: [connectedOrgs.memberDid, connectedOrgs.orgDid],
      set: { orgName: row.orgName, connectedAt: row.connectedAt },
    });
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `pnpm --filter chalk test -- docsIndex`
Expected: PASS (all tests in the file, including the two existing ones — `toEqual` comparisons on `DocSummary` now include `isOrg: false`, so double check the two `toEqual` assertions written before this task still pass; they will, since `isOrg` defaults to `false` and the objects only assert specific keys via `toEqual` on the full object — if `toEqual` fails on the pre-existing tests because they don't list `isOrg`, add `isOrg: false` to those two expected objects).

- [ ] **Step 7: Commit**

```bash
git add typescript/apps/chalk/src/db/schema.ts typescript/apps/chalk/src/db/index.ts typescript/apps/chalk/test/docsIndex.test.ts typescript/apps/chalk/drizzle
git commit -m "chalk: add docs.isOrg and connectedOrgs table"
```

---

### Task 2: `SapClient.call()` gets an `Atproto-Proxy` option

**Files:**
- Modify: `typescript/apps/chalk/src/server/sapClient.ts`
- Modify: `typescript/apps/chalk/test/sapClient.test.ts`

**Interfaces:**
- Consumes: nothing new.
- Produces: `SapClient.call<T>(nsid, method, payload, opts?: { atprotoProxy?: string })`.

- [ ] **Step 1: Write the failing test** — add to `typescript/apps/chalk/test/sapClient.test.ts`, inside the `describe("SapClient", ...)` block:

```typescript
it("sets Atproto-Proxy when given an atprotoProxy target", async () => {
  let headers: Headers | undefined;
  server.use(
    http.post(
      "http://sap-internal.test/proxy/community.opensocial.createSpace",
      ({ request }) => {
        headers = request.headers;
        return HttpResponse.json({ uri: "at://did:web:org.example/x" });
      },
    ),
  );
  const client = new SapClient(testEnv, "did:plc:member1");
  await client.call(
    "community.opensocial.createSpace",
    "POST",
    { org: "did:web:org.example", type: "network.habitat.docs" },
    { atprotoProxy: "did:web:org.example#habitat" },
  );
  expect(headers?.get("Atproto-Proxy")).toBe("did:web:org.example#habitat");
});

it("omits Atproto-Proxy when no atprotoProxy is given", async () => {
  let headers: Headers | undefined;
  server.use(
    http.get(
      "http://sap-internal.test/proxy/network.habitat.space.listRecords",
      ({ request }) => {
        headers = request.headers;
        return HttpResponse.json({ records: [] });
      },
    ),
  );
  const client = new SapClient(testEnv, "did:plc:member1");
  await client.call("network.habitat.space.listRecords", "GET", {});
  expect(headers?.has("Atproto-Proxy")).toBe(false);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter chalk test -- sapClient`
Expected: FAIL — `call()` doesn't accept a 4th argument (TypeScript will actually fail to compile this; that's the expected "RED" here since the test file won't even run).

- [ ] **Step 3: Implement the option** — in `typescript/apps/chalk/src/server/sapClient.ts`, change `call`'s signature:

```typescript
async call<T>(
  nsid: string,
  method: "GET" | "POST",
  payload: Record<string, unknown>,
  opts?: { atprotoProxy?: string },
): Promise<T> {
  const base = `${this.base}/proxy/${nsid}`;
  let url = base;
  let body: string | undefined;
  const headers: Record<string, string> = {
    [habitatDIDHeader]: this.did,
    ...sapAuthHeaders(this.env),
  };
  if (opts?.atprotoProxy) {
    headers["Atproto-Proxy"] = opts.atprotoProxy;
  }
  if (method === "GET") {
    const qs = new URLSearchParams();
    for (const [k, v] of Object.entries(payload)) {
      if (v !== undefined && v !== null) qs.set(k, String(v));
    }
    url = `${base}?${qs.toString()}`;
  } else {
    body = JSON.stringify(payload);
    headers["content-type"] = "application/json";
  }
  const res = await fetch(url, { method, body, headers });
  if (!res.ok) {
    throw new Error(`${nsid} failed (${res.status}): ${await res.text()}`);
  }
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm --filter chalk test -- sapClient`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add typescript/apps/chalk/src/server/sapClient.ts typescript/apps/chalk/test/sapClient.test.ts
git commit -m "chalk: add Atproto-Proxy support to SapClient.call"
```

---

### Task 3: `currentOrg` session field

**Files:**
- Modify: `typescript/apps/chalk/src/server/session.ts`

**Interfaces:**
- Produces: `ChalkSessionData.currentOrg?: string`.

No new test is needed here: `session.test.ts` only asserts `useSession`'s config args and that `.data.did` round-trips through the mocked session — it never enumerates `ChalkSessionData`'s keys, so adding an optional field doesn't change its behavior or need a new assertion. `requireSession`'s existing test in `functions.server.test.ts` (Task 4 touches that file next) is unaffected too, since it only reads `.did`.

- [ ] **Step 1: Add the field**

In `typescript/apps/chalk/src/server/session.ts`:

```typescript
export interface ChalkSessionData {
  did?: string;
  // The org DID the member is currently acting as, if any. Unset means
  // "Personal" mode — see docs/superpowers/specs/2026-09-03-chalk-org-support-design.md.
  currentOrg?: string;
}
```

- [ ] **Step 2: Typecheck**

Run: `pnpm --filter chalk exec tsc --noEmit`
Expected: no new errors.

- [ ] **Step 3: Commit**

```bash
git add typescript/apps/chalk/src/server/session.ts
git commit -m "chalk: add currentOrg to session data"
```

---

### Task 4: `createDocSpace` — the mode-branching space-creation logic

**Files:**
- Modify: `typescript/apps/chalk/src/server/functions.server.ts`
- Modify: `typescript/apps/chalk/test/functions.server.test.ts`

**Interfaces:**
- Consumes: `SapClient.call` with the `opts` param from Task 2.
- Produces: `createDocSpace(client: SapClient, did: string, currentOrg: string | undefined): Promise<{ uri: string; ownerDid: string; isOrg: boolean }>`.

- [ ] **Step 1: Write the failing tests** — add to `typescript/apps/chalk/test/functions.server.test.ts`, as a new `describe` block (this file will need `SapClient` and msw imports added — see the full new imports below):

```typescript
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";

// ... existing sessionData / vi.mock("../src/server/session", ...) block
// and the requireSession describe block stay as they are ...

const testEnv = {
  CHALK_SAP_INTERNAL_URL: "http://sap-internal.test",
} as Env;

describe("createDocSpace", () => {
  const server = setupServer();
  beforeEach(() => server.listen({ onUnhandledRequest: "error" }));
  afterEach(() => {
    server.resetHandlers();
    server.close();
  });

  it("creates a personal space via network.habitat.simplespace.createSpace", async () => {
    let body: unknown;
    let proxyHeader: string | null = null;
    server.use(
      http.post(
        "http://sap-internal.test/proxy/network.habitat.simplespace.createSpace",
        async ({ request }) => {
          body = await request.json();
          proxyHeader = request.headers.get("Atproto-Proxy");
          return HttpResponse.json({ uri: "at://did:plc:member1/space/x" });
        },
      ),
    );
    const { SapClient } = await import("../src/server/sapClient");
    const { createDocSpace } = await import("../src/server/functions.server");
    const client = new SapClient(testEnv, "did:plc:member1");
    const result = await createDocSpace(client, "did:plc:member1", undefined);
    expect(body).toEqual({
      did: "did:plc:member1",
      type: "network.habitat.docs",
    });
    expect(proxyHeader).toBeNull();
    expect(result).toEqual({
      uri: "at://did:plc:member1/space/x",
      ownerDid: "did:plc:member1",
      isOrg: false,
    });
  });

  it("creates an org space via community.opensocial.createSpace, proxied to the org", async () => {
    let body: unknown;
    let proxyHeader: string | null = null;
    server.use(
      http.post(
        "http://sap-internal.test/proxy/community.opensocial.createSpace",
        async ({ request }) => {
          body = await request.json();
          proxyHeader = request.headers.get("Atproto-Proxy");
          return HttpResponse.json({ uri: "at://did:web:org.example/space/x" });
        },
      ),
    );
    const { SapClient } = await import("../src/server/sapClient");
    const { createDocSpace } = await import("../src/server/functions.server");
    const client = new SapClient(testEnv, "did:plc:member1");
    const result = await createDocSpace(
      client,
      "did:plc:member1",
      "did:web:org.example",
    );
    expect(body).toEqual({
      org: "did:web:org.example",
      type: "network.habitat.docs",
      roles: ["admin", "member"],
    });
    expect(proxyHeader).toBe("did:web:org.example#habitat");
    expect(result).toEqual({
      uri: "at://did:web:org.example/space/x",
      ownerDid: "did:web:org.example",
      isOrg: true,
    });
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter chalk test -- functions.server`
Expected: FAIL — `createDocSpace` is not exported.

- [ ] **Step 3: Implement `createDocSpace`** — in `typescript/apps/chalk/src/server/functions.server.ts`, add (near `DOCS_SPACE_TYPE`, needs `SapClient` imported as a type):

```typescript
import type { SapClient } from "./sapClient";

// ... existing DOCS_SPACE_TYPE / CRDT_COLLECTION consts ...

// createDocSpace creates the underlying space for a new doc, branching on
// whether the member is in org mode (currentOrg set) or personal mode.
// client is always scoped to the member's own DID in both cases — org mode
// reaches the org's own createSpace endpoint via Atproto-Proxy rather than
// resuming a separate org session (see the design doc's "Connecting an
// org" section for why).
export async function createDocSpace(
  client: SapClient,
  did: string,
  currentOrg: string | undefined,
): Promise<{ uri: string; ownerDid: string; isOrg: boolean }> {
  if (currentOrg) {
    const created = await client.call<{ uri: string }>(
      "community.opensocial.createSpace",
      "POST",
      { org: currentOrg, type: DOCS_SPACE_TYPE, roles: ["admin", "member"] },
      { atprotoProxy: `${currentOrg}#habitat` },
    );
    return { uri: created.uri, ownerDid: currentOrg, isOrg: true };
  }
  const created = await client.call<{ uri: string }>(
    "network.habitat.simplespace.createSpace",
    "POST",
    { did, type: DOCS_SPACE_TYPE },
  );
  return { uri: created.uri, ownerDid: did, isOrg: false };
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm --filter chalk test -- functions.server`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add typescript/apps/chalk/src/server/functions.server.ts typescript/apps/chalk/test/functions.server.test.ts
git commit -m "chalk: add createDocSpace for personal/org doc-space creation"
```

---

### Task 5: `fetchOrgName` — member-scoped, proxied org profile read

**Files:**
- Modify: `typescript/apps/chalk/src/server/functions.server.ts`
- Modify: `typescript/apps/chalk/test/functions.server.test.ts`

**Interfaces:**
- Consumes: `SapClient.call` (Task 2), `constructSpaceURI` from `"internal"`.
- Produces: `fetchOrgName(client: SapClient, orgDid: string): Promise<string | null>`.

- [ ] **Step 1: Write the failing tests** — add to `typescript/apps/chalk/test/functions.server.test.ts`:

```typescript
describe("fetchOrgName", () => {
  const server = setupServer();
  beforeEach(() => server.listen({ onUnhandledRequest: "error" }));
  afterEach(() => {
    server.resetHandlers();
    server.close();
  });

  it("reads the org's profile record via a proxied getRecord call", async () => {
    let params: URLSearchParams | undefined;
    let proxyHeader: string | null = null;
    server.use(
      http.get(
        "http://sap-internal.test/proxy/network.habitat.space.getRecord",
        ({ request }) => {
          params = new URL(request.url).searchParams;
          proxyHeader = request.headers.get("Atproto-Proxy");
          return HttpResponse.json({ value: { name: "Acme Corp" } });
        },
      ),
    );
    const { SapClient } = await import("../src/server/sapClient");
    const { fetchOrgName } = await import("../src/server/functions.server");
    const client = new SapClient(testEnv, "did:plc:member1");
    const name = await fetchOrgName(client, "did:web:org.example");
    expect(name).toBe("Acme Corp");
    expect(proxyHeader).toBe("did:web:org.example#habitat");
    expect(params?.get("repo")).toBe("did:web:org.example");
    expect(params?.get("collection")).toBe("community.opensocial.profile");
    expect(params?.get("rkey")).toBe("self");
    expect(params?.get("space")).toBe(
      "at://did:web:org.example/space/community.opensocial.about/self",
    );
  });

  it("returns null when the read fails (not a member, org gone, etc.)", async () => {
    server.use(
      http.get(
        "http://sap-internal.test/proxy/network.habitat.space.getRecord",
        () => new HttpResponse("nope", { status: 400 }),
      ),
    );
    const { SapClient } = await import("../src/server/sapClient");
    const { fetchOrgName } = await import("../src/server/functions.server");
    const client = new SapClient(testEnv, "did:plc:member1");
    expect(await fetchOrgName(client, "did:web:org.example")).toBeNull();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter chalk test -- functions.server`
Expected: FAIL — `fetchOrgName` is not exported.

- [ ] **Step 3: Implement `fetchOrgName`** — add to `typescript/apps/chalk/src/server/functions.server.ts`:

```typescript
import { constructSpaceURI } from "internal";

// fetchOrgName reads an org's display name off its
// community.opensocial.profile record, via the member's own session
// proxied into the org's #habitat service (see createDocSpace's comment —
// same mechanism, same reason: no separate org session needed). Returns
// null if the caller isn't a member of the org, or the read otherwise
// fails, rather than throwing — callers show the raw DID as a fallback.
export async function fetchOrgName(
  client: SapClient,
  orgDid: string,
): Promise<string | null> {
  const aboutSpace = constructSpaceURI({
    spaceOwner: orgDid,
    spaceType: "community.opensocial.about",
    spaceKey: "self",
  });
  try {
    const { value } = await client.call<{ value: { name: string } }>(
      "network.habitat.space.getRecord",
      "GET",
      {
        space: aboutSpace,
        repo: orgDid,
        collection: "community.opensocial.profile",
        rkey: "self",
      },
      { atprotoProxy: `${orgDid}#habitat` },
    );
    return value.name;
  } catch {
    return null;
  }
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm --filter chalk test -- functions.server`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add typescript/apps/chalk/src/server/functions.server.ts typescript/apps/chalk/test/functions.server.test.ts
git commit -m "chalk: add fetchOrgName for member-scoped org profile reads"
```

---

### Task 6: Wire `createDoc` and `listDocs` to the new mode logic

**Files:**
- Modify: `typescript/apps/chalk/src/server/functions.ts`
- Modify: `typescript/apps/chalk/src/db/index.ts`
- Modify: `typescript/apps/chalk/test/docsIndex.test.ts`

**Interfaces:**
- Consumes: `createDocSpace` (Task 4), `ChalkSessionData.currentOrg` (Task 3), `docsForAccessor`/`DocSummary` (Task 1).
- Produces: `docsForOrg(db: Db, orgDid: string): Promise<DocSummary[]>`; `createDoc`/`listDocs` (unchanged export names/signatures — only their bodies change, so no other file needs to know).

- [ ] **Step 1: Write the failing test** — add to `typescript/apps/chalk/test/docsIndex.test.ts`:

```typescript
it("docsForOrg returns every doc owned by the org regardless of doc_access", async () => {
  const db = getDb(env);
  const orgDoc = "at://did:web:org.example/space/network.habitat.docs/abc";
  await upsertDoc(db, {
    spaceUri: orgDoc,
    docId: orgDoc,
    ownerDid: "did:web:org.example",
    title: "Org doc",
    isOrg: true,
  });
  // No doc_access row for this doc at all — org-mode listing must not
  // require one.
  const rows = await docsForOrg(db, "did:web:org.example");
  expect(rows).toEqual([
    {
      docId: orgDoc,
      uri: orgDoc,
      ownerDid: "did:web:org.example",
      title: "Org doc",
      isOrg: true,
    },
  ]);
});

it("docsForOrg excludes personal docs and other orgs' docs", async () => {
  const db = getDb(env);
  await upsertDoc(db, {
    spaceUri: URI,
    docId: URI,
    ownerDid: ALICE,
    title: "Personal doc",
  });
  const otherOrgDoc = "at://did:web:other.example/space/network.habitat.docs/xyz";
  await upsertDoc(db, {
    spaceUri: otherOrgDoc,
    docId: otherOrgDoc,
    ownerDid: "did:web:other.example",
    title: "Other org's doc",
    isOrg: true,
  });
  expect(await docsForOrg(db, "did:web:org.example")).toEqual([]);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter chalk test -- docsIndex`
Expected: FAIL — `docsForOrg` is not exported.

- [ ] **Step 3: Implement `docsForOrg`** — in `typescript/apps/chalk/src/db/index.ts`, add after `docsForAccessor`:

```typescript
// docsForOrg returns every doc owned by org, regardless of who created it
// or any doc_access grant — org docs have none (see createDoc's org-mode
// branch), since access is org-wide by construction (the space's own
// community.opensocial.access record, not a per-user relation).
export async function docsForOrg(db: Db, org: string): Promise<DocSummary[]> {
  const rows = await db
    .select({
      spaceUri: docs.spaceUri,
      docId: docs.docId,
      ownerDid: docs.ownerDid,
      title: docs.title,
      updatedAt: docs.updatedAt,
      isOrg: docs.isOrg,
    })
    .from(docs)
    .where(and(eq(docs.isOrg, true), eq(docs.ownerDid, org)))
    .orderBy(desc(docs.updatedAt));
  return rows.map(toSummary);
}
```

Add `and` to the existing `import { desc, eq } from "drizzle-orm";` line, making it `import { and, desc, eq } from "drizzle-orm";`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm --filter chalk test -- docsIndex`
Expected: PASS.

- [ ] **Step 5: Update `functions.ts`'s `createDoc` and `listDocs`**

In `typescript/apps/chalk/src/server/functions.ts`, replace the imports and both functions:

```typescript
import { createServerFn } from "@tanstack/react-start";
import { env } from "cloudflare:workers";
import {
  deleteDocAccess,
  docByUri,
  docsForAccessor,
  docsForOrg,
  getDb,
  upsertDoc,
  upsertDocAccess,
  type DocSummary,
} from "../db";
import {
  clearSession,
  createDocSpace,
  docRole,
  requireSession,
} from "./functions.server";
import { SapClient } from "./sapClient";
```

`DOCS_SPACE_TYPE` and `fetchOrgName` are no longer imported here — `createDoc` now delegates to `createDocSpace` (which owns `DOCS_SPACE_TYPE`), and `fetchOrgName`/`startLogin` aren't needed until Task 7/8 add `listMyOrgs`/`startOrgConnect` to this same import block. `docByUri` isn't called yet either (Task 11 uses it) but is imported now since Task 11 only adds the one new export and shouldn't need to revisit this import list.

Replace `createDoc`:

```typescript
export const createDoc = createServerFn({ method: "POST" }).handler(
  async (): Promise<{ docId: string; uri: string }> => {
    const { did, currentOrg } = await requireSession();
    const client = new SapClient(env, did);

    const { uri, ownerDid, isOrg } = await createDocSpace(
      client,
      did,
      currentOrg,
    );

    // sap has no way to discover this space on its own until the member's
    // next session crawl — tell it explicitly so DocSync's outbox consumer
    // actually receives events for edits to it.
    await client.trackSpace(uri);

    // docId is the doc's full space URI, not just its trailing skey: a
    // space's skey alone doesn't say which host/repo it lives under, so a
    // bare skey can't be resolved back to a space without already having it
    // in this instance's DocStore — which breaks for a doc shared with a
    // member whose chalk instance never created or synced it. The full URI
    // is self-describing.
    const docId = uri;

    const db = getDb(env);
    await upsertDoc(db, {
      spaceUri: uri,
      docId,
      ownerDid,
      title: "Untitled",
      isOrg,
    });

    // Personal docs grant the owner local doc_access now rather than
    // waiting on the outbox webhook to sync their own userRelation record
    // back — without this, docsForAccessor's inner join hides the doc the
    // owner just created until that async round-trip lands. Org docs have
    // no doc_access rows at all: access is org-wide via the space's own
    // community.opensocial.access record (see createDocSpace), not a
    // per-user grant.
    if (!isOrg) {
      await upsertDocAccess(db, {
        uri,
        spaceUri: uri,
        subjectDid: did,
        relation: "owner",
      });
    }

    // Record the room's identity now, so the owner-republish alarm knows the
    // owner before the webhook (src/server/webhook.ts) delivers it.
    await env.DOC.get(env.DOC.idFromName(uri)).seedIdentity({
      spaceUri: uri,
      ownerDid,
    });

    return { docId, uri };
  },
);
```

Replace `listDocs`:

```typescript
export const listDocs = createServerFn({ method: "GET" }).handler(
  async (): Promise<DocSummary[]> => {
    const { did, currentOrg } = await requireSession();
    const db = getDb(env);
    return currentOrg ? docsForOrg(db, currentOrg) : docsForAccessor(db, did);
  },
);
```

- [ ] **Step 6: Typecheck and fix unused imports**

Run: `pnpm --filter chalk exec tsc --noEmit`
Expected: no errors. If `DOCS_SPACE_TYPE` (or anything else) is now unused in `functions.ts`, remove it from the import list — `createDoc` no longer references it directly since `createDocSpace` owns that constant now.

- [ ] **Step 7: Commit**

```bash
git add typescript/apps/chalk/src/server/functions.ts typescript/apps/chalk/src/db/index.ts typescript/apps/chalk/test/docsIndex.test.ts
git commit -m "chalk: wire createDoc/listDocs to org-mode space creation and listing"
```

---

### Task 7: `listMyOrgs` and `startOrgConnect` server functions

**Files:**
- Modify: `typescript/apps/chalk/src/server/functions.ts`
- Modify: `typescript/apps/chalk/test/functions.server.test.ts` (tests the plain helper `listMyOrgs` delegates to, added to `functions.server.ts`)
- Modify: `typescript/apps/chalk/src/server/functions.server.ts`

**Interfaces:**
- Consumes: `SapClient.call`, `fetchOrgName` (Task 5), `startLogin`-style pattern from `sapClient.ts`.
- Produces: `listMyOrgIds(client: SapClient): Promise<string[]>` (functions.server.ts, tested); `listMyOrgs` and `startOrgConnect` (functions.ts, `createServerFn` wrappers, not directly unit tested — same convention as `createDoc`/`listDocs`).

- [ ] **Step 1: Write the failing test** — add to `typescript/apps/chalk/test/functions.server.test.ts`:

```typescript
describe("listMyOrgIds", () => {
  const server = setupServer();
  beforeEach(() => server.listen({ onUnhandledRequest: "error" }));
  afterEach(() => {
    server.resetHandlers();
    server.close();
  });

  it("returns the owner DID of every community.opensocial.members space", async () => {
    server.use(
      http.get(
        "http://sap-internal.test/proxy/network.habitat.space.listSpaces",
        ({ request }) => {
          expect(new URL(request.url).searchParams.get("type")).toBe(
            "community.opensocial.members",
          );
          return HttpResponse.json({
            spaces: [
              { uri: "at://did:web:org1.example/space/community.opensocial.members/self" },
              { uri: "at://did:web:org2.example/space/community.opensocial.members/self" },
            ],
          });
        },
      ),
    );
    const { SapClient } = await import("../src/server/sapClient");
    const { listMyOrgIds } = await import("../src/server/functions.server");
    const client = new SapClient(testEnv, "did:plc:member1");
    expect(await listMyOrgIds(client)).toEqual([
      "did:web:org1.example",
      "did:web:org2.example",
    ]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter chalk test -- functions.server`
Expected: FAIL — `listMyOrgIds` is not exported.

- [ ] **Step 3: Implement `listMyOrgIds`** — add to `typescript/apps/chalk/src/server/functions.server.ts`:

```typescript
import { parseSpaceURI } from "internal";

// listMyOrgIds lists the DIDs of every opensocial org the member belongs
// to — every community.opensocial.members space they hold a membership or
// acceptance record in, same query frontend's Communities page uses
// (frontend/src/queries/opensocial.ts's myOrgsQueryOptions).
export async function listMyOrgIds(client: SapClient): Promise<string[]> {
  const { spaces } = await client.call<{ spaces: { uri: string }[] }>(
    "network.habitat.space.listSpaces",
    "GET",
    { type: "community.opensocial.members" },
  );
  const orgs: string[] = [];
  for (const space of spaces) {
    const parts = parseSpaceURI(space.uri);
    if (parts) orgs.push(parts.spaceOwner);
  }
  return orgs;
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm --filter chalk test -- functions.server`
Expected: PASS.

- [ ] **Step 5: Add the `createServerFn` wrappers** — in `typescript/apps/chalk/src/server/functions.ts`, update the `./functions.server` and `./sapClient` import lines to:

```typescript
import {
  clearSession,
  createDocSpace,
  docRole,
  fetchOrgName,
  listMyOrgIds,
  requireSession,
} from "./functions.server";
import { SapClient, startLogin } from "./sapClient";
```

then add two new exports, placed after `listDocs`:

```typescript
export interface OrgOption {
  did: string;
  name: string | null;
}

// listMyOrgs lists every org the member belongs to, with a best-effort
// display name (null if the read failed — the org-picker falls back to
// showing the raw DID).
export const listMyOrgs = createServerFn({ method: "GET" }).handler(
  async (): Promise<OrgOption[]> => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    const orgIds = await listMyOrgIds(client);
    return Promise.all(
      orgIds.map(async (orgDid) => ({
        did: orgDid,
        name: await fetchOrgName(client, orgDid),
      })),
    );
  },
);

// startOrgConnect asks sap to begin the opensocial admin sign-in flow for
// orgDid, telling it to redirect the browser back to chalk's
// /session/org-callback (with the resolved DID — always orgDid itself,
// since handleAddSession resolves whatever identifier it's given) once
// that flow completes. Returns the URL the browser should be sent to next.
// Mirrors startLogin (sapClient.ts) exactly, but with a DID instead of a
// handle and a different return_to.
export const startOrgConnect = createServerFn({ method: "POST" })
  .validator((input: { orgDid: string }) => input)
  .handler(async ({ data }): Promise<{ redirectUrl: string }> => {
    await requireSession();
    return { redirectUrl: await startLogin(env, data.orgDid) };
  });
```

`startLogin` always sets `return_to` to `${env.CHALK_BASE_URL}/session/callback` today — Task 8 changes that to accept a `returnPath` so this can point at `/session/org-callback` instead. Update the call above once Task 8 lands (this task's `startOrgConnect` is written against the Task 8 signature already — see Task 8's Step 3 for `startLogin`'s new signature, and revisit this call site there).

- [ ] **Step 6: Typecheck**

Run: `pnpm --filter chalk exec tsc --noEmit`
Expected: errors are fine at this point (`startLogin` doesn't yet take a second argument) — Task 8 fixes this. Do not fix it here; that's Task 8's job. Just confirm the only error is the `startLogin` call arity, nothing else.

- [ ] **Step 7: Commit**

```bash
git add typescript/apps/chalk/src/server/functions.ts typescript/apps/chalk/src/server/functions.server.ts typescript/apps/chalk/test/functions.server.test.ts
git commit -m "chalk: add listMyOrgs and startOrgConnect (WIP, needs Task 8's startLogin change)"
```

---

### Task 8: `startLogin` takes a return path; org-connect callback route

**Files:**
- Modify: `typescript/apps/chalk/src/server/sapClient.ts`
- Modify: `typescript/apps/chalk/test/sapClient.test.ts`
- Modify: `typescript/apps/chalk/src/routes/login.tsx`
- Modify: `typescript/apps/chalk/src/server/functions.ts` (fix the `startOrgConnect` call from Task 7)
- Create: `typescript/apps/chalk/src/routes/session.org-callback.tsx`

**Interfaces:**
- Consumes: `upsertConnectedOrg` (Task 1), `fetchOrgName` (Task 5).
- Produces: `startLogin(env, handle, returnPath?: string): Promise<string>` (defaults to `"/session/callback"`, preserving `login.tsx`'s existing call unchanged).

- [ ] **Step 1: Write the failing test** — add to `typescript/apps/chalk/test/sapClient.test.ts`, inside `describe("startLogin", ...)`:

```typescript
it("posts a custom return_to path when given one", async () => {
  let body: unknown;
  server.use(
    http.post("http://sap-internal.test/session/add", async ({ request }) => {
      body = await request.json();
      return HttpResponse.json({
        redirect_url: "https://pear.example/oauth/authorize",
      });
    }),
  );
  await startLogin(testEnv, "did:web:org.example", "/session/org-callback");
  expect(body).toEqual({
    handle: "did:web:org.example",
    return_to: "https://chalk.test/session/org-callback",
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter chalk test -- sapClient`
Expected: FAIL — `startLogin` only takes 2 arguments today.

- [ ] **Step 3: Update `startLogin`** — in `typescript/apps/chalk/src/server/sapClient.ts`:

```typescript
export async function startLogin(
  env: Env,
  handle: string,
  returnPath = "/session/callback",
): Promise<string> {
  const base = env.CHALK_BASE_URL;
  if (!base) throw new Error("CHALK_BASE_URL is not set");
  if (!env.CHALK_SAP_INTERNAL_URL)
    throw new Error("CHALK_SAP_INTERNAL_URL is not set");
  const res = await fetch(`${env.CHALK_SAP_INTERNAL_URL}/session/add`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      ...sapAuthHeaders(env),
    },
    body: JSON.stringify({
      handle,
      return_to: `${base}${returnPath}`,
    }),
  });
  if (!res.ok) {
    throw new Error(
      `failed to start login (${res.status}): ${await res.text()}`,
    );
  }
  const { redirect_url } = (await res.json()) as { redirect_url: string };
  return redirect_url;
}
```

`login.tsx`'s existing call (`startLogin(env, data.handle)`) needs no change — the new parameter defaults to the old path.

- [ ] **Step 4: Fix `startOrgConnect`'s call from Task 7** — in `typescript/apps/chalk/src/server/functions.ts`:

```typescript
export const startOrgConnect = createServerFn({ method: "POST" })
  .validator((input: { orgDid: string }) => input)
  .handler(async ({ data }): Promise<{ redirectUrl: string }> => {
    await requireSession();
    return {
      redirectUrl: await startLogin(env, data.orgDid, "/session/org-callback"),
    };
  });
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `pnpm --filter chalk test -- sapClient`
Expected: PASS.

Run: `pnpm --filter chalk exec tsc --noEmit`
Expected: no errors now (Task 7's arity error is fixed).

- [ ] **Step 6: Create the org-callback route** — create `typescript/apps/chalk/src/routes/session.org-callback.tsx`, modeled on `session.callback.tsx`:

```typescript
import { env } from "cloudflare:workers";
import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { z } from "zod";
import { fetchOrgName, requireSession } from "@/server/functions.server";
import { getDb, upsertConnectedOrg } from "@/db";
import { SapClient } from "@/server/sapClient";
import { useAppSession } from "@/server/session";
import { Button } from "internal/components/ui";

// connectOrgFn verifies the connection actually works (a member who wasn't
// really an admin never reaches here — pear's HandleOpensocial already
// checked that before completing PDS login) by reading the org's own
// profile, records the connection, and returns the name to show. Returns
// null on failure instead of throwing, so the route can render a plain
// error state.
const connectOrgFn = createServerFn({ method: "POST" })
  .validator((input: { orgDid: string }) => input)
  .handler(async ({ data }): Promise<{ orgName: string } | null> => {
    const { did } = await requireSession();
    const client = new SapClient(env, did);
    const orgName = await fetchOrgName(client, data.orgDid);
    if (orgName === null) return null;
    await upsertConnectedOrg(getDb(env), {
      memberDid: did,
      orgDid: data.orgDid,
      orgName,
    });
    return { orgName };
  });

const setCurrentOrgFn = createServerFn({ method: "POST" })
  .validator((input: { orgDid: string }) => input)
  .handler(async ({ data }) => {
    const session = await useAppSession();
    await session.update({ currentOrg: data.orgDid });
  });

export const Route = createFileRoute("/session/org-callback")({
  validateSearch: z.object({
    did: z.string().optional(),
  }),
  loaderDeps: ({ search }) => ({ did: search.did }),
  loader: async ({ deps }) => {
    if (!deps.did) return { orgDid: undefined, result: null };
    const result = await connectOrgFn({ data: { orgDid: deps.did } });
    return { orgDid: deps.did, result };
  },
  component() {
    const { orgDid, result } = Route.useLoaderData();
    const navigate = Route.useNavigate();

    if (!orgDid) {
      return <p>Missing org — please try connecting again from /orgs.</p>;
    }
    if (!result) {
      return (
        <p>
          Couldn't connect this org — you may not be an admin of it, or the
          connection failed. Please try again from /orgs.
        </p>
      );
    }
    return (
      <div className="flex flex-col items-center gap-4 py-32">
        <p>Successfully approved Chalk with {result.orgName}</p>
        <Button
          onClick={async () => {
            await setCurrentOrgFn({ data: { orgDid } });
            navigate({ to: "/" });
          }}
        >
          Go home
        </Button>
      </div>
    );
  },
});
```

- [ ] **Step 7: Typecheck**

Run: `pnpm --filter chalk exec tsc --noEmit`
Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add typescript/apps/chalk/src/server/sapClient.ts typescript/apps/chalk/test/sapClient.test.ts typescript/apps/chalk/src/server/functions.ts typescript/apps/chalk/src/routes/session.org-callback.tsx
git commit -m "chalk: add org-connect callback route"
```

---

### Task 9: Org-picker page

**Files:**
- Create: `typescript/apps/chalk/src/routes/orgs.tsx`

**Interfaces:**
- Consumes: `listMyOrgs`, `startOrgConnect` (Task 7), `OrgOption` type (Task 7).

- [ ] **Step 1: Create the route** — create `typescript/apps/chalk/src/routes/orgs.tsx`:

```typescript
import { createFileRoute } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  Button,
  Item,
  ItemContent,
  ItemTitle,
  toast,
} from "internal/components/ui";
import { listMyOrgs, startOrgConnect } from "@/server/functions";

export const Route = createFileRoute("/orgs")({
  loader: async ({ context }) =>
    context.queryClient.ensureQueryData({
      queryKey: ["myOrgs"],
      queryFn: () => listMyOrgs(),
    }),
  component() {
    const {
      data: orgs = [],
      isError: orgsFailed,
      error: orgsError,
    } = useQuery({
      queryKey: ["myOrgs"],
      queryFn: () => listMyOrgs(),
    });
    const { mutate: connect, isPending } = useMutation({
      mutationFn: (orgDid: string) => startOrgConnect({ data: { orgDid } }),
      onSuccess: ({ redirectUrl }) => {
        window.location.href = redirectUrl;
      },
      onError: (error) => {
        toast.add({
          type: "error",
          title: "Couldn't start connecting this org",
          description: error.message,
        });
      },
    });

    return (
      <div className="flex w-full max-w-md flex-col gap-2 py-16 mx-auto">
        <h1 className="text-lg font-semibold">Connect an org</h1>
        {orgsFailed && (
          <p className="text-sm text-destructive">
            Couldn't load your orgs: {orgsError.message}
          </p>
        )}
        {!orgsFailed && orgs.length === 0 && (
          <p className="text-sm text-muted-foreground">
            You don't belong to any orgs yet.
          </p>
        )}
        {orgs.map((org) => (
          <Item key={org.did} variant="outline">
            <ItemContent>
              <ItemTitle>{org.name ?? org.did}</ItemTitle>
            </ItemContent>
            <Button disabled={isPending} onClick={() => connect(org.did)}>
              Connect
            </Button>
          </Item>
        ))}
      </div>
    );
  },
});
```

- [ ] **Step 2: Typecheck**

Run: `pnpm --filter chalk exec tsc --noEmit`
Expected: no errors. If `Item`/`ItemContent`/`ItemTitle` aren't exported from `internal/components/ui`, check `frontend/src/components/OrgItem.tsx`'s import (it uses the same three) and match its exact import path.

- [ ] **Step 3: Commit**

```bash
git add typescript/apps/chalk/src/routes/orgs.tsx
git commit -m "chalk: add org-picker page"
```

---

### Task 10: Sidebar mode indicator and link to `/orgs`

**Files:**
- Modify: `typescript/apps/chalk/src/routes/_requireAuth.tsx`

**Interfaces:**
- Consumes: nothing new — just needs the current session's `currentOrg`, which `getCaller`/`requireSession` doesn't currently return. Extend `requireSession`'s return type.

- [ ] **Step 1: Extend `requireSession`'s return type** — in `typescript/apps/chalk/src/server/functions.server.ts`:

```typescript
export async function requireSession(): Promise<{
  did: string;
  currentOrg?: string;
}> {
  const session = await useAppSession();
  if (!session.data.did) {
    throw redirect({ to: "/login" });
  }
  return { did: session.data.did, currentOrg: session.data.currentOrg };
}
```

Existing callers (`createDoc`, `listDocs`, `getDocRole`, `getDocInitialState`) only destructure `{ did }`, so this is backward compatible. Update the existing test in `functions.server.test.ts`'s `requireSession` describe block:

```typescript
it("returns the caller when a session DID is set", async () => {
  sessionData.did = "did:plc:member1";
  await expect(requireSession()).resolves.toEqual({
    did: "did:plc:member1",
    currentOrg: undefined,
  });
});
```

- [ ] **Step 2: Run the affected test**

Run: `pnpm --filter chalk test -- functions.server`
Expected: PASS.

- [ ] **Step 3: Add a `getCaller`-adjacent mode read and sidebar UI** — in `typescript/apps/chalk/src/routes/_requireAuth.tsx`, add a `getCurrentOrg` server function and use it in the sidebar. Add near the top (after existing imports), a new export in `functions.ts` first:

In `typescript/apps/chalk/src/server/functions.ts`, add after `getCaller`:

```typescript
// getCurrentOrg resolves the member's currently-selected org, if any — used
// by the sidebar to show which mode (Personal, or which org) is active.
export const getCurrentOrg = createServerFn({ method: "GET" }).handler(
  async (): Promise<string | undefined> => {
    const { currentOrg } = await requireSession();
    return currentOrg;
  },
);
```

Then in `typescript/apps/chalk/src/routes/_requireAuth.tsx`, update the imports and loader/component:

```typescript
import { createDoc, getCaller, getCurrentOrg, listDocs, signOut } from "@/server/functions";
```

In the `loader`, add `getCurrentOrg()` to the `Promise.all`:

```typescript
  loader: async ({ context }) => {
    const [, actor, currentOrg] = await Promise.all([
      context.queryClient.ensureQueryData({
        queryKey: ["docs"],
        queryFn: listDocs,
      }),
      getProfile(context.did),
      getCurrentOrg(),
    ]);
    return { actor, currentOrg };
  },
```

In the `component`, destructure `currentOrg` and add a sidebar item showing the mode plus a link to `/orgs`:

```typescript
    const { actor, currentOrg } = Route.useLoaderData();
```

Add this `SidebarMenuItem` inside the existing `sidebarHeader`'s `SidebarMenu`, right after the "New Document" item:

```typescript
            <SidebarMenuItem>
              <SidebarMenuButton variant="outline" render={<Link to="/orgs" />}>
                <span>{currentOrg ? `Org: ${currentOrg}` : "Personal"}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
```

- [ ] **Step 4: Typecheck**

Run: `pnpm --filter chalk exec tsc --noEmit`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add typescript/apps/chalk/src/server/functions.server.ts typescript/apps/chalk/test/functions.server.test.ts typescript/apps/chalk/src/server/functions.ts typescript/apps/chalk/src/routes/_requireAuth.tsx
git commit -m "chalk: show current mode in sidebar, link to /orgs"
```

---

### Task 11: Hide `ShareDialog` on org docs

**Files:**
- Modify: `typescript/apps/chalk/src/server/functions.ts`
- Modify: `typescript/apps/chalk/src/routes/_requireAuth/$uri.tsx`

**Interfaces:**
- Produces: `getDoc(docId): Promise<DocSummary | undefined>` (functions.ts, wraps `docByUri`).

- [ ] **Step 1: Add `getDoc`** — in `typescript/apps/chalk/src/server/functions.ts`, add (needs `docByUri` added to the `../db` import — already added in Task 6's import block):

```typescript
export const getDoc = createServerFn({ method: "GET" })
  .validator((input: { docId: string }) => input)
  .handler(async ({ data }): Promise<DocSummary | undefined> => {
    await requireSession();
    return docByUri(getDb(env), data.docId);
  });
```

- [ ] **Step 2: Use it in the doc route** — in `typescript/apps/chalk/src/routes/_requireAuth/$uri.tsx`, add `getDoc` to the import from `@/server/functions`, add a query, and gate `ShareDialog` on `!doc?.isOrg`:

```typescript
import {
  getDoc,
  getDocInitialState,
  getDocRole,
  listDocAccess,
  revokeDocAccess,
  shareDoc,
} from "@/server/functions";
```

Add a query options helper next to the existing two:

```typescript
const docQueryOptions = (docId: string) =>
  queryOptions({
    queryKey: ["doc", docId],
    queryFn: () => getDoc({ data: { docId } }),
  });
```

In the loader, fetch it alongside the existing two:

```typescript
  loader: async ({ context, params }) => {
    const [role, initialState, doc] = await Promise.all([
      context.queryClient.ensureQueryData(docRoleQueryOptions(params.uri)),
      context.queryClient.ensureQueryData(
        docInitialStateQueryOptions(params.uri),
      ),
      context.queryClient.ensureQueryData(docQueryOptions(params.uri)),
    ]);
    return { role, initialState, doc };
  },
```

In the component, destructure `doc` and change the `ShareDialog` condition:

```typescript
    const { role, initialState, doc } = Route.useLoaderData();
```

```typescript
            {role === "editor" && !doc?.isOrg && (
              <ShareDialog
                ...
```

- [ ] **Step 3: Typecheck**

Run: `pnpm --filter chalk exec tsc --noEmit`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add typescript/apps/chalk/src/server/functions.ts typescript/apps/chalk/src/routes/_requireAuth/\$uri.tsx
git commit -m "chalk: hide ShareDialog on org docs"
```

---

### Task 12: Full-suite check

**Files:** none (verification only)

- [ ] **Step 1: Run the full chalk test suite**

Run: `pnpm --filter chalk test`
Expected: all tests pass, including the pre-existing `docRoom*`, `outbox`, `webhook`, `spaceUri` suites (unaffected by this plan, but confirms nothing broke).

- [ ] **Step 2: Typecheck and build**

Run: `pnpm --filter chalk exec tsc --noEmit`
Run: `pnpm --filter chalk build`
Expected: both succeed.

- [ ] **Step 3: Format and lint**

Run: `npx prettier --check typescript/apps/chalk/src typescript/apps/chalk/test`
Run: `npx oxlint typescript/apps/chalk/src typescript/apps/chalk/test`
Expected: no issues. Fix any formatting with `npx prettier --write` on the flagged files.

No commit for this task — it's verification of the state Task 11 already committed.

import { describe, expect, it } from "vitest";
import { memoryDatabase } from "../test/sqlite.js";
import {
  AppDefinitionError,
  appIndexes,
  createRuntime,
  defineApp,
  migrate,
  mutation,
  query,
  SpaceWriteError,
  toLexJson,
  type HabitatAgent,
  type SliceBatch,
  type SqlDatabase,
} from "./index.js";

const ORG = "did:plc:org";
const ALICE = "did:plc:alice";
const BOB = "did:plc:bob";
const MESSAGE = "com.example.chat.message";
const MEMBERS = `at://${ORG}/space/community.opensocial.members/self`;
const GENERAL = `at://${ORG}/space/com.example.chat/general`;
const SECRET = `at://${ORG}/space/com.example.chat/secret`;

const app = defineApp({
  name: "chat",
  spaceTypes: ["com.example.chat"],
  collections: {
    [MESSAGE]: {
      fields: { createdAt: "$.createdAt", text: "$.text" },
      indexes: [["space", "createdAt"]],
    },
  },
});

const cid = (s: string) => ({ toString: () => s });

function ops(
  space: string,
  did: string,
  changes: Array<{
    collection: string;
    rkey: string;
    value?: unknown;
    delete?: boolean;
  }>,
): SliceBatch {
  return {
    _tag: "Ops",
    space,
    did,
    changes: changes.map((c) => ({
      uri: `${space}/${did}/${c.collection}/${c.rkey}`,
      collection: c.collection,
      rkey: c.rkey,
      cid: c.delete ? null : cid(`cid-${c.rkey}`),
      value: c.value,
    })),
  };
}

const membership = (did: string, roles: string[]) =>
  ops(MEMBERS, ORG, [
    {
      collection: "community.opensocial.membership",
      rkey: did,
      value: { roles },
    },
  ]);
const access = (space: string, roles: string[]) =>
  ops(space, ORG, [
    {
      collection: "community.opensocial.access",
      rkey: "self",
      value: { roles },
    },
  ]);
const message = (space: string, did: string, rkey: string, text: string) =>
  ops(space, did, [
    {
      collection: MESSAGE,
      rkey,
      value: { text, createdAt: `2026-01-01T00:00:0${rkey}Z` },
    },
  ]);

const listMessages = query({
  handler: async (ctx) =>
    (
      await ctx.db
        .query<{ text: string }>(MESSAGE)
        .orderBy("createdAt")
        .collect()
    ).map((r) => r.value.text),
});

// seeded: alice is a member; general is open to members, secret to admins.
async function seeded(db: SqlDatabase = memoryDatabase()) {
  const runtime = createRuntime(app, { db });
  await runtime.migrate();
  await runtime.sink.apply(membership(ALICE, ["member"]));
  await runtime.sink.apply(access(GENERAL, ["member"]));
  await runtime.sink.apply(access(SECRET, ["admin"]));
  await runtime.sink.apply(message(GENERAL, ALICE, "1", "hello"));
  await runtime.sink.apply(message(SECRET, ORG, "2", "classified"));
  return { db, runtime };
}

describe("defineApp", () => {
  it("rejects paths that can't be inlined safely", () => {
    const define = (path: string) =>
      defineApp({
        name: "x",
        spaceTypes: [],
        collections: { [MESSAGE]: { fields: { f: path as "$.x" } } },
      });
    expect(() => define("$.a'); DROP TABLE hab_records; --")).toThrow(
      AppDefinitionError,
    );
    expect(() => define("$.items[0]")).toThrow(AppDefinitionError);
    expect(() => define("$.a.b_c")).not.toThrow();
  });

  it("rejects indexes over undeclared fields and bad NSIDs", () => {
    expect(() =>
      defineApp({
        name: "x",
        spaceTypes: [],
        collections: { [MESSAGE]: { indexes: [["nope"]] } },
      }),
    ).toThrow(/unknown field/);
    expect(() =>
      defineApp({
        name: "x",
        spaceTypes: [],
        collections: { "not an nsid": {} },
      }),
    ).toThrow(/invalid collection/);
    expect(() =>
      defineApp({
        name: "x",
        spaceTypes: [],
        collections: { [MESSAGE]: { fields: { space: "$.s" } } },
      }),
    ).toThrow(/built-in/);
  });
});

describe("migrate", () => {
  const indexNames = async (db: SqlDatabase) =>
    (
      await db
        .prepare(
          `SELECT name FROM sqlite_master WHERE type = 'index' AND name LIKE 'hab_ix_%'`,
        )
        .all<{ name: string }>()
    ).results.map((r) => r.name);

  it("is idempotent and builds the declared indexes", async () => {
    const db = memoryDatabase();
    await migrate(db, app);
    await migrate(db, app);
    expect(await indexNames(db)).toEqual(appIndexes(app).map((i) => i.name));
  });

  it("adds a new index over existing records and drops a stale one, keeping data", async () => {
    const { db, runtime } = await seeded();
    const next = defineApp({
      name: "chat",
      spaceTypes: ["com.example.chat"],
      collections: {
        [MESSAGE]: {
          fields: { createdAt: "$.createdAt", text: "$.text" },
          indexes: [["text"]],
        },
      },
    });
    await migrate(db, next);
    expect(await indexNames(db)).toEqual(appIndexes(next).map((i) => i.name));
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([
      "hello",
    ]);
  });

  it("queries on a declared index use it", async () => {
    const { db } = await seeded();
    const built = createRuntime(app, { db });
    const sql = await built.run(
      query({
        handler: async (ctx) =>
          ctx.db
            .query(MESSAGE)
            .in(GENERAL)
            .orderBy("createdAt", "desc")
            .limit(10)
            .toSQL(),
      }),
      { viewer: ALICE },
    );
    const plan = await db
      .prepare(`EXPLAIN QUERY PLAN ${sql.sql}`)
      .bind(...sql.params)
      .all<{ detail: string }>();
    expect(plan.results.map((r) => r.detail).join("\n")).toContain(
      appIndexes(app)[0]!.name,
    );
  });
});

describe("viewer filtering", () => {
  it("returns only records in spaces the viewer can read", async () => {
    const { runtime } = await seeded();
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([
      "hello",
    ]);
    expect(await runtime.run(listMessages, { viewer: BOB })).toEqual([]);
  });

  it("applies to get, count, canRead and spaces too", async () => {
    const { runtime } = await seeded();
    const probe = query({
      handler: async (ctx) => ({
        secret: await ctx.db.get(`${SECRET}/${ORG}/${MESSAGE}/2`),
        count: await ctx.db.query(MESSAGE).count(),
        canRead: await ctx.db.canRead(SECRET),
        spaces: await ctx.db.spaces({ type: "com.example.chat" }),
        narrowed: await ctx.db.query(MESSAGE).in(SECRET).collect(),
      }),
    });
    expect(await runtime.run(probe, { viewer: ALICE })).toEqual({
      secret: null,
      count: 1,
      canRead: false,
      spaces: [GENERAL],
      narrowed: [],
    });
  });

  it("follows membership and access changes without app code", async () => {
    const { runtime } = await seeded();
    await runtime.sink.apply(membership(ALICE, ["member", "admin"]));
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([
      "hello",
      "classified",
    ]);
    await runtime.sink.apply(access(GENERAL, ["admin"]));
    await runtime.sink.apply(membership(ALICE, ["member"]));
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([]);
    await runtime.sink.apply(access(GENERAL, ["member"]));
    await runtime.sink.apply(
      ops(MEMBERS, ORG, [
        {
          collection: "community.opensocial.membership",
          rkey: ALICE,
          delete: true,
        },
      ]),
    );
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([]);
  });

  it("ignores access and membership records the authority didn't write", async () => {
    const { runtime } = await seeded();
    // Bob grants himself a role and opens the secret space, from his own repo.
    await runtime.sink.apply(
      ops(MEMBERS, BOB, [
        {
          collection: "community.opensocial.membership",
          rkey: BOB,
          value: { roles: ["admin"] },
        },
      ]),
    );
    await runtime.sink.apply(
      ops(SECRET, BOB, [
        {
          collection: "community.opensocial.access",
          rkey: "self",
          value: { roles: ["member"] },
        },
      ]),
    );
    // A membership record outside the members space doesn't count either.
    await runtime.sink.apply(
      ops(GENERAL, ORG, [
        {
          collection: "community.opensocial.membership",
          rkey: BOB,
          value: { roles: ["member"] },
        },
      ]),
    );
    expect(await runtime.run(listMessages, { viewer: BOB })).toEqual([]);
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([
      "hello",
    ]);
  });

  it("needs a viewer, and gates unsafeDb behind the definition", async () => {
    const { db, runtime } = await seeded();
    await expect(runtime.run(listMessages, { viewer: "" })).rejects.toThrow(
      /viewer/,
    );
    const all = query({
      handler: async (ctx) => ctx.unsafeDb.query(MESSAGE).count(),
    });
    await expect(runtime.run(all, { viewer: BOB })).rejects.toThrow(
      /unsafeCrossSpaceReads/,
    );
    const unsafeApp = defineApp({
      ...app.definition,
      unsafeCrossSpaceReads: true,
    });
    expect(
      await createRuntime(unsafeApp, { db }).run(all, { viewer: BOB }),
    ).toBe(2);
  });
});

describe("SliceSink", () => {
  it("stores only declared collections, and deletes on tombstones", async () => {
    const { runtime } = await seeded();
    await runtime.sink.apply(
      ops(GENERAL, ALICE, [
        {
          collection: "com.example.other",
          rkey: "9",
          value: { text: "ignored" },
        },
        {
          collection: MESSAGE,
          rkey: "3",
          value: { text: "second", createdAt: "2026-01-01T00:00:03Z" },
        },
      ]),
    );
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([
      "hello",
      "second",
    ]);
    await runtime.sink.apply(
      ops(GENERAL, ALICE, [{ collection: MESSAGE, rkey: "1", delete: true }]),
    );
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([
      "second",
    ]);
  });

  it("is idempotent under replay", async () => {
    const { runtime } = await seeded();
    await runtime.sink.apply(message(GENERAL, ALICE, "1", "hello"));
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([
      "hello",
    ]);
  });

  it("Reset replaces a repo's records, and the authority's ACL rows", async () => {
    const { runtime } = await seeded();
    async function* records(
      list: Array<{ collection: string; rkey: string; record: unknown }>,
    ) {
      for (const r of list) yield { ...r, cid: cid(`cid-${r.rkey}`) };
    }
    await runtime.sink.apply({
      _tag: "Reset",
      space: GENERAL,
      did: ALICE,
      records: records([
        {
          collection: MESSAGE,
          rkey: "5",
          record: { text: "after reset", createdAt: "2026-01-01T00:00:05Z" },
        },
      ]),
    });
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([
      "after reset",
    ]);
    // The org's repo reset without an access record closes the space.
    await runtime.sink.apply({
      _tag: "Reset",
      space: GENERAL,
      did: ORG,
      records: records([]),
    });
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([]);
  });

  it("a failed Reset changes nothing", async () => {
    const { runtime } = await seeded();
    const failing: AsyncIterable<never> = {
      [Symbol.asyncIterator]: () => ({
        next: async () => {
          throw new Error("verification failed");
        },
      }),
    };
    await expect(
      runtime.sink.apply({
        _tag: "Reset",
        space: GENERAL,
        did: ALICE,
        records: failing,
      }),
    ).rejects.toThrow("verification failed");
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([
      "hello",
    ]);
  });

  it("RepoRemoved and SpaceDeleted clear what they covered", async () => {
    const { runtime } = await seeded();
    await runtime.sink.apply(membership(BOB, ["member"]));
    await runtime.sink.apply(message(GENERAL, BOB, "4", "from bob"));
    await runtime.sink.apply({ _tag: "RepoRemoved", space: GENERAL, did: BOB });
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([
      "hello",
    ]);
    await runtime.sink.apply({ _tag: "SpaceDeleted", space: GENERAL });
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([]);
    await runtime.sink.apply(access(GENERAL, ["member"]));
    await runtime.sink.apply({ _tag: "SpaceDeleted", space: MEMBERS });
    const spaces = query({ handler: async (ctx) => ctx.db.spaces() });
    expect(await runtime.run(spaces, { viewer: ALICE })).toEqual([]);
  });

  it("bumps the version on each committed change only", async () => {
    const { runtime } = await seeded();
    const before = await runtime.version();
    await runtime.sink.apply(
      ops(GENERAL, ALICE, [
        { collection: "com.example.other", rkey: "1", value: {} },
      ]),
    );
    expect(await runtime.version()).toBe(before);
    await runtime.sink.apply(message(GENERAL, ALICE, "6", "more"));
    expect(await runtime.version()).toBe(before + 1);
  });

  it("stores links and bytes in atproto's JSON form", () => {
    const link = {
      version: 1,
      code: 113,
      multihash: {},
      toString: () => "bafy",
    };
    expect(
      toLexJson({ a: [link], b: new Uint8Array([1, 2, 3]), c: null }),
    ).toEqual({
      a: [{ $link: "bafy" }],
      b: { $bytes: "AQID" },
      c: null,
    });
  });
});

describe("mutations", () => {
  const send = mutation({
    args: {
      parse: (input: unknown) => input as { space: string; text: string },
    },
    handler: async (ctx, { space, text }) =>
      ctx.spaces.put({
        space,
        collection: MESSAGE,
        record: { text, createdAt: "2026-01-01T00:00:07Z" },
      }),
  });

  function fakeAgent(respond: (path: string, body: any) => Response) {
    const calls: Array<{ path: string; body: any }> = [];
    const agent: HabitatAgent = {
      fetchHandler: async (path, init) => {
        const body = JSON.parse(String(init.body));
        calls.push({ path, body });
        return respond(path, body);
      },
    };
    return { agent, calls };
  }

  it("writes through the Habitat instance as the viewer, then reads its own write", async () => {
    const { db } = await seeded();
    const { agent, calls } = fakeAgent((_path, body) =>
      Response.json({
        uri: `${body.space}/${body.repo}/${body.collection}/7`,
        cid: "cid-7",
      }),
    );
    const synced: string[] = [];
    const runtime = createRuntime(app, {
      db,
      agent: async () => agent,
      afterWrite: (space) => void synced.push(space),
    });
    const written = await runtime.run(send, {
      viewer: ALICE,
      args: { space: GENERAL, text: "sent" },
    });
    expect(written).toEqual({
      uri: `${GENERAL}/${ALICE}/${MESSAGE}/7`,
      cid: "cid-7",
      rkey: "7",
    });
    expect(calls).toEqual([
      {
        path: "/xrpc/com.atproto.space.putRecord",
        body: {
          space: GENERAL,
          repo: ALICE,
          collection: MESSAGE,
          record: {
            text: "sent",
            createdAt: "2026-01-01T00:00:07Z",
            $type: MESSAGE,
          },
        },
      },
    ]);
    expect(synced).toEqual([GENERAL]);
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([
      "hello",
      "sent",
    ]);

    const remove = mutation({
      handler: async (ctx) =>
        ctx.spaces.delete({ space: GENERAL, collection: MESSAGE, rkey: "7" }),
    });
    await runtime.run(remove, { viewer: ALICE });
    expect(calls[1]!.path).toBe("/xrpc/com.atproto.space.deleteRecord");
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([
      "hello",
    ]);
  });

  it("leaves the slice alone when the Habitat instance refuses", async () => {
    const { db } = await seeded();
    const { agent } = fakeAgent(
      () => new Response("forbidden", { status: 403 }),
    );
    const runtime = createRuntime(app, { db, agent: async () => agent });
    const err = await runtime
      .run(send, { viewer: BOB, args: { space: GENERAL, text: "nope" } })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(SpaceWriteError);
    expect((err as SpaceWriteError).status).toBe(403);
    expect(await runtime.run(listMessages, { viewer: ALICE })).toEqual([
      "hello",
    ]);
  });

  it("refuses undeclared collections and needs an agent", async () => {
    const { db } = await seeded();
    const { agent, calls } = fakeAgent(() => Response.json({}));
    const other = mutation({
      handler: async (ctx) =>
        ctx.spaces.put({
          space: GENERAL,
          collection: "com.example.other",
          record: {},
        }),
    });
    await expect(
      createRuntime(app, { db, agent: async () => agent }).run(other, {
        viewer: ALICE,
      }),
    ).rejects.toThrow(/not a collection/);
    expect(calls).toEqual([]);
    await expect(
      createRuntime(app, { db }).run(other, { viewer: ALICE }),
    ).rejects.toThrow(/agent/);
  });
});

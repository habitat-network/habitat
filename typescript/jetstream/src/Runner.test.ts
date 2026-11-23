import { type Cause, Effect, Queue, Sink, Stream } from "effect";
import { describe, expect, it } from "vitest";
import { ConnectionError } from "./errors";
import { Jetstream } from "./Jetstream";
import type { SpaceEvent } from "./events";
import { LexIndexer } from "./LexIndexer";
import type { FetchHandler, Transport } from "./transport";

const SPACE = "at://did:web:org.test/space/network.habitat.docs/d1";
const NOTE = "network.habitat.note";
const uriOf = (rkey: string, did = "did:web:org.test") =>
  `${SPACE}/${did}/${NOTE}/${rkey}`;

function commit(seq: number, rkey: string, cid: string, text = rkey) {
  return JSON.stringify({
    $type: "message",
    payload: {
      $type: "network.habitat.space.subscribe#commit",
      seq,
      space: SPACE,
      did: "did:web:org.test",
      uri: uriOf(rkey),
      time: "2026-11-20T00:00:00.000000Z",
      rev: "3l3qo2vutsw2b",
      operation: "create",
      collection: NOTE,
      rkey,
      cid,
      record: {
        $type: NOTE,
        text,
        blob: {
          $type: "blob",
          ref: {
            $link:
              "bafkreigh2akiscaildcqabsyg3dfr6chu3fgpregiymsck7e7aqa4s52zy",
          },
          mimeType: "text/plain",
          size: 1,
        },
      },
    },
  });
}

function del(seq: number, rkey: string) {
  return JSON.stringify({
    $type: "message",
    payload: {
      $type: "network.habitat.space.subscribe#commit",
      seq,
      space: SPACE,
      did: "did:web:org.test",
      uri: uriOf(rkey),
      time: "2026-11-20T00:00:00.000000Z",
      rev: "3l3qo2vutsw2c",
      operation: "delete",
      collection: NOTE,
      rkey,
    },
  });
}

// FakeServer is the subscribe endpoint: each connection gets the frames
// pushed to it from the cursor it asked for. close() drops the live one.
class FakeServer {
  log: { seq: number; frame: string }[] = [];
  connects: URL[] = [];
  private live: Queue.Queue<string, ConnectionError | Cause.Done> | undefined;

  push(seq: number, frame: string) {
    this.log.push({ seq, frame });
    if (this.live) Queue.offerUnsafe(this.live, frame);
  }

  pushRaw(frame: string) {
    if (this.live) Queue.offerUnsafe(this.live, frame);
  }

  transport: Transport = (url) =>
    Stream.callback<string, ConnectionError>((queue) =>
      Effect.sync(() => {
        this.connects.push(url);
        this.live = queue;
        const cursor = Number(url.searchParams.get("cursor") ?? Infinity);
        for (const { seq, frame } of this.log) {
          if (seq >= cursor) Queue.offerUnsafe(queue, frame);
        }
      }),
    );

  fail(message: string) {
    if (this.live) {
      const q = this.live;
      this.live = undefined;
      Effect.runSync(Queue.fail(q, new ConnectionError({ message })));
    }
  }
}

const tick = () => new Promise((r) => setTimeout(r, 5));
async function until(cond: () => boolean) {
  for (let i = 0; i < 400 && !cond(); i++) await tick();
  expect(cond()).toBe(true);
}

function setup(fetch?: FetchHandler) {
  const server = new FakeServer();
  const js = new Jetstream({
    service: "https://pear.test",
    mode: "spaces",
    transport: server.transport,
    fetch: fetch ?? (async () => new Response("{}", { status: 500 })),
    reconnect: { base: "1 millis", cap: "5 millis" },
  });
  const db = new Map<string, unknown>();
  let saved: number | undefined;
  const indexer = new LexIndexer({ mode: "spaces" }).commit(NOTE, {
    put: async (e) => {
      db.set(e.uri, e.record);
    },
    del: (e) =>
      Effect.sync(() => {
        db.delete(e.uri);
      }),
  });
  const runner = js.runner(indexer, {
    cursor: { get: () => saved, set: (c) => void (saved = c) },
    writeTimeout: "500 millis",
  });
  return { server, js, db, runner, saved: () => saved };
}

describe("Runner", () => {
  it("replays, follows live, and asks only for the indexed collections", async () => {
    const { server, db, runner, saved } = setup();
    server.push(1, commit(1, "a", "cid-a"));
    server.push(3, commit(3, "b", "cid-b"));
    void runner.replay();
    await until(() => db.size === 2);
    expect(server.connects[0].searchParams.get("cursor")).toBe("0");
    expect(server.connects[0].searchParams.getAll("collections")).toEqual([
      NOTE,
    ]);
    expect(saved()).toBe(4);

    server.push(4, del(4, "a"));
    await until(() => !db.has(uriOf("a")));
    expect(saved()).toBe(5);
    await runner.stop();
  });

  it("catches up over getSnapshot pages, then follows live from the tip", async () => {
    const urls: URL[] = [];
    let server!: FakeServer;
    // The snapshot endpoint over the server's log, two events a page.
    const snapshotFetch: FetchHandler = async (input) => {
      const url = new URL(input);
      urls.push(url);
      const after = Number(url.searchParams.get("afterSeq"));
      const tip = Number(url.searchParams.get("beforeSeq") ?? 5);
      const page = server.log.filter((e) => e.seq > after && e.seq <= tip);
      const events = page.slice(0, 2);
      const through = page.length > 2 ? events[1]!.seq : tip;
      return Response.json({
        events: events.map((e) => JSON.parse(e.frame).payload),
        plannedThroughSeq: through,
        tipSeq: tip,
      });
    };
    const made = setup(snapshotFetch);
    server = made.server;
    for (const [i, k] of ["a", "b", "c", "d", "e"].entries()) {
      server.push(i + 1, commit(i + 1, k, `cid-${k}`));
    }
    void made.runner.replay({ snapshot: true });
    await until(() => made.db.size === 5);
    await until(() => server.connects.length === 1);
    expect(urls.map((u) => u.searchParams.get("afterSeq"))).toEqual([
      "-1",
      "2",
      "4",
    ]);
    // The tip is pinned by the first page.
    expect(urls.map((u) => u.searchParams.get("beforeSeq"))).toEqual([
      null,
      "5",
      "5",
    ]);
    expect(made.saved()).toBe(6);
    expect(server.connects[0].searchParams.get("cursor")).toBe("6");
    server.push(6, commit(6, "f", "cid-f"));
    await until(() => made.db.size === 6);
    await made.runner.stop();
  });

  it("decodes records as lex values", async () => {
    const { server, db, runner } = setup();
    server.push(1, commit(1, "a", "cid-a"));
    void runner.replay();
    await until(() => db.size === 1);
    const record = db.get(uriOf("a")) as { blob: { ref: unknown } };
    // A blob ref's $link becomes a CID object, as when read from a repo.
    expect(typeof record.blob.ref).toBe("object");
    expect(String(record.blob.ref)).toBe(
      "bafkreigh2akiscaildcqabsyg3dfr6chu3fgpregiymsck7e7aqa4s52zy",
    );
    await runner.stop();
  });

  it("reconnects from the seq after the last event, including on ExpiredToken", async () => {
    const { server, db, runner } = setup();
    server.push(1, commit(1, "a", "cid-a"));
    void runner.replay({ cursor: 0 });
    await until(() => db.size === 1);

    server.fail("socket closed");
    server.push(2, commit(2, "b", "cid-b"));
    await until(() => db.size === 2);
    expect(server.connects.map((u) => u.searchParams.get("cursor"))).toEqual([
      "0",
      "2",
    ]);

    server.pushRaw(JSON.stringify({ $type: "error", error: "ExpiredToken" }));
    await until(() => server.connects.length === 3);
    expect(server.connects[2].searchParams.get("cursor")).toBe("3");
    await runner.stop();
  });

  it("retries a failing handler without advancing the cursor", async () => {
    const server = new FakeServer();
    const js = new Jetstream({
      service: "https://pear.test",
      mode: "spaces",
      transport: server.transport,
    });
    let attempts = 0;
    const seen: string[] = [];
    const runner = js.runner(
      new LexIndexer().commit(NOTE, {
        put: (e) => {
          attempts++;
          if (attempts < 3) return Promise.reject(new Error("db down"));
          seen.push(e.rkey);
        },
      }),
    );
    server.push(1, commit(1, "a", "cid-a"));
    server.push(2, commit(2, "b", "cid-b"));
    void runner.replay();
    await until(() => seen.length === 2);
    expect(seen).toEqual(["a", "b"]);
    expect(await Effect.runPromise(runner.cursor())).toBe(3);
    await runner.stop();
  });

  it("putRecord resolves once its event is indexed, matched by uri and cid", async () => {
    const calls: { path: string; body: unknown }[] = [];
    const ctx = setup(async (path, init) => {
      calls.push({
        path,
        body: init?.body ? JSON.parse(String(init.body)) : undefined,
      });
      return Response.json({ uri: uriOf("w"), cid: "cid-w2" });
    });
    const { server, db, runner } = ctx;
    void runner.replay();
    await until(() => server.connects.length === 1);

    const write = Effect.runPromise(
      runner.putRecord({
        space: SPACE,
        repo: "did:web:org.test",
        collection: NOTE,
        rkey: "w",
        record: { $type: NOTE, text: "hi" },
      }),
    );
    let settled = false;
    void write.then(() => (settled = true));
    // An older version of the same record doesn't settle it.
    server.push(1, commit(1, "w", "cid-w1"));
    await tick();
    await tick();
    expect(settled).toBe(false);
    // The same content elsewhere (same cid, other uri) doesn't either.
    server.push(2, commit(2, "other", "cid-w2"));
    await tick();
    expect(settled).toBe(false);
    server.push(3, commit(3, "w", "cid-w2", "hi"));
    expect(await write).toEqual({ uri: uriOf("w"), cid: "cid-w2" });
    expect(db.get(uriOf("w"))).toMatchObject({ text: "hi" });
    expect(calls[0].path).toBe("/xrpc/com.atproto.space.putRecord");
    expect(calls[0].body).toMatchObject({ rkey: "w", record: { text: "hi" } });
    await runner.stop();
  });

  it("putRecord resolves when the event was indexed before the response arrived", async () => {
    let respond!: () => void;
    const ctx = setup(
      () =>
        new Promise((resolve) => {
          respond = () =>
            resolve(Response.json({ uri: uriOf("w"), cid: "cid-w" }));
        }),
    );
    const { server, db, runner } = ctx;
    void runner.replay();
    await until(() => server.connects.length === 1);
    const write = Effect.runPromise(
      runner.putRecord({
        space: SPACE,
        repo: "did:web:org.test",
        collection: NOTE,
        rkey: "w",
        record: {},
      }),
    );
    server.push(1, commit(1, "w", "cid-w"));
    await until(() => db.size === 1);
    respond();
    await expect(write).resolves.toEqual({ uri: uriOf("w"), cid: "cid-w" });
    await runner.stop();
  });

  it("putRecord fails with WriteNotObserved when the event never comes", async () => {
    const { runner, server } = setup(async () =>
      Response.json({ uri: uriOf("w"), cid: "cid-w" }),
    );
    void runner.replay();
    await until(() => server.connects.length === 1);
    const exit = await Effect.runPromise(
      runner
        .putRecord(
          {
            space: SPACE,
            repo: "did:web:org.test",
            collection: NOTE,
            rkey: "w",
            record: {},
          },
          { timeout: "20 millis" },
        )
        .pipe(Effect.flip),
    );
    expect(exit._tag).toBe("WriteNotObserved");
    await runner.stop();
  });

  it("putRecord fails with WriteError when the host refuses the write", async () => {
    const { runner } = setup(async () =>
      Response.json(
        { error: "InvalidRequest", message: "nope" },
        { status: 400 },
      ),
    );
    const err = await Effect.runPromise(
      runner
        .putRecord({
          space: SPACE,
          repo: "did:web:org.test",
          collection: NOTE,
          rkey: "w",
          record: {},
        })
        .pipe(Effect.flip),
    );
    expect(err).toMatchObject({
      _tag: "WriteError",
      status: 400,
      error: "InvalidRequest",
    });
  });

  it("deleteRecord waits for the delete, or returns at once if the record is gone", async () => {
    let exists = true;
    const ctx = setup(async (path) => {
      if (path.startsWith("/xrpc/com.atproto.space.getRecord")) {
        return exists
          ? Response.json({ uri: uriOf("a"), cid: "cid-a", value: {} })
          : Response.json({ error: "RecordNotFound" }, { status: 400 });
      }
      return Response.json({});
    });
    const { server, db, runner } = ctx;
    server.push(1, commit(1, "a", "cid-a"));
    void runner.replay();
    await until(() => db.size === 1);
    const target = {
      space: SPACE,
      repo: "did:web:org.test",
      collection: NOTE,
      rkey: "a",
    };
    const done = Effect.runPromise(runner.deleteRecord(target));
    await tick();
    server.push(2, del(2, "a"));
    await done;
    expect(db.size).toBe(0);

    exists = false;
    await Effect.runPromise(
      runner.deleteRecord(target, { timeout: "20 millis" }),
    );
    await runner.stop();
  });

  it("runs an Effect Sink over the events, one at a time by default", async () => {
    const server = new FakeServer();
    const js = new Jetstream({
      service: "https://pear.test",
      mode: "spaces",
      transport: server.transport,
    });
    const groups: string[][] = [];
    const runner = js.runner(
      Sink.forEachArray((events: ReadonlyArray<SpaceEvent>) =>
        Effect.sync(() => {
          groups.push(events.map((e) => (e.kind === "put" ? e.rkey : "?")));
        }),
      ),
      { collections: [NOTE] },
    );
    server.push(1, commit(1, "a", "cid-a"));
    server.push(2, commit(2, "b", "cid-b"));
    void runner.replay();
    await until(() => groups.flat().length === 2);
    expect(groups.flat()).toEqual(["a", "b"]);
    expect(await Effect.runPromise(runner.cursor())).toBe(3);
    await runner.stop();
  });

  it("hands a Sink a batch of events at once and advances the cursor past it", async () => {
    const server = new FakeServer();
    const js = new Jetstream({
      service: "https://pear.test",
      mode: "spaces",
      transport: server.transport,
    });
    const groups: string[][] = [];
    const runner = js.runner(
      Sink.forEachArray((events: ReadonlyArray<SpaceEvent>) =>
        Effect.sync(() => {
          groups.push(events.map((e) => (e.kind === "put" ? e.rkey : "?")));
        }),
      ),
      { batch: { size: 3, window: "1 second" } },
    );
    for (const [seq, key] of [
      [1, "a"],
      [2, "b"],
      [3, "c"],
    ] as const) {
      server.push(seq, commit(seq, key, `cid-${key}`));
    }
    void runner.replay();
    await until(() => groups.length === 1);
    expect(groups).toEqual([["a", "b", "c"]]);
    expect(await Effect.runPromise(runner.cursor())).toBe(4);
    await runner.stop();
  });

  it("retries a failing Sink without advancing the cursor", async () => {
    const server = new FakeServer();
    const js = new Jetstream({
      service: "https://pear.test",
      mode: "spaces",
      transport: server.transport,
    });
    let attempts = 0;
    const seen: string[] = [];
    const runner = js.runner(
      Sink.forEach((e: SpaceEvent) =>
        Effect.suspend(() => {
          attempts++;
          if (attempts < 3) return Effect.fail("db down");
          seen.push(e.kind === "put" ? e.rkey : "?");
          return Effect.void;
        }),
      ),
    );
    server.push(1, commit(1, "a", "cid-a"));
    void runner.replay();
    await until(() => seen.length === 1);
    expect(attempts).toBe(3);
    expect(await Effect.runPromise(runner.cursor())).toBe(2);
    await runner.stop();
  });

  it("putRecord settles on an event a Sink indexed", async () => {
    const { server, js } = setup(async () =>
      Response.json({ uri: uriOf("w"), cid: "cid-w" }),
    );
    const runner = js.runner(Sink.drain, { writeTimeout: "500 millis" });
    void runner.replay();
    await until(() => server.connects.length === 1);
    const write = Effect.runPromise(
      runner.putRecord({
        space: SPACE,
        repo: "did:web:org.test",
        collection: NOTE,
        rkey: "w",
        record: { $type: NOTE, text: "hi" },
      }),
    );
    server.push(1, commit(1, "w", "cid-w"));
    expect(await write).toEqual({ uri: uriOf("w"), cid: "cid-w" });
    await runner.stop();
  });
});

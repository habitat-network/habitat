import { Effect, Stream } from "effect";
import { describe, expect, it, vi } from "vitest";
import type { SpaceEvent } from "@habitat-network/jetstream";
import type { RepoBatch, SyncSink } from "../src/server/stream/batch";
import {
  appEventSink,
  batchesOf,
  INDEXED_COLLECTIONS,
  SinkError,
} from "@/server/stream/indexer";

const SPACE = "at://did:web:org.example/space/network.habitat.docs/d1";
const CID = "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm";
const base = {
  space: SPACE,
  did: "did:plc:bob",
  rev: "3l3qo2vutsw2b",
  time: "2026-11-20T00:00:00.000000Z",
};

function put(seq: number, rkey: string, body = "hi"): SpaceEvent {
  const collection = "network.habitat.docs.comment";
  return {
    ...base,
    kind: "put",
    operation: "create",
    seq,
    uri: `${SPACE}/did:plc:bob/${collection}/${rkey}`,
    collection,
    rkey,
    cid: CID,
    record: { body },
  };
}

function del(seq: number, rkey: string): SpaceEvent {
  const collection = "network.habitat.docs.comment";
  return {
    ...base,
    kind: "del",
    operation: "delete",
    seq,
    uri: `${SPACE}/did:plc:bob/${collection}/${rkey}`,
    collection,
    rkey,
  };
}

const run = (sink: ReturnType<typeof appEventSink>, events: SpaceEvent[]) =>
  Effect.runPromise(Stream.run(Stream.fromArray(events), sink));

describe("batchesOf", () => {
  it("makes a one-change batch of each event", () => {
    const [batch, ...rest] = batchesOf([put(1, "c1")]);
    expect(rest).toEqual([]);
    expect(batch).toMatchObject({ _tag: "Ops", did: "did:plc:bob" });
    const changes = (batch as Extract<RepoBatch, { _tag: "Ops" }>).changes;
    expect(changes[0].cid?.toString()).toBe(CID);
    expect(changes[0].value).toEqual({ body: "hi" });
  });

  it("merges adjacent writes by one repo, keeping each record's last change", () => {
    const batches = batchesOf([
      put(1, "a"),
      put(2, "b", "first"),
      put(3, "b", "second"),
      del(4, "a"),
    ]);
    expect(batches).toHaveLength(1);
    const changes = (batches[0] as Extract<RepoBatch, { _tag: "Ops" }>).changes;
    expect(changes.map((c) => [c.rkey, c.cid === null])).toEqual([
      ["b", false],
      ["a", true],
    ]);
    expect(changes[0].value).toEqual({ body: "second" });
  });

  it("keeps a space deletion between writes in order", () => {
    const batches = batchesOf([
      put(1, "a"),
      { kind: "spaceDeleted", seq: 2, space: SPACE, time: base.time },
      put(3, "a"),
    ]);
    expect(batches.map((b) => b._tag)).toEqual(["Ops", "SpaceDeleted", "Ops"]);
  });

  it("skips collections the app doesn't index", () => {
    expect(
      batchesOf([
        {
          ...del(1, "p"),
          collection: "app.bsky.feed.post",
          uri: `${SPACE}/did:plc:bob/app.bsky.feed.post/p`,
        },
      ]),
    ).toEqual([]);
    expect(INDEXED_COLLECTIONS).toContain("network.habitat.docs.comment");
  });
});

describe("appEventSink", () => {
  it("applies a group of events to the sink as merged batches", async () => {
    const applied: RepoBatch[] = [];
    const sink: SyncSink = {
      apply: vi.fn(async (b: RepoBatch) => {
        applied.push(b);
      }),
    };
    await run(appEventSink(sink), [put(1, "a"), put(2, "b")]);
    expect(applied).toHaveLength(1);
    expect(
      (applied[0] as Extract<RepoBatch, { _tag: "Ops" }>).changes,
    ).toHaveLength(2);
  });

  it("fails with a SinkError when the sink fails", async () => {
    const sink: SyncSink = {
      apply: async () => {
        throw new Error("db down");
      },
    };
    const err = await run(appEventSink(sink), [put(1, "a")]).catch(
      (e: unknown) => e,
    );
    expect(err).toBeInstanceOf(SinkError);
    expect((err as SinkError).message).toBe("db down");
  });

  it("does nothing for events it doesn't index", async () => {
    const sink: SyncSink = { apply: vi.fn(async () => {}) };
    await run(appEventSink(sink), [
      { ...del(1, "p"), collection: "app.bsky.feed.post" },
    ]);
    expect(sink.apply).not.toHaveBeenCalled();
  });
});

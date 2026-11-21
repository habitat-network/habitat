import { describe, expect, it, vi } from "vitest";
import type { RepoBatch, SyncSink } from "../src/server/stream/batch";
import { appIndexer, INDEXED_COLLECTIONS } from "@/server/stream/indexer";
import { Effect } from "effect";

const SPACE = "at://did:web:org.example/space/network.habitat.docs/d1";

describe("appIndexer", () => {
  it("asks the stream for exactly the collections the sinks index", () => {
    const sink: SyncSink = { apply: async () => {} };
    expect(appIndexer(sink).collections()).toEqual([...INDEXED_COLLECTIONS]);
  });

  it("hands each event to the sink as a one-change batch", async () => {
    const batches: RepoBatch[] = [];
    const sink: SyncSink = {
      apply: vi.fn(async (b: RepoBatch) => {
        batches.push(b);
      }),
    };
    const indexer = appIndexer(sink);
    const base = {
      seq: 1,
      space: SPACE,
      did: "did:plc:bob",
      uri: `${SPACE}/did:plc:bob/network.habitat.docs.comment/c1`,
      collection: "network.habitat.docs.comment",
      rkey: "c1",
      rev: "3l3qo2vutsw2b",
      time: "2026-11-20T00:00:00.000000Z",
    };
    await Effect.runPromise(
      indexer.handle({
        ...base,
        kind: "put",
        operation: "create",
        cid: "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm",
        record: { body: "hi" },
      }),
    );
    await Effect.runPromise(
      indexer.handle({ ...base, seq: 2, kind: "del", operation: "delete" }),
    );
    await Effect.runPromise(
      indexer.handle({
        kind: "spaceDeleted",
        seq: 3,
        space: SPACE,
        time: base.time,
      }),
    );
    expect(batches.map((b) => b._tag)).toEqual(["Ops", "Ops", "SpaceDeleted"]);
    const [put, del] = batches as Extract<RepoBatch, { _tag: "Ops" }>[];
    expect(put.did).toBe("did:plc:bob");
    expect(put.changes[0].cid?.toString()).toBe(
      "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm",
    );
    expect(put.changes[0].value).toEqual({ body: "hi" });
    expect(del.changes[0]).toMatchObject({ uri: base.uri, cid: null });
  });

  it("ignores collections it doesn't index", async () => {
    const sink: SyncSink = { apply: vi.fn(async () => {}) };
    await Effect.runPromise(
      appIndexer(sink).handle({
        kind: "del",
        operation: "delete",
        seq: 1,
        space: SPACE,
        did: "did:plc:bob",
        uri: `${SPACE}/did:plc:bob/app.bsky.feed.post/p`,
        collection: "app.bsky.feed.post",
        rkey: "p",
        rev: "3l3qo2vutsw2b",
        time: "2026-11-20T00:00:00.000000Z",
      }),
    );
    expect(sink.apply).not.toHaveBeenCalled();
  });
});

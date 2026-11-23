import { Effect } from "effect";
import { describe, expect, it } from "vitest";
import { decodeFrame, decodeSnapshotPage } from "./events";

const NSID = "network.habitat.space.subscribe";
const message = (payload: unknown) =>
  JSON.stringify({ $type: "message", payload });
const base = {
  seq: 4,
  space: "at://did:web:o/space/t/k",
  did: "did:web:o",
  uri: "at://did:web:o/space/t/k/did:web:o/c.x/r",
  time: "2026-11-20T00:00:00.000000Z",
  rev: "3l3qo2vutsw2b",
  collection: "c.x",
  rkey: "r",
};
const run = <A, E>(e: Effect.Effect<A, E>) => Effect.runPromise(Effect.flip(e));

describe("decodeFrame", () => {
  it("decodes a create and a delete", async () => {
    const put = await Effect.runPromise(
      decodeFrame(
        message({
          $type: `${NSID}#commit`,
          ...base,
          operation: "create",
          cid: "c1",
          record: { a: 1 },
        }),
      ),
    );
    expect(put).toMatchObject({
      _tag: "Event",
      event: { kind: "put", cid: "c1", record: { a: 1 } },
    });
    const del = await Effect.runPromise(
      decodeFrame(
        message({ $type: `${NSID}#commit`, ...base, operation: "delete" }),
      ),
    );
    expect(del).toMatchObject({ _tag: "Event", event: { kind: "del" } });
  });

  it("skips kinds and operations it doesn't know", async () => {
    const kind = await Effect.runPromise(
      decodeFrame(message({ $type: `${NSID}#future`, seq: 1 })),
    );
    expect(kind._tag).toBe("Unknown");
    const op = await Effect.runPromise(
      decodeFrame(
        message({ $type: `${NSID}#commit`, ...base, operation: "move" }),
      ),
    );
    expect(op._tag).toBe("Unknown");
  });

  it("fails on a known kind that doesn't fit its shape", async () => {
    const noRecord = await run(
      decodeFrame(
        message({ $type: `${NSID}#commit`, ...base, operation: "create" }),
      ),
    );
    expect(noRecord._tag).toBe("FrameDecodeError");
    const noSeq = await run(
      decodeFrame(message({ $type: `${NSID}#spaceDeleted`, space: "s" })),
    );
    expect(noSeq._tag).toBe("FrameDecodeError");
    expect((await run(decodeFrame("not json")))._tag).toBe("FrameDecodeError");
  });
});

describe("decodeSnapshotPage", () => {
  it("decodes events and bounds, dropping unknown kinds", async () => {
    const page = await Effect.runPromise(
      decodeSnapshotPage({
        events: [
          { $type: `${NSID}#spaceDeleted`, seq: 2, space: "s", time: "t" },
          { $type: `${NSID}#future` },
        ],
        plannedThroughSeq: 3,
        tipSeq: 9,
      }),
    );
    expect(page.events).toHaveLength(1);
    expect(page.tipSeq).toBe(9);
  });

  it("rejects a response missing its bounds", async () => {
    const err = await run(decodeSnapshotPage({ events: [] }));
    expect(err._tag).toBe("FrameDecodeError");
  });
});

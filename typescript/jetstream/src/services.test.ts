import { Effect, Fiber, Layer, Queue, Stream } from "effect";
import { describe, expect, it } from "vitest";
import { Jetstream, LexIndexer, Runner } from "./effect";
import { ConnectionError } from "./errors";
import type { Transport } from "./transport";

const NSID = "network.habitat.space.subscribe";
const frame = (seq: number, rkey: string) =>
  JSON.stringify({
    $type: "message",
    payload: {
      $type: `${NSID}#commit`,
      seq,
      space: "at://did:web:o/space/t/k",
      did: "did:web:o",
      uri: `at://did:web:o/space/t/k/did:web:o/c.x/${rkey}`,
      time: "2026-11-20T00:00:00.000000Z",
      rev: "r",
      operation: "create",
      collection: "c.x",
      rkey,
      cid: `cid-${rkey}`,
      record: { rkey },
    },
  });

describe("Effect services", () => {
  it("runs a Runner built from layers, with Effect handlers", async () => {
    const seen: string[] = [];
    const transport: Transport = () =>
      Stream.callback<string, ConnectionError>((queue) =>
        Effect.sync(() => {
          Queue.offerUnsafe(queue, frame(1, "a"));
          Queue.offerUnsafe(queue, frame(2, "b"));
        }),
      );
    const indexer = new LexIndexer().commit("c.x", {
      put: (e) => Effect.sync(() => void seen.push(e.rkey)),
    });
    const layers = Runner.layer({ target: indexer }).pipe(
      Layer.provide(
        Jetstream.layer({
          service: "https://pear.test",
          mode: "spaces",
          transport,
        }),
      ),
    );
    const program = Effect.gen(function* () {
      const runner = yield* Runner;
      const fiber = yield* Effect.forkChild(runner.run());
      yield* Effect.promise(async () => {
        for (let i = 0; i < 200 && seen.length < 2; i++) {
          await new Promise((r) => setTimeout(r, 5));
        }
      });
      yield* Fiber.interrupt(fiber);
      return yield* runner.cursor();
    });
    const cursor = await Effect.runPromise(
      program.pipe(Effect.provide(layers)),
    );
    expect(seen).toEqual(["a", "b"]);
    expect(cursor).toBe(3);
  });
});

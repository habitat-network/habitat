// @vitest-environment node
import { it } from "@effect/vitest";
import { Effect, Option } from "effect";
import { describe, expect } from "vitest";
import { SyncStore } from "./SyncStore";

const space = (name: string, nextDueAt: number) => ({
  space: `at://did:plc:a/space/com.example.board/${name}`,
  authority: "did:plc:a",
  nextDueAt,
  failures: 0,
});

describe("SyncStore.memory", () => {
  it.effect("returns due spaces oldest first, limited", () =>
    Effect.gen(function* () {
      const store = yield* SyncStore;
      yield* store.putSpace(space("c", 30));
      yield* store.putSpace(space("a", 10));
      yield* store.putSpace(space("b", 20));
      yield* store.putSpace(space("later", 100));
      const due = yield* store.dueSpaces(50, 2);
      expect(due.map((s) => s.space.split("/").at(-1))).toEqual(["a", "b"]);
    }).pipe(Effect.provide(SyncStore.memory)),
  );

  it.effect("removeSpace drops its repo states", () =>
    Effect.gen(function* () {
      const store = yield* SyncStore;
      const s = space("x", 0);
      yield* store.putSpace(s);
      yield* store.putRepo({
        space: s.space,
        did: "did:plc:b",
        rev: "1",
        ltHash: new Uint8Array(2048),
      });
      expect(yield* store.listRepoDids(s.space)).toEqual(["did:plc:b"]);
      yield* store.removeSpace(s.space);
      expect(Option.isNone(yield* store.getRepo(s.space, "did:plc:b"))).toBe(
        true,
      );
      expect(Option.isNone(yield* store.getSpace(s.space))).toBe(true);
    }).pipe(Effect.provide(SyncStore.memory)),
  );

  it.effect(
    "stores a copy of ltHash so callers can't mutate stored state",
    () =>
      Effect.gen(function* () {
        const store = yield* SyncStore;
        const ltHash = new Uint8Array(2048);
        yield* store.putRepo({ space: "s", did: "d", rev: "1", ltHash });
        ltHash[0] = 9;
        const stored = yield* store.getRepo("s", "d");
        expect(Option.getOrThrow(stored).ltHash[0]).toBe(0);
      }).pipe(Effect.provide(SyncStore.memory)),
  );
});

// @vitest-environment node
import { it } from "@effect/vitest";
import { Effect, Option } from "effect";
import { describe, expect } from "vitest";
import { syncRepo } from "./repoSync";
import { SyncStore } from "./SyncStore";
import { runWithHarness } from "./test/harness";

const listed = (space: { repoRevOf(d: string): string; spaceRevOf(d: string): string }, did: string) => ({
  did,
  repoRev: space.repoRevOf(did),
  spaceRev: space.spaceRevOf(did),
});

describe("syncRepo", () => {
  it.live("recovers a repo with no local state and stores its rev", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        const event = yield* syncRepo(space.ref, listed(space, alice.did));
        expect(Option.getOrThrow(event)._tag).toBe("Reset");
        expect(sink.view(space.ref, alice.did)).toEqual(space.expectedView(alice.did));
        const stored = yield* Effect.flatMap(SyncStore, (s) => s.getRepo(space.ref, alice.did));
        expect(Option.getOrThrow(stored).rev).toBe(space.repoRevOf(alice.did));
      }),
    ),
  );

  it.live("syncs new writes incrementally", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* syncRepo(space.ref, listed(space, alice.did));
        yield* Effect.promise(() => space.write(alice, "com.example.post", "2", { text: "b" }));
        const event = yield* syncRepo(space.ref, listed(space, alice.did));
        expect(Option.getOrThrow(event)._tag).toBe("Ops");
        expect(space.getRepoCalls).toBe(1);
        expect(sink.view(space.ref, alice.did)).toEqual(space.expectedView(alice.did));
      }),
    ),
  );

  it.live("collapses multiple ops on one path into the final change", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "keep", { text: "k" }));
        yield* syncRepo(space.ref, listed(space, alice.did));
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "v1" }));
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", null));
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "v3" }));
        yield* syncRepo(space.ref, listed(space, alice.did));
        expect(sink.batches.at(-1)?.paths).toEqual(["com.example.post/1"]);
        expect(sink.view(space.ref, alice.did)).toEqual(space.expectedView(alice.did));
      }),
    ),
  );

  it.live("falls back to recovery when the oplog no longer covers `since`", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* syncRepo(space.ref, listed(space, alice.did));
        yield* Effect.promise(() => space.write(alice, "com.example.post", "2", { text: "b" }));
        space.truncateOplog(alice.did);
        const event = yield* syncRepo(space.ref, listed(space, alice.did));
        expect(Option.getOrThrow(event)._tag).toBe("Reset");
        expect(sink.view(space.ref, alice.did)).toEqual(space.expectedView(alice.did));
      }),
    ),
  );

  it.live("falls back to recovery when the oplog commit fails verification", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* syncRepo(space.ref, listed(space, alice.did));
        yield* Effect.promise(() => space.write(alice, "com.example.post", "2", { text: "b" }));
        space.corruptOpsCommits.add(alice.did);
        const event = yield* syncRepo(space.ref, listed(space, alice.did));
        expect(Option.getOrThrow(event)._tag).toBe("Reset");
      }),
    ),
  );

  it.live("falls back to recovery when the local set hash has diverged", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        yield* syncRepo(space.ref, listed(space, alice.did));
        const store = yield* SyncStore;
        const local = Option.getOrThrow(yield* store.getRepo(space.ref, alice.did));
        const tampered = local.ltHash.slice();
        tampered[0] ^= 0xff;
        yield* store.putRepo({ ...local, ltHash: tampered });
        yield* Effect.promise(() => space.write(alice, "com.example.post", "2", { text: "b" }));
        const event = yield* syncRepo(space.ref, listed(space, alice.did));
        expect(Option.getOrThrow(event)._tag).toBe("Reset");
      }),
    ),
  );

  it.live("does not advance the store when the sink fails", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        sink.failNext = 1;
        const error = yield* Effect.flip(syncRepo(space.ref, listed(space, alice.did)));
        expect(error._tag).toBe("SinkError");
        const stored = yield* Effect.flatMap(SyncStore, (s) => s.getRepo(space.ref, alice.did));
        expect(Option.isNone(stored)).toBe(true);
      }),
    ),
  );

  it.live("fails with RepoSyncError when the host is behind the listed revision", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() => space.write(alice, "com.example.post", "1", { text: "a" }));
        const error = yield* Effect.flip(
          syncRepo(space.ref, { ...listed(space, alice.did), repoRev: "zzzzzzzzzzzzz" }),
        );
        expect(error._tag).toBe("RepoSyncError");
      }),
    ),
  );
});
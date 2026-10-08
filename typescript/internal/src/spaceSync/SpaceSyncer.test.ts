// @vitest-environment node
import { it } from "@effect/vitest";
import { Duration, Effect, Fiber, Option, Stream } from "effect";
import { describe, expect } from "vitest";
import { SpaceSyncer } from "./SpaceSyncer";
import { SyncStore } from "./SyncStore";
import { runWithSyncer } from "./test/harness";
import { com } from "api";
import { decodeLex } from "./wire";

/** The spaceRev of a notifyWrite body, as receiveNotifyWrite would pass it on. */
const decodeNotify = (body: unknown) =>
  decodeLex(com.atproto.space.notifyWrite.$input.schema)(body).pipe(
    Effect.map((input) => input.spaceRev),
    Effect.orDie,
  );

describe("SpaceSyncer", () => {
  it.live("watch performs an initial full sync and then goes idle", () =>
    runWithSyncer(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() =>
          space.write(alice, "com.example.post", "1", { text: "a" }),
        );
        const syncer = yield* SpaceSyncer;
        yield* syncer.watch(space.ref);
        yield* syncer.awaitIdle(space.ref);
        expect(sink.view(space.id, alice.did)).toEqual(
          space.expectedView(alice.did),
        );
        expect(yield* syncer.activeSpaces).toBe(0);
      }),
    ),
  );

  it.live(
    "notifyWrite triggers an incremental catch-up; stale notifications are dropped",
    () =>
      runWithSyncer(({ net, sink }) =>
        Effect.gen(function* () {
          const alice = yield* Effect.promise(() => net.createAccount("alice"));
          const space = net.createSpace(alice);
          yield* Effect.promise(() =>
            space.write(alice, "com.example.post", "1", { text: "a" }),
          );
          const syncer = yield* SpaceSyncer;
          yield* syncer.watch(space.ref);
          yield* syncer.awaitIdle(space.ref);
          const stale = yield* decodeNotify(space.notifyWriteBody(alice.did));
          const listsBefore = space.listReposCalls;
          yield* syncer.notifyWrite(space.ref, stale);
          yield* syncer.awaitIdle(space.ref);
          expect(space.listReposCalls).toBe(listsBefore);

          yield* Effect.promise(() =>
            space.write(alice, "com.example.post", "2", { text: "b" }),
          );
          yield* syncer.notifyWrite(
            space.ref,
            yield* decodeNotify(space.notifyWriteBody(alice.did)),
          );
          yield* syncer.awaitIdle(space.ref);
          expect(sink.batches.at(-1)?._tag).toBe("Ops");
          expect(sink.view(space.id, alice.did)).toEqual(
            space.expectedView(alice.did),
          );
        }),
      ),
  );

  it.live("coalesces a burst of notifications into one extra pass", () =>
    runWithSyncer(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() =>
          space.write(alice, "com.example.post", "0", { text: "0" }),
        );
        const syncer = yield* SpaceSyncer;
        yield* syncer.watch(space.ref);
        yield* syncer.awaitIdle(space.ref);
        sink.applyDelayMs = 50;
        const listsBefore = space.listReposCalls;
        const opsBefore = space.listRepoOpsCalls;
        for (let i = 1; i <= 50; i++) {
          yield* Effect.promise(() =>
            space.write(alice, "com.example.post", String(i), {
              text: String(i),
            }),
          );
          yield* syncer.notifyWrite(
            space.ref,
            yield* decodeNotify(space.notifyWriteBody(alice.did)),
          );
        }
        yield* syncer.awaitIdle(space.ref);
        // One pass for the first notification, at most one more for everything queued behind it.
        // Measured in productive passes (one listRepoOps per repo synced): the listRepos walk
        // itself paginates while the writer is active, so counting pages would measure the
        // walk, not the coalescing. 2 is the bound; 50 uncoalesced notifications would give ~50.
        expect(space.listRepoOpsCalls - opsBefore).toBeLessThanOrEqual(2);
        expect(space.listReposCalls - listsBefore).toBeLessThanOrEqual(25);
        expect(sink.view(space.id, alice.did)).toEqual(
          space.expectedView(alice.did),
        );
      }),
    ),
  );

  it.live(
    "notifySpaceDeleted drops the space from the sink and the store",
    () =>
      runWithSyncer(({ net, sink }) =>
        Effect.gen(function* () {
          const alice = yield* Effect.promise(() => net.createAccount("alice"));
          const space = net.createSpace(alice);
          yield* Effect.promise(() =>
            space.write(alice, "com.example.post", "1", { text: "a" }),
          );
          const syncer = yield* SpaceSyncer;
          yield* syncer.watch(space.ref);
          yield* syncer.awaitIdle(space.ref);
          yield* syncer.notifySpaceDeleted(space.ref);
          expect(sink.batches.at(-1)?._tag).toBe("SpaceDeleted");
          expect(
            Option.isNone(
              yield* Effect.flatMap(SyncStore, (s) => s.getSpace(space.id)),
            ),
          ).toBe(true);
        }),
      ),
  );

  it.live(
    "a SpaceDeleted credential error during a pass deletes the space",
    () =>
      runWithSyncer(({ net, sink }) =>
        Effect.gen(function* () {
          const alice = yield* Effect.promise(() => net.createAccount("alice"));
          const space = net.createSpace(alice);
          yield* Effect.promise(() =>
            space.write(alice, "com.example.post", "1", { text: "a" }),
          );
          space.deleted = true;
          const syncer = yield* SpaceSyncer;
          yield* syncer.watch(space.ref);
          yield* syncer.awaitIdle(space.ref);
          expect(sink.batches.map((b) => b._tag)).toEqual(["SpaceDeleted"]);
          expect(
            Option.isNone(
              yield* Effect.flatMap(SyncStore, (s) => s.getSpace(space.id)),
            ),
          ).toBe(true);
        }),
      ),
  );

  it.live("a failed pass backs off and the scheduler retries it", () =>
    runWithSyncer(({ net, sink }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() =>
          space.write(alice, "com.example.post", "1", { text: "a" }),
        );
        space.deniedUsers.add(alice.did);
        const syncer = yield* SpaceSyncer;
        yield* syncer.watch(space.ref);
        yield* syncer.awaitIdle(space.ref);
        const failed = Option.getOrThrow(
          yield* Effect.flatMap(SyncStore, (s) => s.getSpace(space.id)),
        );
        expect(failed.failures).toBe(1);
        space.deniedUsers.clear();
        yield* Effect.sleep("400 millis");
        yield* syncer.awaitIdle(space.ref);
        expect(sink.view(space.id, alice.did)).toEqual(
          space.expectedView(alice.did),
        );
      }),
    ),
  );

  it.live("never runs more than maxActiveSpaces passes at once", () =>
    runWithSyncer(
      ({ net, sink }) =>
        Effect.gen(function* () {
          const alice = yield* Effect.promise(() => net.createAccount("alice"));
          sink.applyDelayMs = 20;
          const spaces = [];
          for (let i = 0; i < 20; i++) {
            const space = net.createSpace(alice, `s${i}`);
            yield* Effect.promise(() =>
              space.write(alice, "com.example.post", "1", { text: "a" }),
            );
            spaces.push(space);
          }
          const syncer = yield* SpaceSyncer;
          yield* Effect.forEach(spaces, (s) => syncer.watch(s.ref), {
            discard: true,
          });
          yield* Effect.forEach(spaces, (s) => syncer.awaitIdle(s.ref), {
            discard: true,
          });
          expect(sink.maxConcurrentSpaces).toBeLessThanOrEqual(3);
          expect(sink.batches.filter((b) => b._tag === "Reset")).toHaveLength(
            20,
          );
          expect(yield* syncer.activeSpaces).toBe(0);
        }),
      { maxActiveSpaces: 3 },
    ),
  );

  it.live(
    "unwatch interrupts an in-flight sink apply and leaves no state",
    () =>
      runWithSyncer(({ net, sink }) =>
        Effect.gen(function* () {
          const alice = yield* Effect.promise(() => net.createAccount("alice"));
          const space = net.createSpace(alice);
          yield* Effect.promise(() =>
            space.write(alice, "com.example.post", "1", { text: "a" }),
          );
          sink.applyDelayMs = 500;
          const syncer = yield* SpaceSyncer;
          yield* syncer.watch(space.ref);
          yield* Effect.sleep("150 millis");
          yield* syncer.unwatch(space.ref);
          expect(sink.interrupted).toBe(1);
          expect(sink.batches).toHaveLength(0);
          const store = yield* SyncStore;
          expect(Option.isNone(yield* store.getSpace(space.id))).toBe(true);
          expect(yield* store.listRepoDids(space.id)).toEqual([]);
          expect(yield* syncer.activeSpaces).toBe(0);
        }),
      ),
  );

  it.live("publishes committed batches on the events stream", () =>
    runWithSyncer(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() =>
          space.write(alice, "com.example.post", "1", { text: "a" }),
        );
        const syncer = yield* SpaceSyncer;
        const collector = yield* syncer.events.pipe(
          Stream.take(1),
          Stream.runCollect,
          Effect.forkScoped,
        );
        yield* Effect.sleep("20 millis");
        yield* syncer.watch(space.ref);
        const events = yield* Fiber.join(collector);
        expect(events[0]).toEqual({
          _tag: "Reset",
          space: space.id,
          did: alice.did,
          rev: space.repoRevOf(alice.did),
        });
      }),
    ),
  );

  it.live("the scheduler renews registrations before they expire", () =>
    runWithSyncer(
      ({ net }) =>
        Effect.gen(function* () {
          const alice = yield* Effect.promise(() => net.createAccount("alice"));
          const space = net.createSpace(alice);
          space.registrationLifetimeMs = 150;
          yield* Effect.promise(() =>
            space.write(alice, "com.example.post", "1", { text: "a" }),
          );
          const syncer = yield* SpaceSyncer;
          yield* syncer.watch(space.ref);
          yield* Effect.sleep("500 millis");
          expect(space.registrations.length).toBeGreaterThanOrEqual(2);
        }),
      { registrationRenewLead: Duration.millis(100) },
    ),
  );

  it.live("a notification racing unwatch doesn't bring the space back", () =>
    runWithSyncer(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() =>
          space.write(alice, "com.example.post", "1", { text: "a" }),
        );
        const syncer = yield* SpaceSyncer;
        yield* syncer.watch(space.ref);
        yield* syncer.awaitIdle(space.ref);
        yield* Effect.promise(() =>
          space.write(alice, "com.example.post", "2", { text: "b" }),
        );
        yield* Effect.all(
          [syncer.unwatch(space.ref), syncer.notifyWrite(space.ref, undefined)],
          { concurrency: "unbounded" },
        );
        yield* syncer.awaitIdle(space.ref);
        const store = yield* SyncStore;
        expect(Option.isNone(yield* store.getSpace(space.id))).toBe(true);
        expect(yield* store.listRepoDids(space.id)).toEqual([]);
      }),
    ),
  );
});

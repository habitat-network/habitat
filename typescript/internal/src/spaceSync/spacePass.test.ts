// @vitest-environment node
import { it } from "@effect/vitest";
import { Duration, Effect, Option } from "effect";
import { describe, expect } from "vitest";
import type { SpaceRef, SpaceRefString } from "@atproto/syntax";
import { SpacePass } from "./spacePass";
import { SyncStore } from "./SyncStore";
import { type FakeSpace } from "./test/fakeNetwork";
import { SERVICE_DID, runWithHarness } from "./test/harness";

const track = (space: FakeSpace) =>
  Effect.flatMap(SyncStore, (s) =>
    s.putSpace({
      space: space.id,
      authority: space.authority.did,
      nextDueAt: 0,
      failures: 0,
    }),
  );
const stateOf = (space: FakeSpace) =>
  Effect.flatMap(SyncStore, (s) => s.getSpace(space.id)).pipe(
    Effect.map(Option.getOrThrow),
  );

const runSpacePass = (space: SpaceRef, full: boolean) =>
  Effect.flatMap(SpacePass, (p) => p.run(space, full));
const recordPassFailure = (space: SpaceRefString, error: unknown) =>
  Effect.flatMap(SpacePass, (p) => p.recordFailure(space, error));

describe("SpacePass.run", () => {
  it.live(
    "full pass syncs every writer, registers, and checkpoints at the newest spaceRev",
    () =>
      runWithHarness(({ net, sink }) =>
        Effect.gen(function* () {
          const [alice, bob] = yield* Effect.promise(() =>
            Promise.all([net.createAccount("alice"), net.createAccount("bob")]),
          );
          const space = net.createSpace(alice);
          yield* Effect.promise(() =>
            space.write(alice, "com.example.post", "1", { text: "a" }),
          );
          yield* Effect.promise(() =>
            space.write(bob, "com.example.post", "1", { text: "b" }),
          );
          yield* track(space);
          const events = yield* runSpacePass(space.ref, true);
          expect(events.map((e) => e._tag)).toEqual(["Reset", "Reset"]);
          expect(space.registrations).toEqual([SERVICE_DID]);
          const state = yield* stateOf(space);
          expect(state.spaceRev).toBe(space.spaceRevOf(bob.did));
          expect(state.failures).toBe(0);
          expect(state.lastFullPassAt).toBeDefined();
          expect(sink.view(space.id, bob.did)).toEqual(
            space.expectedView(bob.did),
          );
        }),
      ),
  );

  it.live("catch-up pass only touches writers after the checkpoint", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const [alice, bob] = yield* Effect.promise(() =>
          Promise.all([net.createAccount("alice"), net.createAccount("bob")]),
        );
        const space = net.createSpace(alice);
        yield* Effect.promise(() =>
          space.write(alice, "com.example.post", "1", { text: "a" }),
        );
        yield* Effect.promise(() =>
          space.write(bob, "com.example.post", "1", { text: "b" }),
        );
        yield* track(space);
        yield* runSpacePass(space.ref, true);
        yield* Effect.promise(() =>
          space.write(bob, "com.example.post", "2", { text: "b2" }),
        );
        const opsBefore = space.listRepoOpsCalls;
        const events = yield* runSpacePass(space.ref, false);
        expect(events).toEqual([
          expect.objectContaining({ _tag: "Ops", did: bob.did }),
        ]);
        expect(space.listRepoOpsCalls - opsBefore).toBe(1);
      }),
    ),
  );

  it.live("full pass prunes writers no longer listed", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const [alice, bob] = yield* Effect.promise(() =>
          Promise.all([net.createAccount("alice"), net.createAccount("bob")]),
        );
        const space = net.createSpace(alice);
        yield* Effect.promise(() =>
          space.write(alice, "com.example.post", "1", { text: "a" }),
        );
        yield* Effect.promise(() =>
          space.write(bob, "com.example.post", "1", { text: "b" }),
        );
        yield* track(space);
        yield* runSpacePass(space.ref, true);
        space.delisted.add(bob.did);
        const events = yield* runSpacePass(space.ref, true);
        expect(events).toContainEqual({
          _tag: "RepoRemoved",
          space: space.id,
          did: bob.did,
        });
        expect(
          yield* Effect.flatMap(SyncStore, (s) => s.listRepoDids(space.id)),
        ).toEqual([alice.did]);
        expect(sink.view(space.id, bob.did).size).toBe(0);
      }),
    ),
  );

  it.live(
    "checkpoints before the first failed repo and records a failure",
    () =>
      runWithHarness(({ net }) =>
        Effect.gen(function* () {
          const [alice, bob, carol] = yield* Effect.promise(() =>
            Promise.all([
              net.createAccount("alice"),
              net.createAccount("bob"),
              net.createAccount("carol"),
            ]),
          );
          const space = net.createSpace(alice);
          yield* Effect.promise(() =>
            space.write(alice, "com.example.post", "1", { text: "a" }),
          );
          yield* Effect.promise(() =>
            space.write(bob, "com.example.post", "1", { text: "b" }),
          );
          yield* Effect.promise(() =>
            space.write(carol, "com.example.post", "1", { text: "c" }),
          );
          space.failingRepos.add(bob.did);
          yield* track(space);
          const events = yield* runSpacePass(space.ref, true);
          expect(events.map((e) => "did" in e && e.did)).toEqual([
            alice.did,
            carol.did,
          ]);
          const failed = yield* stateOf(space);
          expect(failed.spaceRev).toBe(space.spaceRevOf(alice.did));
          expect(failed.failures).toBe(1);
          space.failingRepos.clear();
          // Bob is backing off after the failure; wait it out (testConfig's backoffBase).
          yield* Effect.sleep(Duration.millis(60));
          yield* runSpacePass(space.ref, false);
          const healed = yield* stateOf(space);
          expect(healed.spaceRev).toBe(space.spaceRevOf(carol.did));
          expect(healed.failures).toBe(0);
        }),
      ),
  );

  it.live("dedupes a writer that reappears in the listing", () =>
    runWithHarness(({ net, sink }) =>
      Effect.gen(function* () {
        const [alice, bob] = yield* Effect.promise(() =>
          Promise.all([net.createAccount("alice"), net.createAccount("bob")]),
        );
        const space = net.createSpace(alice);
        yield* Effect.promise(() =>
          space.write(alice, "com.example.post", "1", { text: "a" }),
        );
        yield* Effect.promise(() =>
          space.write(bob, "com.example.post", "1", { text: "b" }),
        );
        // One entry per page; alice writes again after the first page, so she
        // reappears later in the same walk with a newer repoRev.
        space.listReposPageSize = 1;
        let wrote = false;
        space.onListRepos = async () => {
          if (wrote) return;
          wrote = true;
          await space.write(alice, "com.example.post", "2", { text: "a2" });
        };
        yield* track(space);
        const events = yield* runSpacePass(space.ref, true);
        expect(
          events.filter((e) => "did" in e && e.did === alice.did),
        ).toHaveLength(1);
        expect(space.getRepoCalls).toBe(2);
        expect(sink.view(space.id, alice.did)).toEqual(
          space.expectedView(alice.did),
        );
      }),
    ),
  );

  it.live(
    "rejects an unordered listRepos response without advancing the checkpoint",
    () =>
      runWithHarness(({ net }) =>
        Effect.gen(function* () {
          const [alice, bob] = yield* Effect.promise(() =>
            Promise.all([net.createAccount("alice"), net.createAccount("bob")]),
          );
          const space = net.createSpace(alice);
          yield* Effect.promise(() =>
            space.write(alice, "com.example.post", "1", { text: "a" }),
          );
          yield* Effect.promise(() =>
            space.write(bob, "com.example.post", "1", { text: "b" }),
          );
          space.unorderedListRepos = true;
          yield* track(space);
          const error = yield* Effect.flip(runSpacePass(space.ref, true));
          expect(error._tag).toBe("XrpcError");
          expect((yield* stateOf(space)).spaceRev).toBeUndefined();
        }),
      ),
  );

  it.live("renews the registration when it is inside the renewal lead", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        space.registrationLifetimeMs = 30 * 60 * 1000; // inside the default 1h lead
        yield* Effect.promise(() =>
          space.write(alice, "com.example.post", "1", { text: "a" }),
        );
        yield* track(space);
        yield* runSpacePass(space.ref, true);
        yield* runSpacePass(space.ref, false);
        expect(space.registrations).toHaveLength(2);
      }),
    ),
  );

  it.live("recordPassFailure backs off exponentially up to the cap", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* track(space);
        const before = Date.now();
        for (let i = 0; i < 5; i++)
          yield* recordPassFailure(space.id, new Error("nope"));
        const state = yield* stateOf(space);
        expect(state.failures).toBe(5);
        expect(state.lastError).toBe("nope");
        // test config: base 50ms, cap 200ms
        expect(state.nextDueAt - before).toBeLessThanOrEqual(200 + 50);
      }),
    ),
  );

  /** Alice and Bob each write once; alice is the authority. */
  const twoWriters = Effect.fnUntraced(function* (
    net: Parameters<Parameters<typeof runWithHarness>[0]>[0]["net"],
  ) {
    const [alice, bob] = yield* Effect.promise(() =>
      Promise.all([net.createAccount("alice"), net.createAccount("bob")]),
    );
    const space = net.createSpace(alice);
    yield* Effect.promise(() =>
      space.write(bob, "com.example.post", "1", { text: "b" }),
    );
    yield* Effect.promise(() =>
      space.write(alice, "com.example.post", "1", { text: "a" }),
    );
    yield* track(space);
    return { alice, bob, space };
  });

  it.live(
    "a writer's repo host answering SpaceDeleted can't delete the space",
    () =>
      runWithHarness(({ net, sink }) =>
        Effect.gen(function* () {
          const { alice, bob, space } = yield* twoWriters(net);
          space.repoHostErrors.set(bob.did, "SpaceDeleted");
          yield* runSpacePass(space.ref, true);
          const state = yield* stateOf(space);
          expect(state.failures).toBe(1);
          expect(sink.batches.some((b) => b._tag === "SpaceDeleted")).toBe(
            false,
          );
          expect(sink.view(space.id, alice.did)).toEqual(
            space.expectedView(alice.did),
          );
        }),
      ),
  );

  it.live("an unavailable writer doesn't hold back the checkpoint", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const { alice, bob, space } = yield* twoWriters(net);
        space.repoHostErrors.set(bob.did, "RepoDeactivated");
        yield* runSpacePass(space.ref, true);
        const state = yield* stateOf(space);
        expect(state.spaceRev).toBe(space.spaceRevOf(alice.did));
        expect(state.failures).toBe(0);
      }),
    ),
  );

  it.live("a failing repo backs off instead of being retried every pass", () =>
    runWithHarness(
      ({ net }) =>
        Effect.gen(function* () {
          const { bob, space } = yield* twoWriters(net);
          space.failingRepos.add(bob.did);
          yield* runSpacePass(space.ref, true);
          const callsAfterFirst = space.getRepoCalls;
          // An immediate second pass (as a notification would trigger) skips bob.
          yield* runSpacePass(space.ref, false);
          expect(space.getRepoCalls).toBe(callsAfterFirst);
          const state = yield* stateOf(space);
          // Bob is first in spaceRev order, so the checkpoint can't move past him.
          expect(state.spaceRev).toBeUndefined();
          expect(state.nextDueAt).toBeGreaterThan(Date.now());
        }),
      { backoffBase: Duration.seconds(10), backoffCap: Duration.seconds(10) },
    ),
  );
});

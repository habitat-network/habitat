// @vitest-environment node
import { it } from "@effect/vitest";
import { Effect, Exit, Layer } from "effect";
import { TestClock } from "effect/testing";
import { describe, expect } from "vitest";
import { server } from "../test/msw";
import { Credentials } from "./Credentials";
import { Identity } from "./Identity";
import { FakeNetwork } from "./test/fakeNetwork";
import { fakeDelegation, testConfig } from "./test/harness";

const setup = Effect.promise(async () => {
  const net = new FakeNetwork();
  server.use(...net.handlers);
  const alice = await net.createAccount("alice");
  const space = net.createSpace(alice);
  const layer = Credentials.layer.pipe(
    Layer.provide(Identity.layer),
    Layer.provide(fakeDelegation(net)),
    Layer.provide(testConfig()),
  );
  return { net, alice, space, layer };
});

describe("Credentials", () => {
  it.effect("mints once and reuses the cached credential", () =>
    Effect.gen(function* () {
      const { space, layer } = yield* setup;
      yield* Effect.gen(function* () {
        yield* TestClock.setTime(Date.now());
        const credentials = yield* Credentials;
        const a = yield* credentials.get(space.ref);
        const b = yield* credentials.get(space.ref);
        expect(a.token).toBe(b.token);
        expect(space.credentialJtis).toHaveLength(1);
      }).pipe(Effect.provide(layer));
    }),
  );

  it.effect(
    "re-mints once the credential is within the refresh lead of expiry",
    () =>
      Effect.gen(function* () {
        const { space, layer } = yield* setup;
        yield* Effect.gen(function* () {
          yield* TestClock.setTime(Date.now());
          const credentials = yield* Credentials;
          yield* credentials.get(space.ref);
          yield* TestClock.adjust("10 minutes");
          yield* credentials.get(space.ref);
          expect(space.credentialJtis).toHaveLength(2);
        }).pipe(Effect.provide(layer));
      }),
  );

  it.effect("re-mints after invalidate", () =>
    Effect.gen(function* () {
      const { space, layer } = yield* setup;
      yield* Effect.gen(function* () {
        yield* TestClock.setTime(Date.now());
        const credentials = yield* Credentials;
        yield* credentials.get(space.ref);
        yield* credentials.invalidate(space.ref);
        yield* credentials.get(space.ref);
        expect(space.credentialJtis).toHaveLength(2);
      }).pipe(Effect.provide(layer));
    }),
  );

  it.effect("maps SpaceDeleted and UserNotAuthorized to typed reasons", () =>
    Effect.gen(function* () {
      const { alice, space, layer } = yield* setup;
      yield* Effect.gen(function* () {
        const credentials = yield* Credentials;
        space.deniedUsers.add(alice.did);
        const denied = yield* Effect.flip(credentials.get(space.ref));
        expect(denied.reason).toBe("NotAuthorized");
        space.deniedUsers.clear();
        space.deleted = true;
        const deleted = yield* Effect.flip(credentials.get(space.ref));
        expect(deleted.reason).toBe("SpaceDeleted");
      }).pipe(Effect.provide(layer));
    }),
  );

  it.effect("does not cache failures", () =>
    Effect.gen(function* () {
      const { alice, space, layer } = yield* setup;
      yield* Effect.gen(function* () {
        const credentials = yield* Credentials;
        space.deniedUsers.add(alice.did);
        expect(
          Exit.isFailure(yield* Effect.exit(credentials.get(space.ref))),
        ).toBe(true);
        space.deniedUsers.clear();
        expect(
          Exit.isSuccess(yield* Effect.exit(credentials.get(space.ref))),
        ).toBe(true);
      }).pipe(Effect.provide(layer));
    }),
  );
});

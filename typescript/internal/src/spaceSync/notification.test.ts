// @vitest-environment node
import { it } from "@effect/vitest";
import { Effect, Exit } from "effect";
import { describe, expect } from "vitest";
import {
  receiveConfirmRegistration,
  receiveNotifySpaceUpdate,
} from "./notification";
import { NotifyToken } from "./NotifyToken";
import { runWithHarness, runWithSyncer } from "./test/harness";

describe("receiveConfirmRegistration", () => {
  it.live("echoes the challenge only for the token we issued", () =>
    runWithHarness(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        const other = net.createSpace(alice, "other");
        const token = yield* (yield* NotifyToken).issue(space.id);

        expect(
          yield* receiveConfirmRegistration({
            space: space.id,
            token,
            challenge: "c1",
          }),
        ).toEqual({ challenge: "c1" });
        // The token is bound to its space and must be ours.
        for (const body of [
          { space: other.id, token, challenge: "c1" },
          { space: space.id, token: "forged", challenge: "c1" },
        ])
          expect(
            Exit.isFailure(
              yield* Effect.exit(receiveConfirmRegistration(body)),
            ),
          ).toBe(true);
      }),
    ),
  );
});

describe("receiveNotifySpaceUpdate", () => {
  it.live("rejects a bad token and ignores unwatched spaces", () =>
    runWithSyncer(({ net }) =>
      Effect.gen(function* () {
        const alice = yield* Effect.promise(() => net.createAccount("alice"));
        const space = net.createSpace(alice);
        yield* Effect.promise(() =>
          space.write(alice, "com.example.post", "1", { text: "a" }),
        );
        const token = yield* (yield* NotifyToken).issue(space.id);

        expect(
          Exit.isFailure(
            yield* Effect.exit(
              receiveNotifySpaceUpdate(
                space.notifySpaceUpdateBody(alice.did, "forged"),
              ),
            ),
          ),
        ).toBe(true);
        // Valid token, but the space isn't watched: dropped without any fetch.
        yield* receiveNotifySpaceUpdate(
          space.notifySpaceUpdateBody(alice.did, token),
        );
        expect(space.listReposCalls).toBe(0);
      }),
    ),
  );
});

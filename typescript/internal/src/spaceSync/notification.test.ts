// @vitest-environment node
import { it } from "@effect/vitest";
import { Secp256k1Keypair } from "@atproto/crypto";
import { Effect, Exit } from "effect";
import { describe, expect } from "vitest";
import { NotificationAuth, receiveNotifyWrite } from "./notification";
import { com } from "api";
import { SERVICE_DID, runWithHarness, runWithSyncer } from "./test/harness";

const LXM = "com.atproto.space.notifyWrite" as const;

describe("verifyNotification", () => {
  it.live(
    "accepts service auth from the space authority and rejects everything else",
    () =>
      runWithHarness(({ net }) =>
        Effect.gen(function* () {
          const [alice, mallory] = yield* Effect.promise(() =>
            Promise.all([
              net.createAccount("alice"),
              net.createAccount("mallory"),
            ]),
          );
          const space = net.createSpace(alice);
          const notificationAuth = yield* NotificationAuth;
          const check = (token: string | undefined) =>
            Effect.exit(
              notificationAuth.verify(
                token && `Bearer ${token}`,
                com.atproto.space.notifyWrite.main,
                space.ref,
              ),
            );
          const auth = (
            issuer = alice,
            extra: Partial<Parameters<typeof net.serviceAuth>[1]> = {},
          ) =>
            Effect.promise(() =>
              net.serviceAuth(issuer, { aud: SERVICE_DID, lxm: LXM, ...extra }),
            );
          const otherKey = yield* Effect.promise(() =>
            Secp256k1Keypair.create(),
          );

          expect(Exit.isSuccess(yield* check(yield* auth()))).toBe(true);
          expect(Exit.isFailure(yield* check(undefined))).toBe(true);
          expect(Exit.isFailure(yield* check(yield* auth(mallory)))).toBe(true);
          expect(
            Exit.isFailure(
              yield* check(yield* auth(alice, { aud: "did:web:other.test" })),
            ),
          ).toBe(true);
          expect(
            Exit.isFailure(
              yield* check(
                yield* auth(alice, {
                  lxm: "com.atproto.space.notifySpaceDeleted",
                }),
              ),
            ),
          ).toBe(true);
          expect(
            Exit.isFailure(yield* check(yield* auth(alice, { expSec: -10 }))),
          ).toBe(true);
          expect(
            Exit.isFailure(
              yield* check(yield* auth(alice, { signer: otherKey })),
            ),
          ).toBe(true);
        }),
      ),
  );

  it.live(
    "ignores notifications for unwatched spaces before any auth work",
    () =>
      runWithSyncer(({ net }) =>
        Effect.gen(function* () {
          const alice = yield* Effect.promise(() => net.createAccount("alice"));
          const space = net.createSpace(alice);
          yield* Effect.promise(() =>
            space.write(alice, "com.example.post", "1", { text: "a" }),
          );
          // No Authorization at all: this would fail if verification ran.
          yield* receiveNotifyWrite(
            space.notifyWriteBody(alice.did),
            undefined,
          );
          expect(space.listReposCalls).toBe(0);
        }),
      ),
  );
});

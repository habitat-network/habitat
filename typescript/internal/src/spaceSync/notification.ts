import { serviceAuth } from "@atproto/lex-server";
import type { DidString, SpaceRef } from "@atproto/syntax";
import { com } from "api";
import { Context, Effect, Layer, Option } from "effect";
import { SpaceSyncConfig } from "./config";
import { NotificationAuthError, errorMessage } from "./errors";
import { SpaceSyncer } from "./SpaceSyncer";
import { SyncStore } from "./SyncStore";
import { decodeLex, parseSpaceRef } from "./wire";

export type NotificationMethod =
  | typeof com.atproto.space.notifyWrite.main
  | typeof com.atproto.space.notifySpaceDeleted.main;

/** How long a seen nonce is remembered; longer than serviceAuth's 5 minute max token age. */
const NONCE_TTL_MS = 10 * 60 * 1000;

/**
 * Verifies the service-auth JWT on inbound notifications with
 * `@atproto/lex-server`'s `serviceAuth` (typ, exp/iat/nbf, aud, lxm, nonce
 * replay, signature with a DID-doc refresh on key rotation), then checks the
 * issuer is the space's authority.
 */
export class NotificationAuth extends Context.Service<
  NotificationAuth,
  {
    readonly verify: (
      authorization: string | undefined,
      method: NotificationMethod,
      space: SpaceRef,
    ) => Effect.Effect<void, NotificationAuthError>;
  }
>()("internal/spaceSync/NotificationAuth") {
  static readonly layer = Layer.effect(
    NotificationAuth,
    Effect.gen(function* () {
      const config = yield* SpaceSyncConfig;
      const nonces = new Map<string, number>();
      const auth = serviceAuth({
        audience: config.serviceDid as DidString,
        plcDirectoryUrl: config.plcUrl,
        unique: async (nonce) => {
          const now = Date.now();
          for (const [seen, expiresAt] of nonces)
            if (expiresAt <= now) nonces.delete(seen);
          if (nonces.has(nonce)) return false;
          nonces.set(nonce, now + NONCE_TTL_MS);
          return true;
        },
      });
      return NotificationAuth.of({
        verify: (authorization, method, space) =>
          Effect.tryPromise({
            try: async (signal) =>
              await auth({
                method,
                params: {},
                // serviceAuth reads only the Authorization header off the request.
                request: new Request(
                  `https://notification.invalid/xrpc/${method.nsid}`,
                  {
                    method: "POST",
                    headers: authorization ? { authorization } : {},
                    signal,
                  },
                ),
              }),
            catch: (error) =>
              new NotificationAuthError({ message: errorMessage(error) }),
          }).pipe(
            Effect.flatMap(({ did }) =>
              did === space.spaceDid
                ? Effect.void
                : Effect.fail(
                    new NotificationAuthError({
                      message: "issuer is not the space authority",
                    }),
                  ),
            ),
          ),
      });
    }),
  );
}

/**
 * Parse the body and drop notifications for spaces we don't watch before any
 * auth work: verification resolves the authority's DID, which unauthenticated
 * callers must not be able to trigger for arbitrary spaces.
 */
const watchedSpace = (space: string) =>
  Effect.gen(function* () {
    const ref = yield* parseSpaceRef(space);
    const store = yield* SyncStore;
    return Option.isSome(yield* store.getSpace(ref.toString()))
      ? Option.some(ref)
      : Option.none();
  });

/** Handle an inbound com.atproto.space.notifyWrite (raw JSON body + Authorization header). */
export const receiveNotifyWrite = Effect.fn("receiveNotifyWrite")(function* (
  body: unknown,
  authorization: string | undefined,
) {
  const method = com.atproto.space.notifyWrite;
  const input = yield* decodeLex(method.$input.schema)(body);
  const space = yield* watchedSpace(input.space);
  if (Option.isNone(space)) return;
  yield* Effect.flatMap(NotificationAuth, (a) =>
    a.verify(authorization, method.main, space.value),
  );
  yield* Effect.flatMap(SpaceSyncer, (s) =>
    s.notifyWrite(space.value, input.spaceRev),
  );
});

/** Handle an inbound com.atproto.space.notifySpaceDeleted (raw JSON body + Authorization header). */
export const receiveNotifySpaceDeleted = Effect.fn("receiveNotifySpaceDeleted")(
  function* (body: unknown, authorization: string | undefined) {
    const method = com.atproto.space.notifySpaceDeleted;
    const input = yield* decodeLex(method.$input.schema)(body);
    const space = yield* watchedSpace(input.space);
    if (Option.isNone(space)) return;
    yield* Effect.flatMap(NotificationAuth, (a) =>
      a.verify(authorization, method.main, space.value),
    );
    yield* Effect.flatMap(SpaceSyncer, (s) =>
      s.notifySpaceDeleted(space.value),
    );
  },
);

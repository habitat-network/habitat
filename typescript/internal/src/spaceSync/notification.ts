import { jsonToLex } from "@atproto/lex";
import { verifySignature } from "@atproto/crypto";
import type { SpaceRef } from "@atproto/syntax";
import { com } from "api";
import { Clock, Context, Effect, Layer, Option } from "effect";
import { SpaceSyncConfig } from "./config";
import { Identity, requireVerificationKey } from "./Identity";
import { NotificationAuthError, WireDecodeError, errorMessage } from "./errors";
import { SpaceSyncer } from "./SpaceSyncer";
import { SyncStore } from "./SyncStore";
import { parseSpaceRef } from "./wire";

export type NotificationMethod =
  | typeof com.atproto.space.notifyWrite.main
  | typeof com.atproto.space.notifySpaceDeleted.main;

/** An inbound request body as parsed JSON; `jsonToLex` turns `$bytes`/`$link` into lex values. */
type JsonBody = Parameters<typeof jsonToLex>[0];

/** How long a seen nonce is remembered; longer than serviceAuth's 5 minute max token age. */
const NONCE_TTL_MS = 10 * 60 * 1000;

/** Max token lifetime / clock skew, as in `@atproto/lex-server`'s serviceAuth. */
const MAX_AGE_SEC = 5 * 60;

interface ServiceJwt {
  readonly alg: string;
  readonly iss: string;
  readonly message: Uint8Array;
  readonly signature: Uint8Array;
}

const fail = (message: string) => new NotificationAuthError({ message });

const decodeBase64Url = (value: string): Uint8Array => {
  const b64 = value.replaceAll("-", "+").replaceAll("_", "/");
  const bin = atob(b64.padEnd(Math.ceil(b64.length / 4) * 4, "="));
  return Uint8Array.from(bin, (c) => c.charCodeAt(0));
};

const decodeJson = (value: string): Record<string, unknown> => {
  const parsed: unknown = JSON.parse(
    new TextDecoder().decode(decodeBase64Url(value)),
  );
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed))
    throw new Error("not an object");
  return parsed as Record<string, unknown>;
};

/**
 * Verifies the service-auth JWT on inbound notifications: typ, exp/iat/nbf,
 * aud, lxm, nonce replay, that the issuer is the space's authority, and the
 * signature against the authority's `#atproto` key. The key comes from
 * `Identity`, so it resolves through the caller's identity resolver (with a
 * DID-doc refresh on key rotation) rather than the public network.
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
      const identity = yield* Identity;
      // `unique` runs outside Effect, so read the Clock service directly.
      const clock = yield* Clock.Clock;
      const nonces = new Map<string, number>();
      const unique = (nonce: string): boolean => {
        const now = clock.currentTimeMillisUnsafe();
        for (const [seen, expiresAt] of nonces)
          if (expiresAt <= now) nonces.delete(seen);
        if (nonces.has(nonce)) return false;
        nonces.set(nonce, now + NONCE_TTL_MS);
        return true;
      };

      // Everything checkable without resolving the issuer's DID.
      const parse = (
        authorization: string | undefined,
        method: NotificationMethod,
      ): ServiceJwt => {
        if (!authorization?.startsWith("Bearer "))
          throw fail("Bearer token required");
        const parts = authorization.slice("Bearer ".length).trim().split(".");
        if (parts.length !== 3) throw fail("Invalid JWT token");
        const [headerB64, payloadB64, signatureB64] = parts as [
          string,
          string,
          string,
        ];
        let header: Record<string, unknown>;
        let payload: Record<string, unknown>;
        let signature: Uint8Array;
        try {
          header = decodeJson(headerB64);
          payload = decodeJson(payloadB64);
          signature = decodeBase64Url(signatureB64);
        } catch {
          throw fail("Invalid JWT token");
        }
        const { alg, typ } = header;
        // Service tokens are not OAuth access tokens, refresh tokens or DPoP proofs.
        if (
          typeof alg !== "string" ||
          alg === "none" ||
          (typ !== undefined &&
            (typeof typ !== "string" ||
              ["at+jwt", "refresh+jwt", "dpop+jwt"].includes(typ)))
        )
          throw fail("Invalid JWT token");
        const { iss, aud, exp, iat, nbf, lxm, nonce } = payload;
        if (aud !== config.serviceDid) throw fail("Invalid audience");
        if (typeof iss !== "string" || typeof exp !== "number")
          throw fail("Invalid JWT token");
        const now = Math.floor(clock.currentTimeMillisUnsafe() / 1000);
        if (typeof nbf === "number" && now < nbf)
          throw fail("JWT token not yet valid");
        if (now > exp) throw fail("JWT token expired");
        if (
          Math.abs(exp - now) > MAX_AGE_SEC ||
          typeof iat !== "number" ||
          Math.abs(now - iat) > MAX_AGE_SEC
        )
          throw fail("JWT token too old");
        if (lxm != null && lxm !== method.nsid)
          throw fail('Invalid JWT lexicon method ("lxm")');
        if (nonce != null && (typeof nonce !== "string" || !unique(nonce)))
          throw fail("Replay attack detected: nonce is not unique");
        return {
          alg,
          iss,
          message: new TextEncoder().encode(`${headerB64}.${payloadB64}`),
          signature,
        };
      };

      const signatureValid = (jwt: ServiceJwt, signingKey: string) =>
        Effect.promise(() =>
          verifySignature(signingKey, jwt.message, jwt.signature, {
            jwtAlg: jwt.alg,
            allowMalleableSig: true,
          }).catch(() => false),
        );

      return NotificationAuth.of({
        verify: Effect.fn("NotificationAuth.verify")(
          function* (authorization, method, space) {
            const jwt = yield* Effect.try({
              try: () => parse(authorization, method),
              catch: (error) =>
                error instanceof NotificationAuthError
                  ? error
                  : fail(errorMessage(error)),
            });
            // The issuer must be the space's authority; checked before
            // resolving so callers can't trigger lookups of arbitrary DIDs.
            if (jwt.iss !== space.spaceDid)
              return yield* fail("issuer is not the space authority");
            const resolveWith = (
              resolve: typeof identity.resolve | typeof identity.refresh,
            ) =>
              resolve(space.spaceDid).pipe(
                Effect.mapError((e) => fail(e.message)),
              );
            const cached = yield* resolveWith(identity.resolve);
            const cachedKey = yield* requireVerificationKey(cached).pipe(
              Effect.mapError((e) => fail(e.message)),
            );
            if (yield* signatureValid(jwt, cachedKey)) return;
            // Try a fresh DID document in case the authority rotated its key.
            const fresh = yield* resolveWith(identity.refresh);
            const freshKey = yield* requireVerificationKey(fresh).pipe(
              Effect.mapError((e) => fail(e.message)),
            );
            if (
              freshKey === cachedKey ||
              !(yield* signatureValid(jwt, freshKey))
            )
              return yield* fail("Invalid JWT signature");
          },
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
const watchedSpace = Effect.fnUntraced(function* (space: string) {
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
  const input = yield* Effect.try({
    try: () => method.$input.schema.parse(jsonToLex(body as JsonBody)),
    catch: (error) => new WireDecodeError({ message: errorMessage(error) }),
  });
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
    const input = yield* Effect.try({
      try: () => method.$input.schema.parse(jsonToLex(body as JsonBody)),
      catch: (error) => new WireDecodeError({ message: errorMessage(error) }),
    });
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

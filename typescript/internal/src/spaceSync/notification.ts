import type { SpaceRefString } from "@atproto/syntax";
import { verifySignature } from "@atproto/crypto";
import { fromBase64 } from "@atproto/lex";
import { Clock, Effect, Schema } from "effect";
import { NotificationAuthError } from "./errors";
import { Identity } from "./Identity";
import { parseSpaceRef } from "./wire";

export type NotificationLxm =
  "com.atproto.space.notifyWrite" | "com.atproto.space.notifySpaceDeleted";

const ServiceAuthPayload = Schema.Struct({
  iss: Schema.String,
  aud: Schema.String,
  exp: Schema.Number,
  lxm: Schema.optional(Schema.String),
});

const decodePart = (part: string) =>
  Effect.try({
    try: () =>
      JSON.parse(
        new TextDecoder().decode(fromBase64(part, "base64url")),
      ) as unknown,
    catch: () => new NotificationAuthError({ message: "malformed jwt" }),
  });

/**
 * Verify the service-auth JWT on an inbound notifyWrite / notifySpaceDeleted:
 * issued by the space authority, addressed to us, for this method, unexpired,
 * and signed by the authority's #atproto key.
 */
export const verifyNotification = Effect.fn("verifyNotification")(function* (
  authorization: string | undefined,
  opts: {
    readonly lxm: NotificationLxm;
    readonly space: SpaceRefString;
    readonly serviceDid: string;
  },
) {
  const fail = (message: string) => new NotificationAuthError({ message });
  const token = authorization?.match(/^Bearer (.+)$/)?.[1];
  if (!token) return yield* fail("missing bearer token");
  const parts = token.split(".");
  if (parts.length !== 3) return yield* fail("malformed jwt");
  const [head, body, sig] = parts;
  yield* decodePart(head);
  const payload = yield* decodePart(body).pipe(
    Effect.flatMap(Schema.decodeUnknownEffect(ServiceAuthPayload)),
    Effect.mapError(() => fail("malformed jwt payload")),
  );
  const { authority } = yield* parseSpaceRef(opts.space).pipe(
    Effect.mapError(() => fail("invalid space")),
  );
  if (payload.iss.split("#")[0] !== authority)
    return yield* fail("issuer is not the space authority");
  if (payload.aud !== opts.serviceDid) return yield* fail("wrong audience");
  if (payload.lxm !== opts.lxm) return yield* fail("wrong lxm");
  const now = yield* Clock.currentTimeMillis;
  if (payload.exp * 1000 <= now) return yield* fail("token expired");
  const identity = yield* Identity;
  const { signingKey } = yield* identity
    .resolve(authority)
    .pipe(Effect.mapError((e) => fail(e.message)));
  const valid = yield* Effect.promise(() =>
    verifySignature(
      signingKey,
      new TextEncoder().encode(`${head}.${body}`),
      fromBase64(sig, "base64url"),
    ).catch(() => false),
  );
  if (!valid) return yield* fail("bad signature");
});

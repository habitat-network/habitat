import { jsonToLex } from "@atproto/lex";
import type { SpaceRef } from "@atproto/syntax";
import { com } from "api";
import { Effect, Option } from "effect";
import { Credentials } from "./Credentials";
import { NotificationAuthError, WireDecodeError, errorMessage } from "./errors";
import { NotifyToken } from "./NotifyToken";
import { SpaceSyncer } from "./SpaceSyncer";
import { SyncStore } from "./SyncStore";
import { parseSpaceRef } from "./wire";

/** An inbound request body as parsed JSON; `jsonToLex` turns `$bytes`/`$link` into lex values. */
type JsonBody = Parameters<typeof jsonToLex>[0];

const parseFailure = (error: unknown) =>
  new WireDecodeError({ message: errorMessage(error) });

/** Fails unless `token` is the one we gave the space host for `space`. */
const requireToken = Effect.fnUntraced(function* (
  space: SpaceRef,
  token: string,
) {
  const valid = yield* Effect.flatMap(NotifyToken, (t) =>
    t.verify(space.toString(), token),
  );
  if (!valid)
    return yield* new NotificationAuthError({ message: "invalid token" });
});

/**
 * Handle an inbound com.atproto.space.confirmRegistration: a space host asking
 * whether we registered `token` for `space` before it delivers notifications
 * there. Answers with the challenge, which is the confirmation.
 */
export const receiveConfirmRegistration = Effect.fn(
  "receiveConfirmRegistration",
)(function* (body: unknown) {
  const input = yield* Effect.try({
    try: () =>
      com.atproto.space.confirmRegistration.$input.schema.parse(
        jsonToLex(body as JsonBody),
      ),
    catch: parseFailure,
  });
  const space = yield* parseSpaceRef(input.space);
  yield* requireToken(space, input.token);
  return { challenge: input.challenge };
});

/**
 * Handle an inbound com.atproto.space.notifySpaceUpdate. The token in the body
 * is the only authentication, so it is checked first: it is derived from the
 * space, which makes the check free of lookups, and spaces we don't watch are
 * dropped after it.
 */
export const receiveNotifySpaceUpdate = Effect.fn("receiveNotifySpaceUpdate")(
  function* (body: unknown) {
    const input = yield* Effect.try({
      try: () =>
        com.atproto.space.notifySpaceUpdate.$input.schema.parse(
          jsonToLex(body as JsonBody),
        ),
      catch: parseFailure,
    });
    const space = yield* parseSpaceRef(input.space);
    yield* requireToken(space, input.token);
    const store = yield* SyncStore;
    if (Option.isNone(yield* store.getSpace(space.toString()))) return;
    const syncer = yield* SpaceSyncer;
    const { update } = input;
    switch (update.$type) {
      // Not taken on the token's word: the host answers SpaceDeleted when we
      // ask for a fresh credential, and a pass that gets that drops the space.
      case "com.atproto.space.notifySpaceUpdate#spaceDeleted":
        yield* Effect.flatMap(Credentials, (c) => c.invalidate(space));
        return yield* syncer.notifyWrite(space, undefined);
      // A write, a removal and a lapsed registration all mean the space moved
      // on without us hearing every change, which a catch-up pass resolves
      // (and, for the last, re-registers through the credential it mints). An
      // update type we don't know still says something changed.
      default:
        return yield* syncer.notifyWrite(
          space,
          "spaceRev" in update && typeof update.spaceRev === "string"
            ? update.spaceRev
            : undefined,
        );
    }
  },
);

/**
 * The HTTP status to answer a notification that failed with `error`. A space
 * host retries a notification on 5xx and gives up on 4xx, so only a rejected
 * caller (401) or a malformed body (400) is permanent; anything else, such as
 * a sink or store failure while dropping the space, is a 500 so the delivery
 * is retried.
 */
export const notificationErrorStatus = (error: unknown): 400 | 401 | 500 => {
  const tag =
    typeof error === "object" && error !== null && "_tag" in error
      ? error._tag
      : undefined;
  switch (tag) {
    case "NotificationAuthError":
      return 401;
    case "WireDecodeError":
    case "InvalidSpaceRefError":
      return 400;
    default:
      return 500;
  }
};

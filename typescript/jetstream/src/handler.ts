import { Effect } from "effect";

/**
 * What a handler may return: an Effect, a promise, or nothing. Handlers are
 * written whichever way suits the app; the runner runs them all as Effects.
 */
export type HandlerResult<E = unknown> =
  Effect.Effect<void, E> | PromiseLike<void> | void;

/** toEffect runs f, whatever it returns, as an Effect. */
export function toEffect<A, E>(
  f: () => Effect.Effect<A, E> | PromiseLike<A> | A,
): Effect.Effect<A, unknown> {
  return Effect.suspend(() => {
    const out = f();
    if (Effect.isEffect(out)) return out as Effect.Effect<A, E>;
    if (
      out !== null &&
      typeof out === "object" &&
      "then" in out &&
      typeof (out as PromiseLike<A>).then === "function"
    ) {
      return Effect.tryPromise({
        try: () => Promise.resolve(out as PromiseLike<A>),
        catch: (cause) => cause,
      });
    }
    return Effect.succeed(out as A);
  });
}

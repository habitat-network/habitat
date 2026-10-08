import {
  type Agent,
  type InferOutput,
  type Schema,
  type XrpcFailure,
  XrpcInvalidResponseError,
  XrpcResponseError,
  jsonToLex,
} from "@atproto/lex";
import { SpaceRef, isSpaceRefString } from "@atproto/syntax";
import { Effect } from "effect";
import { InvalidSpaceRefError, WireDecodeError } from "./errors";

/**
 * An xrpc Agent for `service` that adds the space signature headers from
 * `sign` to every request. Redirects are refused: the signature is addressed
 * to one host and must not be replayed to another.
 */
export const signedAgent = (
  service: string,
  sign: () => Promise<Record<string, string>>,
): Agent => ({
  fetchHandler: async (path, init) => {
    const headers = new Headers(init.headers);
    for (const [name, value] of Object.entries(await sign()))
      headers.set(name, value);
    return fetch(new URL(path, service), {
      ...init,
      headers,
      redirect: "error",
    });
  },
});

/** HTTP status of a failed xrpc call; 0 when no response arrived. */
export const failureStatus = (failure: XrpcFailure): number =>
  failure instanceof XrpcResponseError ||
  failure instanceof XrpcInvalidResponseError
    ? failure.response.status
    : 0;

/**
 * Validate inbound atproto JSON (`{$bytes}`, `{$link}`) against a lexicon
 * schema, converting it to lex values first.
 */
export const decodeLex =
  <S extends Schema>(schema: S) =>
  (json: unknown): Effect.Effect<InferOutput<S>, WireDecodeError> =>
    Effect.suspend(() => {
      const result = schema.safeParse(
        jsonToLex(json as Parameters<typeof jsonToLex>[0]),
      );
      return result.success
        ? Effect.succeed(result.value as InferOutput<S>)
        : Effect.fail(new WireDecodeError({ message: result.reason.message }));
    });

/**
 * Parse an untrusted string into a SpaceRef, once, at the edge. `isSpaceRefString`
 * accepts anything round-trippable; the round trip through `SpaceRef` is the exact
 * re-check. Everything past the edge passes the SpaceRef around.
 */
export const parseSpaceRef = (
  space: string,
): Effect.Effect<SpaceRef, InvalidSpaceRefError> =>
  Effect.suspend(() => {
    if (!isSpaceRefString(space))
      return Effect.fail(new InvalidSpaceRefError({ space }));
    const ref = SpaceRef.parse(space);
    return ref.toString() === space
      ? Effect.succeed(ref)
      : Effect.fail(new InvalidSpaceRefError({ space }));
  });

/** listRepoOps / getRepo error codes meaning the repo itself is unavailable. */
export const REPO_UNAVAILABLE_CODES: ReadonlySet<string> = new Set([
  "RepoNotFound",
  "RepoTakendown",
  "RepoSuspended",
  "RepoDeactivated",
]);

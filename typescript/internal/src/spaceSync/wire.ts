import {
  type Agent,
  type XrpcFailure,
  XrpcInvalidResponseError,
  XrpcResponseError,
} from "@atproto/lex";
import { SpaceRef, isSpaceRefString } from "@atproto/syntax";
import { Effect } from "effect";
import { InvalidSpaceRefError } from "./errors";

/**
 * fetch that refuses redirects. `redirect: "error"` would say the same but
 * workerd throws on it, so follow nothing and reject any 3xx ourselves.
 */
export const fetchNoRedirect = async (
  input: URL | string,
  init: RequestInit = {},
): Promise<Response> => {
  const res = await fetch(input, { ...init, redirect: "manual" });
  if (res.type === "opaqueredirect" || (res.status >= 300 && res.status < 400))
    throw new TypeError(`unexpected redirect (${res.status}) from ${input}`);
  return res;
};

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
    return fetchNoRedirect(new URL(path, service), { ...init, headers });
  },
});

/** HTTP status of a failed xrpc call; 0 when no response arrived. */
export const failureStatus = (failure: XrpcFailure): number =>
  failure instanceof XrpcResponseError ||
  failure instanceof XrpcInvalidResponseError
    ? failure.response.status
    : 0;

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

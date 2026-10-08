import {
  type Agent,
  type InferOutput,
  type Schema,
  type XrpcFailure,
  XrpcInvalidResponseError,
  XrpcResponseError,
  jsonToLex,
} from "@atproto/lex";
import {
  SpaceRef,
  isSpaceRefString,
  type DidString,
  type NsidString,
  type RecordKeyString,
  type SpaceRefString,
} from "@atproto/syntax";
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
 * Narrow an untrusted string to a SpaceRefString. `isSpaceRefString` accepts anything
 * round-trippable; `SpaceRef.parse` is the exact re-check.
 */
export const toSpaceRef = (
  space: string,
): Effect.Effect<SpaceRefString, InvalidSpaceRefError> =>
  isSpaceRefString(space) && SpaceRef.parse(space).toString() === space
    ? Effect.succeed(space)
    : Effect.fail(new InvalidSpaceRefError({ space }));

export const parseSpaceRef = (
  space: string,
): Effect.Effect<
  { authority: DidString; type: NsidString; skey: RecordKeyString },
  InvalidSpaceRefError
> =>
  Effect.flatMap(toSpaceRef(space), (ref) =>
    Effect.try({
      try: () => {
        const parsed = SpaceRef.parse(ref);
        return {
          authority: parsed.spaceDid,
          type: parsed.spaceType,
          skey: parsed.skey,
        };
      },
      catch: () => new InvalidSpaceRefError({ space }),
    }),
  );

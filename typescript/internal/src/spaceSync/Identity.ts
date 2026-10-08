import { getServiceEndpoint } from "@atproto/common-web";
import type { DidString } from "@atproto/syntax";
import {
  type DidDocument,
  IdResolver,
  getKey,
  getPds,
} from "@atproto/identity";
import { Cache, Context, Duration, Effect, Exit, Layer } from "effect";
import { SpaceSyncConfig } from "./config";
import { IdentityError, errorMessage } from "./errors";

export interface ResolvedIdentity {
  readonly did: DidString;
  /** Repo host for this DID's space repos. */
  readonly pds: string;
  /** `#atproto` signing key as a did:key. Verifies commits and service auth. */
  readonly signingKey: string;
  /** Space host when this DID is a space authority (`#atproto_space_host`, else the PDS). */
  readonly spaceHost: string;
}

const hasEntry = (
  entries: ReadonlyArray<{ readonly id: string }> | undefined,
  did: DidString,
  fragment: string,
): boolean =>
  (entries ?? []).some(
    (entry) => entry.id === fragment || entry.id === `${did}${fragment}`,
  );

/**
 * Applies the proposal's fallback rules: a missing #atproto_space_host falls back
 * to #atproto_pds, but a present-and-malformed one is an error.
 */
export const identityFromDoc = (
  did: DidString,
  doc: DidDocument,
): Effect.Effect<ResolvedIdentity, IdentityError> =>
  Effect.gen(function* () {
    const pds = getPds(doc);
    if (!pds)
      return yield* new IdentityError({
        did,
        message: "missing #atproto_pds service",
      });
    const signingKey = getKey(doc);
    if (!signingKey)
      return yield* new IdentityError({
        did,
        message: "missing #atproto signing key",
      });
    let spaceHost = pds;
    if (hasEntry(doc.service, did, "#atproto_space_host")) {
      const endpoint = getServiceEndpoint(doc, {
        id: "#atproto_space_host",
        type: "AtprotoSpaceHost",
      });
      if (!endpoint)
        return yield* new IdentityError({
          did,
          message: "malformed #atproto_space_host service",
        });
      spaceHost = endpoint;
    }
    return { did, pds, signingKey, spaceHost };
  });

export class Identity extends Context.Service<
  Identity,
  {
    readonly resolve: (
      did: DidString,
    ) => Effect.Effect<ResolvedIdentity, IdentityError>;
  }
>()("internal/spaceSync/Identity") {
  static readonly layer = Layer.effect(
    Identity,
    Effect.gen(function* () {
      const { plcUrl } = yield* SpaceSyncConfig;
      const resolver = new IdResolver({ plcUrl });
      const cache = yield* Cache.makeWith(
        (did: DidString) =>
          Effect.tryPromise({
            try: () => resolver.did.ensureResolve(did),
            catch: (error) =>
              new IdentityError({
                did,
                message: `could not resolve: ${errorMessage(error)}`,
              }),
          }).pipe(Effect.flatMap((doc) => identityFromDoc(did, doc))),
        {
          capacity: 10_000,
          // Never cache failures: a transient PLC outage must not stick for 10 minutes.
          timeToLive: (exit) =>
            Exit.isSuccess(exit) ? Duration.minutes(10) : Duration.zero,
        },
      );
      return Identity.of({ resolve: (did) => Cache.get(cache, did) });
    }),
  );
}

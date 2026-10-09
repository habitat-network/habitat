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
import { fetchNoRedirect } from "./wire";

const DID_ACCEPT = "application/did+ld+json,application/json";

/**
 * did:plc and did:web resolvers that fetch via `fetchNoRedirect`.
 * @atproto/identity's own resolvers pass `redirect: "error"`, which workerd rejects.
 */
export const noRedirectDidMethods = (plcUrl: string, timeout: number) => ({
  plc: {
    resolveNoCheck: async (did: string): Promise<unknown> => {
      const res = await fetchNoRedirect(
        new URL(`/${encodeURIComponent(did)}`, plcUrl),
        {
          headers: { accept: DID_ACCEPT },
          signal: AbortSignal.timeout(timeout),
        },
      );
      if (res.status === 404) return null;
      if (!res.ok)
        throw Object.assign(new Error(res.statusText), { status: res.status });
      return res.json();
    },
  },
  web: {
    resolveNoCheck: async (did: string): Promise<unknown> => {
      const parts = did.split(":").slice(2).map(decodeURIComponent);
      if (parts.length !== 1) throw new Error(`unsupported did:web ${did}`);
      const url = new URL(`https://${parts[0]}/.well-known/did.json`);
      if (url.hostname === "localhost") url.protocol = "http";
      const res = await fetchNoRedirect(url, {
        headers: { accept: DID_ACCEPT },
        signal: AbortSignal.timeout(timeout),
      });
      // Positively not found, versus due to e.g. network error
      if (!res.ok) return null;
      return res.json();
    },
  },
});

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
export const identityFromDoc = Effect.fnUntraced(function* (
  did: DidString,
  doc: DidDocument,
): Effect.fn.Return<ResolvedIdentity, IdentityError> {
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
    /** Drop the cached document and resolve again, e.g. after a signature fails (key rotation). */
    readonly refresh: (
      did: DidString,
    ) => Effect.Effect<ResolvedIdentity, IdentityError>;
  }
>()("internal/spaceSync/Identity") {
  static readonly layer = Layer.effect(
    Identity,
    Effect.gen(function* () {
      const { plcUrl } = yield* SpaceSyncConfig;
      const resolver = new IdResolver({ plcUrl });
      for (const [method, impl] of Object.entries(
        noRedirectDidMethods(plcUrl, 3000),
      ))
        resolver.did.methods.set(method, impl as never);
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
      return Identity.of({
        resolve: (did) => Cache.get(cache, did),
        refresh: (did) =>
          Cache.invalidate(cache, did).pipe(
            Effect.andThen(Cache.get(cache, did)),
          ),
      });
    }),
  );
}

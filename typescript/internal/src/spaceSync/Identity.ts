import { getServiceEndpoint } from "@atproto/common-web";
import type { DidString } from "@atproto/syntax";
import {
  DOC_PATH,
  type DidDocument,
  DidWebResolver,
  IdResolver,
  PoorlyFormattedDidError,
  UnsupportedDidWebPathError,
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

/** Matches the library's own default. */
const DID_WEB_TIMEOUT_MS = 3000;

/**
 * did:web resolution that refuses redirects with `redirect: "manual"` and a
 * status check. The library's resolver passes `redirect: "error"`, which
 * Cloudflare Workers reject outright ("error" isn't implemented at the edge),
 * so every did:web lookup would fail there. Otherwise the same as
 * DidWebResolver: no did:web paths, and plain http for localhost.
 */
export class ManualRedirectDidWebResolver extends DidWebResolver {
  override async resolveNoCheck(did: string): Promise<unknown> {
    const parts = did.split(":").slice(2).map(decodeURIComponent);
    if (parts.length < 1 || !parts[0]) throw new PoorlyFormattedDidError(did);
    if (parts.length > 1) throw new UnsupportedDidWebPathError(did);
    const url = new URL(`https://${parts[0]}${DOC_PATH}`);
    if (url.hostname === "localhost") url.protocol = "http";
    const res = await fetch(url, {
      signal: AbortSignal.timeout(this.timeout),
      redirect: "manual",
      headers: { accept: "application/did+ld+json,application/json" },
    });
    if (
      res.type === "opaqueredirect" ||
      (res.status >= 300 && res.status < 400)
    )
      throw new Error(`did:web document for ${did} redirected`);
    // Positively not found, versus e.g. a network error (which throws).
    if (!res.ok) return null;
    return res.json();
  }
}

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
      const resolver = new IdResolver({ plcUrl, timeout: DID_WEB_TIMEOUT_MS });
      resolver.did.methods.set(
        "web",
        new ManualRedirectDidWebResolver(DID_WEB_TIMEOUT_MS),
      );
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

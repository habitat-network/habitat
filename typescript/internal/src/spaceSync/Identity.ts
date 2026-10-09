import { getServiceEndpoint } from "@atproto/common-web";
import type { DidString } from "@atproto/syntax";
import {
  DOC_PATH,
  type DidDocument,
  DidPlcResolver,
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
import { fetchNoRedirect } from "./wire";

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
const DID_TIMEOUT_MS = 3000;

const DID_DOC_ACCEPT = "application/did+ld+json,application/json";

// The library's did:web and did:plc resolvers fetch with `redirect: "error"`,
// which Cloudflare Workers reject outright, so every lookup would fail there.
// These two behave the same but refuse redirects through fetchNoRedirect.

/** did:web: no did:web paths, and plain http for localhost, as upstream. */
export class NoRedirectDidWebResolver extends DidWebResolver {
  override async resolveNoCheck(did: string): Promise<unknown> {
    const parts = did.split(":").slice(2).map(decodeURIComponent);
    if (parts.length < 1 || !parts[0]) throw new PoorlyFormattedDidError(did);
    if (parts.length > 1) throw new UnsupportedDidWebPathError(did);
    const url = new URL(`https://${parts[0]}${DOC_PATH}`);
    if (url.hostname === "localhost") url.protocol = "http";
    const res = await fetchNoRedirect(url, {
      signal: AbortSignal.timeout(this.timeout),
      headers: { accept: DID_DOC_ACCEPT },
    });
    // Positively not found, versus e.g. a network error (which throws).
    if (!res.ok) return null;
    return res.json();
  }
}

/** did:plc: a 404 is not found, any other failure throws, as upstream. */
export class NoRedirectDidPlcResolver extends DidPlcResolver {
  override async resolveNoCheck(did: string): Promise<unknown> {
    const res = await fetchNoRedirect(
      new URL(`/${encodeURIComponent(did)}`, this.plcUrl),
      {
        signal: AbortSignal.timeout(this.timeout),
        headers: { accept: DID_DOC_ACCEPT },
      },
    );
    if (res.status === 404) return null;
    if (!res.ok)
      throw Object.assign(new Error(res.statusText), { status: res.status });
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
      const resolver = new IdResolver({ plcUrl, timeout: DID_TIMEOUT_MS });
      resolver.did.methods.set(
        "web",
        new NoRedirectDidWebResolver(DID_TIMEOUT_MS),
      );
      resolver.did.methods.set(
        "plc",
        new NoRedirectDidPlcResolver(plcUrl, DID_TIMEOUT_MS),
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

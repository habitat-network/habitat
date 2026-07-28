import type {
  AtprotoDid,
  AtprotoDidDocument,
  IdentityInfo,
  IdentityResolver,
  ResolveIdentityOptions,
} from "@atproto-labs/identity-resolver";

import { HabitatIdentityResolverError } from "./errors.js";
import { normalizeHandle } from "./handle.js";

/** The Habitat instance operated by the Habitat project. */
export const DEFAULT_HABITAT_SERVICE_URL = "https://pear.habitat.network";

const RESOLVE_IDENTITY_PATH = "/xrpc/com.atproto.identity.resolveIdentity";

/** The DID methods the `AtprotoDid` type admits. */
const ATPROTO_DID_REGEX = /^did:(plc|web):/;

/**
 * Resolves AT Protocol identities through a Habitat instance's
 * `com.atproto.identity.resolveIdentity` endpoint.
 *
 * Unlike `AtprotoIdentityResolver`, this performs no client-side bidirectional
 * handle/DID verification — it delegates that to the Habitat instance, which
 * resolves through indigo's directory and falls back to the public network for
 * identities it does not host. Pointing `serviceUrl` at a host you do not trust
 * means trusting that host's identity claims.
 */
export class HabitatIdentityResolver implements IdentityResolver {
  readonly #serviceUrl: URL;

  constructor(serviceUrl: string | URL = DEFAULT_HABITAT_SERVICE_URL) {
    this.#serviceUrl = new URL(serviceUrl);
  }

  async resolve(
    identifier: string,
    options?: ResolveIdentityOptions,
  ): Promise<IdentityInfo> {
    const url = new URL(RESOLVE_IDENTITY_PATH, this.#serviceUrl);
    url.searchParams.set("identifier", identifier);

    const response = await fetch(url, {
      method: "GET",
      headers: { accept: "application/json" },
      signal: options?.signal,
      ...(options?.noCache ? { cache: "no-store" as const } : {}),
    });

    return toIdentityInfo(await response.json());
  }
}

function toIdentityInfo(body: unknown): IdentityInfo {
  const { did, didDoc, handle } = body as Record<string, unknown>;

  if (typeof did !== "string" || !ATPROTO_DID_REGEX.test(did)) {
    throw new HabitatIdentityResolverError(
      `Habitat instance returned an invalid DID: ${JSON.stringify(did)}`,
    );
  }

  return {
    did: did as AtprotoDid,
    didDoc: didDoc as AtprotoDidDocument,
    handle: normalizeHandle(handle as string),
  };
}

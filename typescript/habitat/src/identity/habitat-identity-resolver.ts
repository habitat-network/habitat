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

    let response: Response;
    try {
      response = await fetch(url, {
        method: "GET",
        headers: { accept: "application/json" },
        signal: options?.signal,
        ...(options?.noCache ? { cache: "no-store" as const } : {}),
      });
    } catch (cause) {
      // Cancellation must stay distinguishable from failure.
      if (isAbortError(cause)) throw cause;
      throw new HabitatIdentityResolverError(
        `Could not reach the Habitat instance at ${this.#serviceUrl.origin}`,
        { cause },
      );
    }

    if (!response.ok) {
      throw await toXrpcError(response);
    }

    let body: unknown;
    try {
      body = await response.json();
    } catch (cause) {
      if (isAbortError(cause)) throw cause;
      throw new HabitatIdentityResolverError(
        `Habitat instance at ${this.#serviceUrl.origin} returned a non-JSON identity payload`,
        { cause },
      );
    }

    return toIdentityInfo(body);
  }
}

function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === "AbortError";
}

/** Translates a non-2xx response into an error, tolerating non-XRPC bodies. */
async function toXrpcError(
  response: Response,
): Promise<HabitatIdentityResolverError> {
  let xrpcError: string | undefined;
  let detail: string | undefined;

  try {
    const body = (await response.json()) as {
      error?: unknown;
      message?: unknown;
    };
    if (typeof body?.error === "string") xrpcError = body.error;
    if (typeof body?.message === "string") detail = body.message;
  } catch {
    // A non-JSON error body still yields a useful error via `status`.
  }

  const summary = detail ?? xrpcError ?? response.statusText;

  return new HabitatIdentityResolverError(
    `Habitat identity resolution failed with status ${response.status}${
      summary ? `: ${summary}` : ""
    }`,
    { status: response.status, xrpcError },
  );
}

/**
 * Validates the XRPC payload before asserting it into `IdentityInfo`. This is
 * what makes the type assertions below honest rather than blind.
 */
function toIdentityInfo(body: unknown): IdentityInfo {
  if (typeof body !== "object" || body === null) {
    throw new HabitatIdentityResolverError(
      "Habitat instance returned a non-object identity payload",
    );
  }

  const { did, didDoc, handle } = body as Record<string, unknown>;

  if (typeof did !== "string" || !ATPROTO_DID_REGEX.test(did)) {
    throw new HabitatIdentityResolverError(
      `Habitat instance returned an invalid DID: ${JSON.stringify(did)}`,
    );
  }

  if (typeof didDoc !== "object" || didDoc === null) {
    throw new HabitatIdentityResolverError(
      `Habitat instance returned no DID document for ${did}`,
    );
  }

  const documentId = (didDoc as Record<string, unknown>).id;
  if (documentId !== did) {
    throw new HabitatIdentityResolverError(
      `DID document id ${JSON.stringify(documentId)} does not match resolved DID ${did}`,
    );
  }

  if (typeof handle !== "string") {
    throw new HabitatIdentityResolverError(
      `Habitat instance returned a non-string handle for ${did}`,
    );
  }

  return {
    did: did as AtprotoDid,
    didDoc: didDoc as AtprotoDidDocument,
    handle: normalizeHandle(handle),
  };
}

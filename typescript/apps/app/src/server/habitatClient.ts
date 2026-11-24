import type { OAuthSession } from "@atproto/oauth-client";
import type { LexValue } from "@atproto/lex";

// EmailDomainNotFoundError is thrown by startLogin when a work email can't be
// resolved to an identity, i.e. its domain isn't mapped to any org.
export class EmailDomainNotFoundError extends Error {}

// OAuthAppState rides through the OAuth round-trip in the `state` param and
// tells /oauth/callback which flow just finished: a member signing in, or a
// signed-in member connecting an org (see startOrgConnect in functions.ts).
export type OAuthAppState =
  { kind: "member" } | { kind: "org"; orgDid: string; memberDid: string };

// isDidNotFound reports whether err, or anything in its cause chain, carries
// the XRPC error DidNotFound — what the Habitat identity resolver returns for
// a work email at an unmapped domain (same check as internal's AuthForm). The
// OAuth client wraps resolver failures, so it's usually a cause.
function isDidNotFound(err: unknown): boolean {
  for (let e = err; e instanceof Error; e = e.cause) {
    if ((e as { xrpcError?: unknown }).xrpcError === "DidNotFound") {
      return true;
    }
  }
  return false;
}

// startLogin begins an atproto OAuth flow for loginHint (a handle, DID, or
// work email — the Habitat identity resolver accepts all three, minting an
// identity for a work email at a mapped domain), returning the
// authorization URL the browser should be sent to next. pear redirects back
// to /oauth/callback once it completes.
export async function startLogin(
  env: Env,
  loginHint: string,
  state: OAuthAppState = { kind: "member" },
): Promise<string> {
  const { createOAuthClient, OAUTH_SCOPE } = await import("./oauth");
  try {
    const url = await createOAuthClient(env).authorize(loginHint, {
      scope: OAUTH_SCOPE,
      state: JSON.stringify(state),
    });
    return url.toString();
  } catch (err) {
    if (loginHint.includes("@") && isDidNotFound(err)) {
      throw new EmailDomainNotFoundError(
        err instanceof Error ? err.message : String(err),
        { cause: err },
      );
    }
    throw err;
  }
}

// instanceProxy returns the Atproto-Proxy target that routes a Habitat
// method (network.habitat.*) from the session's own server to this app's
// Habitat instance, or undefined when the session already talks to the
// instance. A member whose account lives on another PDS (such as the spaces
// alpha PDS) signs in there rather than at pear, and their PDS would answer
// those methods with "No service configured" unless told where to proxy them.
export function instanceProxy(
  session: Pick<OAuthSession, "serverMetadata">,
  env: Pick<Env, "APP_HABITAT_URL">,
  nsid: string,
): string | undefined {
  if (!nsid.startsWith("network.habitat.")) return undefined;
  // Without either there is nothing to route by; send the call as is.
  const issuer = session.serverMetadata?.issuer;
  if (!issuer || !env.APP_HABITAT_URL) return undefined;
  const instance = new URL(env.APP_HABITAT_URL);
  if (new URL(issuer).origin === instance.origin) return undefined;
  return `did:web:${instance.host}#habitat`;
}

// HabitatClient is the Promise view of the Habitat service
// (src/server/effect/services.ts) for one DID, for code that isn't written as
// Effects: the doc rooms, the sync sink, the docs and comments helpers. It
// is the same implementation the Effect callers use, so a call fails the
// same way: it rejects with SessionExpired when pear no longer accepts the
// DID's session, and with HabitatError (carrying the HTTP status and XRPC
// error name) for anything else.
export interface HabitatClient {
  // call makes an untyped call by NSID; GET payloads become query
  // parameters. `proxy` is the DID whose `#habitat` service should handle it
  // (Atproto-Proxy).
  call<T>(
    nsid: string,
    method: "GET" | "POST",
    payload: Record<string, unknown>,
    opts?: { proxy?: string },
  ): Promise<T>;
  // uploadBlob uploads raw bytes to the DID's own repo, returning the blob
  // reference to embed in a subsequent putRecord call.
  uploadBlob(
    bytes: Uint8Array,
    mimeType: string,
  ): Promise<{ blob: LexValue; cid: string }>;
  // getBlob reads a blob from a space repo, as the DID.
  getBlob(space: string, repo: string, cid: string): Promise<Uint8Array>;
  // asDid returns a client for another DID's session, for calls only that
  // DID may make (a doc's owner creating its comments space).
  asDid(did: string): HabitatClient;
}

// createHabitatClient builds a HabitatClient for env outside an Effect
// runtime (Durable Objects, the sync sink).
//
// The services module is imported dynamically because DocRoom imports this
// module, and so does the Worker entry the tests load before any test file
// runs: a static import would evaluate ./oauth then, out of reach of the
// vi.mock() the tests swap in a fake OAuth client with (test/fakeOAuth.ts).
export function createHabitatClient(env: Env, did: string): HabitatClient {
  const make = async (did: string) => {
    const [{ makeHabitat, promiseClient }, { createOAuthClient }] =
      await Promise.all([import("./effect/services"), import("./oauth")]);
    return promiseClient(makeHabitat(env, createOAuthClient(env)), did);
  };
  const lazy = (did: string): HabitatClient => {
    let client: Promise<HabitatClient> | undefined;
    const get = () => (client ??= make(did));
    return {
      call: async (...args) => (await get()).call(...args),
      uploadBlob: async (...args) => (await get()).uploadBlob(...args),
      getBlob: async (...args) => (await get()).getBlob(...args),
      asDid: lazy,
    };
  };
  return lazy(did);
}

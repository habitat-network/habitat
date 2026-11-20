import { JoseKey } from "@atproto/jwk-jose";
import {
  OAuthClient,
  type OAuthClientMetadataInput,
  type SessionStore,
  type StateStore,
} from "@atproto/oauth-client";
import { HabitatIdentityResolver } from "@habitat-network/habitat";

// How long to wait on a lock holder before assuming it was cancelled.
const LOCK_WAIT_MS = 15_000;
const lockTails = new Map<string, Promise<void>>();

// boundedLock is a per-name mutex that stops waiting on a predecessor that
// never releases (see the requestLock comment in createOAuthClient).
async function boundedLock<T>(name: string, fn: () => T | PromiseLike<T>) {
  const prev = lockTails.get(name) ?? Promise.resolve();
  let release!: () => void;
  const tail = new Promise<void>((resolve) => (release = resolve));
  lockTails.set(name, tail);
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    await Promise.race([
      prev,
      new Promise<void>((resolve) => {
        timer = setTimeout(resolve, LOCK_WAIT_MS);
      }),
    ]);
    return await fn();
  } finally {
    clearTimeout(timer);
    release();
    if (lockTails.get(name) === tail) lockTails.delete(name);
  }
}

// Shared setup for the Cloudflare Worker apps (brain, chalk, drop, app), each
// of which is a plain atproto OAuth client of the Habitat instance.

// trimTrailingSlashes drops any trailing slashes from url. A loop rather
// than url.replace(/\/+$/, ""), which CodeQL flags as polynomial-time on a
// long run of slashes.
export function trimTrailingSlashes(url: string): string {
  let end = url.length;
  while (end > 0 && url[end - 1] === "/") end--;
  return url.slice(0, end);
}

export const CALLBACK_PATH = "/oauth/callback";
export const CLIENT_METADATA_PATH = "/oauth/client-metadata.json";

// clientMetadata builds the app's atproto OAuth client metadata document,
// served at CLIENT_METADATA_PATH (its URL is the client_id).
export function clientMetadata(options: {
  baseUrl: string;
  name: string;
  scope: string;
}): OAuthClientMetadataInput {
  const origin = trimTrailingSlashes(options.baseUrl);
  return {
    client_id: `${origin}${CLIENT_METADATA_PATH}`,
    client_name: options.name,
    client_uri: origin,
    redirect_uris: [`${origin}${CALLBACK_PATH}`],
    scope: options.scope,
    grant_types: ["authorization_code", "refresh_token"],
    response_types: ["code"],
    // pear treats every atproto client as public (fosite_client.go's
    // IsPublic), so there's no client secret or keyset to manage.
    token_endpoint_auth_method: "none",
    application_type: "web",
    dpop_bound_access_tokens: true,
  };
}

export function createOAuthClient(options: {
  clientMetadata: OAuthClientMetadataInput;
  stateStore: StateStore;
  sessionStore: SessionStore;
  habitatUrl: string;
}): OAuthClient {
  return new OAuthClient({
    responseMode: "query",
    clientMetadata: options.clientMetadata,
    stateStore: options.stateStore,
    sessionStore: options.sessionStore,
    // Resolve handles and DIDs through the Habitat instance, which also
    // knows identities (org did:webs, instance-hosted handles) the public
    // network doesn't — same as the frontend's AuthManager.
    identityResolver: new HabitatIdentityResolver(options.habitatUrl),
    fetch: (input, init) => fetch(input, init),
    runtimeImplementation: {
      // The client's built-in local lock is a module-level promise chain. A
      // worker cancels the continuations of a finished request, so if one
      // is cancelled while holding the lock (say the browser navigates away
      // mid-refresh) the release never runs, and every later call for that
      // session waits forever ("Worker's code had hung"). This lock gives
      // up waiting for a stuck holder after LOCK_WAIT_MS. It still
      // serializes refreshes within an isolate. Refresh tokens rotate (pear
      // deletes the old one once used), so two isolates racing is still
      // possible, and giving a requestLock turns off the client's
      // invalid_grant recovery; a rare lost race costs a re-login.
      requestLock: boundedLock,
      createKey: (algs) => JoseKey.generate(algs),
      getRandomValues: (length) =>
        crypto.getRandomValues(new Uint8Array(length)),
      digest: async (data, { name }) =>
        new Uint8Array(
          await crypto.subtle.digest(`SHA-${name.slice(3)}`, data),
        ),
    },
  });
}

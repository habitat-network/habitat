import { JoseKey } from "@atproto/jwk-jose";
import {
  OAuthClient,
  type OAuthClientMetadataInput,
  type SessionStore,
  type StateStore,
} from "@atproto/oauth-client";
import { HabitatIdentityResolver } from "@habitat-network/habitat";

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
      // No requestLock on purpose. Refreshes rotate the refresh token (pear
      // deletes the old one as soon as it's used), and requests, cron passes
      // and other isolates can't share an in-memory lock, so one can't stop
      // two of them refreshing the same session at once. With no lock
      // given, the client still serializes refreshes within an isolate (its
      // own local lock) and also recovers from a cross-isolate race: on
      // invalid_grant it waits briefly and re-reads the store, keeping the
      // token the other isolate just saved. Passing any requestLock turns that
      // recovery off, and the loser would delete the winner's fresh session.
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

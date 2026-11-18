import { JoseKey } from "@atproto/jwk-jose";
import {
  OAuthClient,
  type OAuthClientMetadataInput,
} from "@atproto/oauth-client";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import { getDb } from "@/db";
import { DrizzleSessionStore, DrizzleStateStore } from "@/db/oauthStores";

// The app is a plain atproto OAuth client of the Habitat instance. Members
// sign in with their own handle, and the resulting session is stored under
// their DID, so `client.restore(did)` is how every call made on a member's
// behalf gets its credentials.

// Space scopes per proposal 0016. Widen or narrow these to what the app needs.
export const OAUTH_SCOPE = [
  "atproto",
  "transition:generic",
  "space:*?authority=*&action=read",
].join(" ");

export const CALLBACK_PATH = "/oauth/callback";
export const CLIENT_METADATA_PATH = "/oauth/client-metadata.json";

export function clientMetadata(baseUrl: string): OAuthClientMetadataInput {
  const origin = baseUrl.replace(/\/+$/, "");
  return {
    client_id: `${origin}${CLIENT_METADATA_PATH}`,
    client_name: "App",
    client_uri: origin,
    redirect_uris: [`${origin}${CALLBACK_PATH}`],
    scope: OAUTH_SCOPE,
    grant_types: ["authorization_code", "refresh_token"],
    response_types: ["code"],
    // pear treats every atproto client as public (fosite_client.go's
    // IsPublic), so there's no client secret or keyset to manage.
    token_endpoint_auth_method: "none",
    application_type: "web",
    dpop_bound_access_tokens: true,
  };
}

export function createOAuthClient(env: Env): OAuthClient {
  const db = getDb(env);
  return new OAuthClient({
    responseMode: "query",
    clientMetadata: clientMetadata(env.APP_BASE_URL),
    stateStore: new DrizzleStateStore(db, env.APP_CREDENTIALS_KEY),
    sessionStore: new DrizzleSessionStore(db, env.APP_CREDENTIALS_KEY),
    // Resolve handles and DIDs through the Habitat instance, which also
    // knows identities (org did:webs, instance-hosted handles) the public
    // network doesn't.
    identityResolver: new HabitatIdentityResolver(env.APP_HABITAT_URL),
    fetch: (input, init) => fetch(input, init),
    runtimeImplementation: {
      // No requestLock on purpose: refreshes rotate the refresh token, and
      // requests run in different isolates, so an in-memory lock can't stop
      // two of them refreshing the same session. Without one, the client
      // recovers from the race itself (see brain's oauth.ts for the long
      // version); passing any requestLock turns that recovery off.
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

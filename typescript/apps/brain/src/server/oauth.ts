import { JoseKey } from "@atproto/jwk-jose";
import {
  OAuthClient,
  type OAuthClientMetadataInput,
} from "@atproto/oauth-client";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import { getDb } from "@/db";
import { DrizzleSessionStore, DrizzleStateStore } from "@/db/oauthStores";

// Brain is a plain atproto OAuth client of the Habitat instance — no sap in
// between (unlike chalk). Members sign in with their own handle, and the
// resulting session is stored under their DID, so `client.restore(did)` is how
// every call made on a member's behalf (including the syncer's delegation
// tokens for their chat spaces) gets its credentials.

// Space scopes per proposal 0016: read every space (members list their orgs,
// search and the syncer read chat spaces), and write to Brain's own space type.
export const OAUTH_SCOPE = [
  "atproto",
  "transition:generic",
  "space:*?authority=*&action=read",
  "space:network.habitat.brain?authority=*&action=create&action=read&action=update&action=delete&manage=create&manage=update&manage=delete",
].join(" ");

export const CALLBACK_PATH = "/oauth/callback";
export const CLIENT_METADATA_PATH = "/oauth/client-metadata.json";

export function clientMetadata(baseUrl: string): OAuthClientMetadataInput {
  const origin = baseUrl.replace(/\/+$/, "");
  return {
    client_id: `${origin}${CLIENT_METADATA_PATH}`,
    client_name: "Brain",
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
    clientMetadata: clientMetadata(env.BRAIN_BASE_URL),
    stateStore: new DrizzleStateStore(db, env.BRAIN_CREDENTIALS_KEY),
    sessionStore: new DrizzleSessionStore(db, env.BRAIN_CREDENTIALS_KEY),
    // Resolve handles and DIDs through the Habitat instance, which also
    // knows identities (org did:webs, instance-hosted handles) the public
    // network doesn't — same as the frontend's AuthManager.
    identityResolver: new HabitatIdentityResolver(env.BRAIN_HABITAT_URL),
    fetch: (input, init) => fetch(input, init),
    runtimeImplementation: {
      // No requestLock on purpose. Refreshes rotate the refresh token (pear
      // deletes the old one as soon as it's used), and requests and cron
      // passes run in different isolates, so an in-memory lock can't
      // stop two of them refreshing the same session at once. With no lock
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

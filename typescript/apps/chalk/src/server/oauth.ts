import { JoseKey } from "@atproto/jwk-jose";
import {
  OAuthClient,
  type OAuthClientMetadataInput,
} from "@atproto/oauth-client";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import { getDb } from "../db";
import { DrizzleSessionStore, DrizzleStateStore } from "../db/oauthStores";

// chalk is a plain atproto OAuth client of the Habitat instance, the same way
// Drop is (typescript/apps/drop/src/server/oauth.ts) — no sap in between.
// Members sign in with their own handle (or work email, see habitatClient.ts's
// startLogin); a member "connects" an org by running the same flow with the
// org's DID as the login hint, which pear routes through its opensocial admin
// approval (oauthserver.HandleOpensocial). Either way the resulting session
// is stored under its subject DID, so `client.restore(did)` is how every call
// made as a member or an org gets its credentials (see HabitatClient).

// Space scopes per proposal 0016: create/manage doc and comments spaces, and
// read the org spaces chalk lists orgs and their names from.
export const OAUTH_SCOPE = [
  "atproto",
  "transition:generic",
  "space:network.habitat.docs?authority=*&action=create&action=read&action=update&action=delete&manage=create&manage=update&manage=delete",
  "space:network.habitat.docs.comments?authority=*&action=create&action=read&action=update&action=delete&manage=create&manage=update&manage=delete",
  "space:community.opensocial.members?authority=*&action=read",
  "space:community.opensocial.about?authority=*&action=read",
].join(" ");

export const CALLBACK_PATH = "/oauth/callback";
export const CLIENT_METADATA_PATH = "/oauth/client-metadata.json";

// trimTrailingSlashes drops any trailing slashes from url. A loop rather
// than url.replace(/\/+$/, ""), which CodeQL flags as polynomial-time on a
// long run of slashes.
export function trimTrailingSlashes(url: string): string {
  let end = url.length;
  while (end > 0 && url[end - 1] === "/") end--;
  return url.slice(0, end);
}

export function clientMetadata(baseUrl: string): OAuthClientMetadataInput {
  const origin = trimTrailingSlashes(baseUrl);
  return {
    client_id: `${origin}${CLIENT_METADATA_PATH}`,
    client_name: "Chalk",
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
    clientMetadata: clientMetadata(env.CHALK_BASE_URL),
    stateStore: new DrizzleStateStore(db, env.CHALK_CREDENTIALS_KEY),
    sessionStore: new DrizzleSessionStore(db, env.CHALK_CREDENTIALS_KEY),
    // Resolve handles and DIDs through the Habitat instance, which also
    // knows identities (org did:webs, instance-hosted handles) the public
    // network doesn't — same as the frontend's AuthManager.
    identityResolver: new HabitatIdentityResolver(env.CHALK_HABITAT_URL),
    fetch: (input, init) => fetch(input, init),
    runtimeImplementation: {
      // No requestLock on purpose. Refreshes rotate the refresh token (pear
      // deletes the old one as soon as it's used), and requests, cron passes
      // and DocRooms run in different isolates, so an in-memory lock can't
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

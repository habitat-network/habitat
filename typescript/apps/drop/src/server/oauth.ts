import { JoseKey } from "@atproto/jwk-jose";
import {
  OAuthClient,
  type OAuthClientMetadataInput,
  type RuntimeLock,
} from "@atproto/oauth-client";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import { getDb } from "@/db";
import { DrizzleSessionStore, DrizzleStateStore } from "@/db/oauthStores";

// Drop is a plain atproto OAuth client of the Habitat instance — no sap in
// between (unlike chalk). Members sign in with their own handle; an org admin
// "connects" an org by running the same flow with the org's DID as the login
// hint, which pear routes through its opensocial admin approval
// (oauthserver.HandleOpensocial). Either way the resulting session is stored
// under its subject DID, so `client.restore(orgDid)` is how every org-scoped
// call below gets its credentials.

// Space scopes per proposal 0016: read every space (members list their orgs,
// the syncer reads file spaces), and write to Drop's own space type.
export const OAUTH_SCOPE = [
  "atproto",
  "transition:generic",
  "space:*?authority=*&action=read",
  "space:network.habitat.drop?authority=*&action=create&action=read&action=update&action=delete&manage=create&manage=update&manage=delete",
].join(" ");

export const CALLBACK_PATH = "/oauth/callback";
export const CLIENT_METADATA_PATH = "/oauth/client-metadata.json";

export function clientMetadata(baseUrl: string): OAuthClientMetadataInput {
  const origin = baseUrl.replace(/\/+$/, "");
  return {
    client_id: `${origin}${CLIENT_METADATA_PATH}`,
    client_name: "Drop",
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

// Token refreshes rotate the refresh token, so two concurrent refreshes of
// the same session would leave one of them holding a revoked token. This
// lock serializes them within an isolate; org sessions are only ever used
// from the SyncHub Durable Object (one isolate), which is what makes this
// sufficient for them.
const locks = new Map<string, Promise<unknown>>();
const requestLock: RuntimeLock = async (name, fn) => {
  const prev = locks.get(name) ?? Promise.resolve();
  const run = prev.catch(() => {}).then(fn);
  locks.set(name, run);
  try {
    return await run;
  } finally {
    if (locks.get(name) === run) locks.delete(name);
  }
};

export function createOAuthClient(env: Env): OAuthClient {
  const db = getDb(env);
  return new OAuthClient({
    responseMode: "query",
    clientMetadata: clientMetadata(env.DROP_BASE_URL),
    stateStore: new DrizzleStateStore(db, env.DROP_CREDENTIALS_KEY),
    sessionStore: new DrizzleSessionStore(db, env.DROP_CREDENTIALS_KEY),
    // Resolve handles and DIDs through the Habitat instance, which also
    // knows identities (org did:webs, instance-hosted handles) the public
    // network doesn't — same as the frontend's AuthManager.
    identityResolver: new HabitatIdentityResolver(env.DROP_HABITAT_URL),
    fetch: (input, init) => fetch(input, init),
    runtimeImplementation: {
      createKey: (algs) => JoseKey.generate(algs),
      getRandomValues: (length) =>
        crypto.getRandomValues(new Uint8Array(length)),
      digest: async (data, { name }) =>
        new Uint8Array(
          await crypto.subtle.digest(`SHA-${name.slice(3)}`, data),
        ),
      requestLock,
    },
  });
}

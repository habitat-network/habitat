import {
  DrizzleSessionStore,
  DrizzleStateStore,
  clientMetadata as buildClientMetadata,
  createOAuthClient as buildOAuthClient,
} from "internal/oauthClient";
import { getDb } from "@/db";

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

export function clientMetadata(baseUrl: string) {
  return buildClientMetadata({
    baseUrl,
    name: "Drop",
    scope: OAUTH_SCOPE,
  });
}

export function createOAuthClient(env: Env) {
  const db = getDb(env);
  return buildOAuthClient({
    clientMetadata: clientMetadata(env.DROP_BASE_URL),
    stateStore: new DrizzleStateStore(db, env.DROP_CREDENTIALS_KEY),
    sessionStore: new DrizzleSessionStore(db, env.DROP_CREDENTIALS_KEY),
    habitatUrl: env.DROP_HABITAT_URL,
  });
}

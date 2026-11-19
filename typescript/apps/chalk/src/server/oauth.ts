import {
  DrizzleSessionStore,
  DrizzleStateStore,
  clientMetadata as buildClientMetadata,
  createOAuthClient as buildOAuthClient,
} from "internal/oauthClient";
import { getDb } from "../db";

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

export function clientMetadata(baseUrl: string) {
  return buildClientMetadata({
    baseUrl,
    name: "Chalk",
    scope: OAUTH_SCOPE,
  });
}

export function createOAuthClient(env: Env) {
  const db = getDb(env);
  return buildOAuthClient({
    clientMetadata: clientMetadata(env.CHALK_BASE_URL),
    stateStore: new DrizzleStateStore(db, env.CHALK_CREDENTIALS_KEY),
    sessionStore: new DrizzleSessionStore(db, env.CHALK_CREDENTIALS_KEY),
    habitatUrl: env.CHALK_HABITAT_URL,
  });
}

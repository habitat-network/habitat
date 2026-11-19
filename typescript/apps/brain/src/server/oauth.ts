import {
  DrizzleSessionStore,
  DrizzleStateStore,
  clientMetadata as buildClientMetadata,
  createOAuthClient as buildOAuthClient,
} from "internal/oauthClient";
import { getDb } from "@/db";

// Brain is a plain atproto OAuth client of the Habitat instance — no sap in
// between (unlike chalk). Members sign in with their own handle, and the
// resulting session is stored under their DID, so `client.restore(did)` is how
// every call made on a member's behalf gets its credentials.

// Space scopes per proposal 0016: read every space (members list their orgs
// and search them).
export const OAUTH_SCOPE = [
  "atproto",
  "transition:generic",
  "space:*?authority=*&action=read",
].join(" ");

export function clientMetadata(baseUrl: string) {
  return buildClientMetadata({
    baseUrl,
    name: "Brain",
    scope: OAUTH_SCOPE,
  });
}

export function createOAuthClient(env: Env) {
  const db = getDb(env);
  return buildOAuthClient({
    clientMetadata: clientMetadata(env.BRAIN_BASE_URL),
    stateStore: new DrizzleStateStore(db, env.BRAIN_CREDENTIALS_KEY),
    sessionStore: new DrizzleSessionStore(db, env.BRAIN_CREDENTIALS_KEY),
    habitatUrl: env.BRAIN_HABITAT_URL,
  });
}

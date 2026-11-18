import {
  DrizzleSessionStore,
  DrizzleStateStore,
  clientMetadata as buildClientMetadata,
  createOAuthClient as buildOAuthClient,
} from "internal/oauthClient";
import { getDb } from "@/db";

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

export function clientMetadata(baseUrl: string) {
  return buildClientMetadata({
    baseUrl,
    name: "App",
    scope: OAUTH_SCOPE,
  });
}

export function createOAuthClient(env: Env) {
  const db = getDb(env);
  return buildOAuthClient({
    clientMetadata: clientMetadata(env.APP_BASE_URL),
    stateStore: new DrizzleStateStore(db, env.APP_CREDENTIALS_KEY),
    sessionStore: new DrizzleSessionStore(db, env.APP_CREDENTIALS_KEY),
    habitatUrl: env.APP_HABITAT_URL,
  });
}

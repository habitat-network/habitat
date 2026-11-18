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

// Space scopes per proposal 0016: read every space (members list their
// groups and read their records; the syncer reads doc and file spaces), and
// create and manage the space types the app writes: docs, their comments,
// and uploaded files.
const MANAGED_SPACE =
  "action=create&action=read&action=update&action=delete&manage=create&manage=update&manage=delete";
export const OAUTH_SCOPE = [
  "atproto",
  "transition:generic",
  "space:*?authority=*&action=read",
  `space:network.habitat.docs?authority=*&${MANAGED_SPACE}`,
  `space:network.habitat.docs.comments?authority=*&${MANAGED_SPACE}`,
  `space:network.habitat.drop?authority=*&${MANAGED_SPACE}`,
].join(" ");

export function clientMetadata(baseUrl: string) {
  return buildClientMetadata({
    baseUrl,
    name: "Habitat",
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

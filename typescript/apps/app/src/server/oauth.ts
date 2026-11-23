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

// Space scopes per proposal 0016: members read every space (they list their
// groups and read their records; the stream and blob reads cover doc and file spaces), and
// the app creates and writes the spaces it manages: docs, their comments, and
// uploaded files. The write grant is for any space type rather than just those
// three, the same grant as the web client's MANAGE_SPACES_SCOPE: the spaces
// alpha PDS refuses sign-in ("Unable to retrieve space declarations") when
// the scope names those space types.
export const OAUTH_SCOPE = [
  "atproto",
  "transition:generic",
  "space:*?authority=*&action=read",
  "space:*?authority=*&collection=*&manage=create&manage=update&manage=delete",
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

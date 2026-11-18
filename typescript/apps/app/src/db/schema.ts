// --- OAuth (src/server/oauth.ts) -------------------------------------------
//
// The app is an atproto OAuth client of the Habitat instance and holds a
// session per signed-in member. `value` is the session's JSON (token set plus
// the DPoP private key), sealed with APP_CREDENTIALS_KEY (internal/oauthClient/seal.ts)
// since it's a bearer credential for that DID.

// oauthStates holds in-flight authorization requests, keyed by the OAuth
// `state` parameter. Rows are short-lived; expired ones are swept whenever a
// new one is written (see DrizzleStateStore).
export { oauthSessions, oauthStates } from "internal/oauthClient/schema";

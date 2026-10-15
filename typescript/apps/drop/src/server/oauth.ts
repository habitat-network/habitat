import { JoseKey } from "@atproto/jwk-jose";
import {
  OAuthClient,
  type OAuthClientMetadataInput,
  type RuntimeLock,
} from "@atproto/oauth-client";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import {
  JWT_BEARER_GRANT_TYPE,
  parseAssertionKey,
  publicJwk,
  requestJwtBearerToken,
  trimTrailingSlashes,
} from "internal/jwtBearer";
import type { Agent } from "@atproto/lex";
import type { OAuthSession, Session } from "@atproto/oauth-client";
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

// DROP_ASSERTION_KEY is an optional wrangler secret: a P-256 private JWK
// (with a `kid`) as JSON. Its public half is published in the client
// metadata's `jwks`, and when pear lists Drop's client_id in its
// --builtin_app allow-list, Drop can sign RFC 7523 JWT bearer assertions
// with it to mint a fresh org session (see refreshOrgSession). Unset, Drop
// behaves as before and a lost org session means reconnecting the org.
type AssertionEnv = Env & { DROP_ASSERTION_KEY?: string };

export function clientMetadata(
  baseUrl: string,
  assertionKey?: string,
): OAuthClientMetadataInput {
  const origin = trimTrailingSlashes(baseUrl);
  return {
    client_id: `${origin}${CLIENT_METADATA_PATH}`,
    client_name: "Drop",
    client_uri: origin,
    redirect_uris: [`${origin}${CALLBACK_PATH}`],
    scope: OAUTH_SCOPE,
    grant_types: [
      "authorization_code",
      "refresh_token",
      ...(assertionKey ? [JWT_BEARER_GRANT_TYPE] : []),
    ],
    response_types: ["code"],
    // pear treats every atproto client as public (fosite_client.go's
    // IsPublic), so there's no client secret to manage; the jwks, when
    // present, is only what pear verifies JWT bearer assertions against.
    token_endpoint_auth_method: "none",
    application_type: "web",
    dpop_bound_access_tokens: true,
    ...(assertionKey
      ? { jwks: { keys: [publicJwk(parseAssertionKey(assertionKey))] } }
      : {}),
  };
}

// Token refreshes rotate the refresh token, so two concurrent refreshes of
// the same session would leave one of them holding a revoked token. This
// lock serializes them within an isolate only. Requests and cron runs in
// different isolates can still race on an org's session; the loser's
// refresh fails and that request (or pass) errors until the session is
// re-read. Acceptable for a demo; a real deployment would want a lock in
// D1 or a single-isolate home for org credentials.
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
    clientMetadata: clientMetadata(
      env.DROP_BASE_URL,
      (env as AssertionEnv).DROP_ASSERTION_KEY,
    ),
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

// In-flight refreshes by org, so concurrent callers that all found the
// session gone share one token request instead of each minting a session.
const refreshes = new Map<string, Promise<void>>();

// refreshOrgSession mints a fresh session for orgDid with the JWT bearer
// grant and stores it where restore() finds it, replacing whatever was
// there. Throws when DROP_ASSERTION_KEY isn't configured or pear refuses
// the grant (e.g. Drop isn't on its allow-list).
export function refreshOrgSession(env: Env, orgDid: string): Promise<void> {
  const inFlight = refreshes.get(orgDid);
  if (inFlight) return inFlight;
  const run = mintOrgSession(env, orgDid).finally(() =>
    refreshes.delete(orgDid),
  );
  refreshes.set(orgDid, run);
  return run;
}

async function mintOrgSession(env: Env, orgDid: string): Promise<void> {
  const assertionKey = (env as AssertionEnv).DROP_ASSERTION_KEY;
  if (!assertionKey) throw new Error("DROP_ASSERTION_KEY is not set");
  const tok = await requestJwtBearerToken({
    habitatUrl: env.DROP_HABITAT_URL,
    clientId: clientMetadata(env.DROP_BASE_URL).client_id!,
    assertionKey: parseAssertionKey(assertionKey),
    subject: orgDid,
    scope: OAUTH_SCOPE,
  });
  if (tok.sub !== orgDid) {
    throw new Error(`jwt bearer grant returned ${tok.sub}, wanted ${orgDid}`);
  }
  const session: Session = {
    dpopKey: await JoseKey.fromJWK(tok.dpopJwk),
    authMethod: { method: "none" },
    tokenSet: {
      iss: tok.issuer,
      sub: tok.sub as Session["tokenSet"]["sub"],
      aud: trimTrailingSlashes(env.DROP_HABITAT_URL),
      scope: tok.scope as Session["tokenSet"]["scope"],
      access_token: tok.accessToken,
      refresh_token: tok.refreshToken,
      token_type: "DPoP",
      expires_at: tok.expiresAt,
    },
  };
  await new DrizzleSessionStore(getDb(env), env.DROP_CREDENTIALS_KEY).set(
    orgDid,
    session,
  );
}

// orgAgent restores an org's OAuth session as an agent that recovers from
// losing it: a session that can't be restored (never stored, revoked, or
// its refresh token expired) is replaced through the JWT bearer grant, and a
// call pear answers with 401 is retried once on a freshly minted session.
// Where the grant isn't available (no DROP_ASSERTION_KEY, or pear refuses
// it) the original failure surfaces unchanged.
export async function orgAgent(env: Env, orgDid: string): Promise<Agent> {
  const restore = () =>
    createOAuthClient(env).restore(orgDid) as Promise<OAuthSession>;
  let session: OAuthSession;
  try {
    session = await restore();
  } catch (err) {
    if (!(await tryRefresh(env, orgDid))) throw err;
    session = await restore();
  }
  return {
    did: session.did,
    fetchHandler: async (path, init) => {
      const res = await session.fetchHandler(path, init);
      if (res.status !== 401 || !(await tryRefresh(env, orgDid))) return res;
      session = await restore();
      return session.fetchHandler(path, init);
    },
  };
}

// tryRefresh reports whether a fresh session is now in place.
async function tryRefresh(env: Env, orgDid: string): Promise<boolean> {
  if (!(env as AssertionEnv).DROP_ASSERTION_KEY) return false;
  try {
    await refreshOrgSession(env, orgDid);
    return true;
  } catch (err) {
    console.error("[drop] jwt bearer refresh for", orgDid, "failed", err);
    return false;
  }
}

import { redirect } from "@tanstack/react-router";
import type { DidString } from "@atproto/syntax";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import type { Agent } from "@atproto/lex";
import type { OAuthSession } from "@atproto/oauth-client";
import { createOAuthClient } from "./oauth";
import { useAppSession } from "./session";
import type { Actor } from "internal";
import { fetchOrgProfiles, listMyOrgIds } from "./habitat";

// Server-only helpers for functions.ts and the server routes, kept apart for
// the same reason as chalk's functions.server.ts: a file mixing
// createServerFn exports with plain functions that call server-only APIs
// can't be imported from client-reachable code.

// requireMember resolves the signed-in member from the session cookie,
// throwing a redirect to /login when there isn't one.
export async function requireMember(): Promise<{
  did: DidString;
  currentOrg?: DidString;
}> {
  const session = await useAppSession();
  if (!session.data.did) throw redirect({ to: "/login" });
  return {
    did: session.data.did as DidString,
    currentOrg: session.data.currentOrg as DidString | undefined,
  };
}

// requireOrg is requireMember for org-scoped pages: it also needs an org
// selected, and sends the member to the org picker otherwise.
export async function requireOrg(): Promise<{
  did: DidString;
  currentOrg: DidString;
}> {
  const { did, currentOrg } = await requireMember();
  if (!currentOrg) throw redirect({ to: "/orgs" });
  return { did, currentOrg };
}

// MemberUnauthorizedError is thrown by a memberSession agent's calls when
// pear answers 401, i.e. it no longer accepts the member's own OAuth
// session even after the OAuth client's own refresh-and-retry. The
// sign-out middleware (src/start.ts) turns it into a sign-out.
export class MemberUnauthorizedError extends Error {}

// memberSession restores the member's own OAuth session. A session that
// can no longer be restored (revoked, or its refresh token expired) means
// signing in again, and so does one pear rejects with a 401 later on: the
// returned agent throws MemberUnauthorizedError for that. Org sessions
// aren't wrapped like this, since an org's session failing doesn't end the
// member's login.
export async function memberSession(env: Env, did: DidString): Promise<Agent> {
  let oauthSession: OAuthSession;
  try {
    oauthSession = await createOAuthClient(env).restore(did);
  } catch (err) {
    console.error("[drop] restoring member session", err);
    return signOut();
  }
  return {
    did: oauthSession.did,
    fetchHandler: async (path, init) => {
      const res = await oauthSession.fetchHandler(path, init);
      if (res.status === 401) {
        throw new MemberUnauthorizedError(
          `${path} failed (401): ${await res.text()}`,
        );
      }
      return res;
    },
  };
}

// signOutIfUnauthorized ends the login when err (or anything in its cause
// chain: xrpc() wraps what a fetch handler throws) is a
// MemberUnauthorizedError. It returns for anything else, which the caller
// rethrows.
export async function signOutIfUnauthorized(err: unknown): Promise<void> {
  for (let e = err; e instanceof Error; e = e.cause) {
    if (e instanceof MemberUnauthorizedError) await signOut();
  }
}

// signOut clears the session cookie and sends the browser to /login.
async function signOut(): Promise<never> {
  const session = await useAppSession();
  await session.clear();
  throw redirect({ to: "/login" });
}

// selectOrg makes orgDid the member's working org, after checking with pear
// that they really are a member of it — the check every org-scoped route
// then relies on (see DropSessionData.currentOrg).
export async function selectOrg(
  env: Env,
  did: DidString,
  orgDid: DidString,
): Promise<void> {
  const orgs = await listMyOrgIds(await memberSession(env, did));
  if (!orgs.includes(orgDid)) {
    throw new Error("You aren't a member of that organization");
  }
  const session = await useAppSession();
  await session.update({ currentOrg: orgDid });
}

// resolveHandle looks up a DID's handle through the Habitat instance, for
// display. Null if it can't be resolved.
export async function resolveHandle(
  env: Env,
  did: string,
): Promise<string | null> {
  try {
    const info = await new HabitatIdentityResolver(
      env.DROP_HABITAT_URL,
    ).resolve(did);
    return info.handle === "handle.invalid" ? null : info.handle;
  } catch {
    return null;
  }
}

// orgProfiles resolves dids to their profiles as members of orgDid, via
// Habitat's getProfiles, indexed by did. Best-effort, like resolveHandle:
// anything it can't resolve (a non-member, or a failed call) is just absent,
// and callers fall back to the bare DID. A 401 still ends the member's login.
export async function orgProfiles(
  env: Env,
  memberDid: DidString,
  orgDid: DidString,
  dids: string[],
): Promise<Map<string, Actor>> {
  if (dids.length === 0) return new Map();
  try {
    const agent = await memberSession(env, memberDid);
    const profiles = await fetchOrgProfiles(agent, orgDid, dids);
    return new Map(profiles.map((p) => [p.did, p]));
  } catch (err) {
    await signOutIfUnauthorized(err);
    console.error("[drop] fetching org profiles", err);
    return new Map();
  }
}

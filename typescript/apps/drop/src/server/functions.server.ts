import { redirect } from "@tanstack/react-router";
import type { DidString } from "@atproto/syntax";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import type { OAuthSession } from "@atproto/oauth-client";
import { createOAuthClient } from "./oauth";
import { useAppSession } from "./session";
import { listMyOrgIds } from "./habitat";

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

// memberSession restores the member's own OAuth session. A session that
// can no longer be restored (revoked, or its refresh token expired) means
// signing in again.
export async function memberSession(
  env: Env,
  did: DidString,
): Promise<OAuthSession> {
  try {
    return await createOAuthClient(env).restore(did);
  } catch (err) {
    console.error("[drop] restoring member session", err);
    const session = await useAppSession();
    await session.clear();
    throw redirect({ to: "/login" });
  }
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

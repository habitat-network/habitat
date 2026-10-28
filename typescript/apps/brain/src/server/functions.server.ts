import { redirect } from "@tanstack/react-router";
import { SpaceRef, type DidString, type SpaceRefString } from "@atproto/syntax";
import { HabitatIdentityResolver } from "@habitat-network/habitat";
import type { Agent } from "@atproto/lex";
import type { OAuthSession } from "@atproto/oauth-client";
import { createOAuthClient } from "./oauth";
import { CHAT_SPACE_TYPE } from "./habitat";
import { useAppSession } from "./session";

// Server-only helpers for functions.ts and the server routes, kept apart for
// the same reason as chalk's functions.server.ts: a file mixing
// createServerFn exports with plain functions that call server-only APIs
// can't be imported from client-reachable code.

// requireMember resolves the signed-in member from the session cookie,
// throwing a redirect to /login when there isn't one.
export async function requireMember(): Promise<{ did: DidString }> {
  const session = await useAppSession();
  if (!session.data.did) throw redirect({ to: "/login" });
  return { did: session.data.did as DidString };
}

// MemberUnauthorizedError is thrown by a memberSession agent's calls when
// pear answers 401, i.e. it no longer accepts the member's own OAuth
// session even after the OAuth client's own refresh-and-retry. The
// sign-out middleware (src/start.ts) turns it into a sign-out.
export class MemberUnauthorizedError extends Error {}

// memberSession restores the member's own OAuth session. A session that
// can no longer be restored (revoked, or its refresh token expired) means
// signing in again, and so does one pear rejects with a 401 later on: the
// returned agent throws MemberUnauthorizedError for that.
export async function memberSession(env: Env, did: DidString): Promise<Agent> {
  let oauthSession: OAuthSession;
  try {
    oauthSession = await createOAuthClient(env).restore(did);
  } catch (err) {
    console.error("[brain] restoring member session", err);
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

// resolveHandle looks up a DID's handle through the Habitat instance, for
// display. Null if it can't be resolved.
export async function resolveHandle(
  env: Env,
  did: string,
): Promise<string | null> {
  try {
    const info = await new HabitatIdentityResolver(
      env.BRAIN_HABITAT_URL,
    ).resolve(did);
    return info.handle === "handle.invalid" ? null : info.handle;
  } catch {
    return null;
  }
}

// ownedChatSpace checks that space is a chat space owned by did, since the
// space URI comes from the browser.
export function ownedChatSpace(space: string, did: string): SpaceRefString {
  const ref = SpaceRef.parse(space);
  if (ref.spaceType !== CHAT_SPACE_TYPE || ref.spaceDid !== did) {
    throw new Error("Chat not found");
  }
  return ref.toString();
}

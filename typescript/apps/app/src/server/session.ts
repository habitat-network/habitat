import { getCookie, setCookie } from "@tanstack/react-start/server";
import { getIronSession } from "iron-session";

export interface AppSessionData {
  // The signed-in member. Their OAuth session lives in D1 (oauth_sessions),
  // keyed by this DID; the cookie only names it.
  did?: string;
  // The groups the member was last seen to belong to, checked with pear
  // when they signed in or first opened the group (see requireOrg in
  // effect/member.ts). Group-scoped reads of the app's own D1 mirror trust
  // this list instead of asking pear on every request; pear still enforces
  // its own access control on every call made to it.
  orgs?: string[];
  // When orgs was last checked with pear (ms since the epoch).
  orgsCheckedAt?: number;
}

const SESSION_MAX_AGE_SECONDS = 60 * 60 * 24 * 30; // ~30 days

export async function useAppSession() {
  const secret = process.env.APP_SESSION_SECRET;
  if (!secret) {
    throw new Error(
      "APP_SESSION_SECRET is not set. Set it in .dev.vars (local) or as a wrangler secret before using App sessions.",
    );
  }
  // Per the TanStack Start auth guide, sessions are encrypted cookies managed
  // by iron-session, wired to Start's getCookie/setCookie helpers.
  const session = await getIronSession<AppSessionData>(
    { read: getCookie, write: setCookie },
    {
      cookieName: "app_session",
      password: secret,
      ttl: SESSION_MAX_AGE_SECONDS,
      cookieOptions: {
        httpOnly: true,
        sameSite: "lax",
        secure: process.env.NODE_ENV === "production",
        path: "/",
      },
    },
  );
  // Keep the small { data, update, clear } surface callers already use.
  return {
    get data(): AppSessionData {
      return {
        did: session.did,
        orgs: session.orgs,
        orgsCheckedAt: session.orgsCheckedAt,
      };
    },
    async update(patch: Partial<AppSessionData>) {
      for (const [key, value] of Object.entries(patch)) {
        // undefined means "unset", as with the old built-in helper.
        (session as Record<string, unknown>)[key] = value;
      }
      await session.save();
    },
    async clear() {
      session.destroy();
    },
  };
}

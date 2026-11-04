import { getCookie, setCookie } from "@tanstack/react-start/server";
import { getIronSession } from "iron-session";

export interface DropSessionData {
  // The signed-in member. Their OAuth session lives in D1 (oauth_sessions),
  // keyed by this DID; the cookie only names it.
  did?: string;
  // The org the member is working in. Only ever set after checking they're
  // a member of it (see selectOrg in functions.server.ts), so every
  // org-scoped route can trust it without re-asking pear.
  currentOrg?: string;
}

const SESSION_MAX_AGE_SECONDS = 60 * 60 * 24 * 30; // ~30 days

export async function useAppSession() {
  const secret = process.env.DROP_SESSION_SECRET;
  if (!secret) {
    throw new Error(
      "DROP_SESSION_SECRET is not set. Set it in .dev.vars (local) or as a wrangler secret before using Drop sessions.",
    );
  }
  // Per the TanStack Start auth guide, sessions are encrypted cookies managed
  // by iron-session, wired to Start's getCookie/setCookie helpers.
  const session = await getIronSession<DropSessionData>(
    { read: getCookie, write: setCookie },
    {
      cookieName: "drop_session",
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
    get data(): DropSessionData {
      return { did: session.did, currentOrg: session.currentOrg };
    },
    async update(patch: Partial<DropSessionData>) {
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

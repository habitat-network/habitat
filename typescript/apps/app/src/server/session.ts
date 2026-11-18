import { getCookie, setCookie } from "@tanstack/react-start/server";
import { getIronSession } from "iron-session";

export interface AppSessionData {
  // The signed-in member. Their OAuth session lives in D1 (oauth_sessions),
  // keyed by this DID; the cookie only names it.
  did?: string;
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
      return { did: session.did };
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

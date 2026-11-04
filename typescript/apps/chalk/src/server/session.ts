import { getCookie, setCookie } from "@tanstack/react-start/server";
import { getIronSession } from "iron-session";

export interface ChalkSessionData {
  did?: string;
  // The org DID the member is currently acting as, if any. Unset means
  // "Personal" mode — see docs/superpowers/specs/2026-09-03-chalk-org-support-design.md.
  currentOrg?: string;
}

const SESSION_MAX_AGE_SECONDS = 60 * 60 * 24 * 30; // ~30 days

// useAppSession wraps an iron-session cookie session (see
// https://tanstack.com/start/latest/docs/framework/react/guide/authentication)
// with chalk's cookie name and secret. The session cookie holds only the
// member DID; their OAuth session (tokens, refresh) lives in D1 (oauth_sessions,
// see src/server/oauth.ts), keyed by that DID.
export async function useAppSession() {
  const secret = process.env.CHALK_SESSION_SECRET;
  if (!secret) {
    throw new Error(
      "CHALK_SESSION_SECRET is not set. Set it in the environment (see dev.env / cmd/keygen) before using chalk sessions.",
    );
  }
  // Per the TanStack Start auth guide, sessions are encrypted cookies managed
  // by iron-session, wired to Start's getCookie/setCookie helpers.
  const session = await getIronSession<ChalkSessionData>(
    { read: getCookie, write: setCookie },
    {
      cookieName: "chalk_session",
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
    get data(): ChalkSessionData {
      return { did: session.did, currentOrg: session.currentOrg };
    },
    async update(patch: Partial<ChalkSessionData>) {
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

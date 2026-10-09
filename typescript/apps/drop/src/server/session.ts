import { useSession } from "@tanstack/react-start/server";

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
  return useSession<DropSessionData>({
    name: "drop_session",
    password: secret,
    maxAge: SESSION_MAX_AGE_SECONDS,
    cookie: {
      httpOnly: true,
      sameSite: "lax",
      secure: process.env.NODE_ENV === "production",
      maxAge: SESSION_MAX_AGE_SECONDS,
    },
  });
}

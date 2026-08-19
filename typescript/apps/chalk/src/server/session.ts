import { useSession } from "@tanstack/react-start/server";

export interface ChalkSessionData {
  did?: string;
}

const SESSION_MAX_AGE_SECONDS = 60 * 60 * 24 * 30; // ~30 days

// useAppSession wraps TanStack Start's built-in session helper (see
// https://tanstack.com/start/latest/docs/framework/react/guide/authentication#2-session-management)
// with chalk's cookie name and secret. The session cookie holds only the
// member DID; everything else (tokens, refresh) lives in sap.
export async function useAppSession() {
  const secret = process.env.CHALK_SESSION_SECRET;
  if (!secret) {
    throw new Error(
      "CHALK_SESSION_SECRET is not set. Set it in the environment (see dev.env / cmd/keygen) before using chalk sessions.",
    );
  }
  return useSession<ChalkSessionData>({
    name: "chalk_session",
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

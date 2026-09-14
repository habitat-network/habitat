import { useSession } from "@tanstack/react-start/server";

export interface AppSessionData {
  did?: string;
}

const SESSION_MAX_AGE_SECONDS = 60 * 60 * 24 * 30;

export async function useAppSession() {
  const secret = process.env.CONVEX_TEST_SESSION_SECRET;
  if (!secret) {
    throw new Error(
      "CONVEX_TEST_SESSION_SECRET is not set. Generate one (e.g. via cmd/keygen or `openssl rand -base64 32`) and set it in the environment before using sessions.",
    );
  }
  return useSession<AppSessionData>({
    name: "convex_test_session",
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

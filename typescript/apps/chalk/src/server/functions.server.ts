import { redirect } from "@tanstack/react-router";
import { useAppSession } from "./session";

// Server-only helpers, kept out of functions.ts so that file can stay
// "pure" (only createServerFn-wrapped exports) per TanStack Start's
// server-functions guide
// (https://tanstack.com/start/latest/docs/framework/react/guide/server-functions):
// a file that mixes createServerFn exports with plain functions calling
// server-only APIs (like useAppSession here) can't be statically imported
// from client-reachable code — the whole module, including those plain
// exports' server-only imports, gets pulled into the client bundle graph,
// which the build's import-protection plugin rejects. This file is only
// ever imported by functions.ts (from inside handler bodies, which the
// framework strips from the client bundle regardless of what they import),
// never directly by a route's client component.

export const DOCS_SPACE_TYPE = "network.habitat.docs";
export const CRDT_COLLECTION = "network.habitat.docs.crdt";

// requireSession resolves the logged-in member's DID from the session
// cookie, throwing a redirect to /login when there isn't one.
export async function requireSession(): Promise<{ did: string }> {
  const session = await useAppSession();
  if (!session.data.did) {
    throw redirect({ to: "/login" });
  }
  return { did: session.data.did };
}

// clearSession drops the member DID from the session cookie, ending the
// login. Session state lives only in the cookie (see session.ts), so
// clearing it is all a sign-out has to do on chalk's side.
export async function clearSession(): Promise<void> {
  const session = await useAppSession();
  await session.clear();
}

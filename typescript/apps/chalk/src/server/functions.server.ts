import { redirect } from "@tanstack/react-router";
import { useAppSession } from "./session";
import type { SapClient } from "./sapClient";

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

// sessionDid resolves the logged-in member's DID without redirecting —
// unlike requireSession, whose `throw redirect(...)` is meant for a
// createServerFn/page-load context the router can turn into an HTTP
// redirect. A WebSocket upgrade handshake (src/routes/ws.$docId.ts) has no
// such thing as a followable redirect, so that route checks this directly
// and returns a plain 401 instead.
export async function sessionDid(): Promise<string | undefined> {
  const session = await useAppSession();
  return session.data.did;
}

// hasDocAccess is the one place chalk actually checks
// network.habitat.relationship before returning a doc's content: DocRoom
// itself has no ACL, so it would otherwise fan out to whoever asks. A
// caller who doesn't already hold at least reader has that same check
// endpoint reject the query itself (it requires reader to call), not
// return { allowed: false } — both outcomes mean "no access" here.
export async function hasDocAccess(
  client: SapClient,
  did: string,
  docId: string,
): Promise<boolean> {
  try {
    const res = await client.call<{ allowed: boolean }>(
      "network.habitat.relationship.checkUserRelation",
      "GET",
      { subject: did, relation: "reader", space: docId },
    );
    return res.allowed;
  } catch {
    return false;
  }
}

// docRole resolves the caller's own role on a doc — "editor" if they hold
// at least writer (also true for the owner/a manager), "viewer" if they
// only hold reader, null otherwise. Used to keep the client-side editor
// read-only for viewers; it is not itself an access check (hasDocAccess/
// the WS route's forbidden response is what actually gates the doc).
async function checkRelation(
  client: SapClient,
  did: string,
  docId: string,
  relation: "writer" | "reader",
): Promise<boolean> {
  try {
    const res = await client.call<{ allowed: boolean }>(
      "network.habitat.relationship.checkUserRelation",
      "GET",
      { subject: did, relation, space: docId },
    );
    return res.allowed;
  } catch {
    return false;
  }
}

export async function docRole(
  client: SapClient,
  did: string,
  docId: string,
): Promise<"editor" | "viewer" | null> {
  if (await checkRelation(client, did, docId, "writer")) return "editor";
  if (await checkRelation(client, did, docId, "reader")) return "viewer";
  return null;
}

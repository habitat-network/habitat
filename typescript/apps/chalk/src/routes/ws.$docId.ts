import { createFileRoute } from "@tanstack/react-router";
import { env } from "cloudflare:workers";
import { hasDocAccess, sessionDid } from "@/server/functions.server";
import { SapClient } from "@/server/sapClient";

// Upgrades a browser's WebSocket request into DocRoom's own Hibernation-API
// socket (src/server/rooms/docRoom.ts's fetch()) — this route only does the
// auth/access check DocRoom itself doesn't, then forwards the upgrade
// request; the 101 Response (carrying the client-side WebSocket) comes back
// from the Durable Object untouched. $docId is the doc's full space URI,
// URL-encoded as a single path segment the same way _requireAuth/$uri.tsx's
// page route already is.
export const Route = createFileRoute("/ws/$docId")({
  server: {
    handlers: {
      GET: async ({ request, params }) => {
        const did = await sessionDid();
        if (!did) return new Response("unauthorized", { status: 401 });

        const allowed = await hasDocAccess(
          new SapClient(env, did),
          did,
          params.docId,
        );
        if (!allowed) return new Response("forbidden", { status: 403 });

        // DocRoom's fetch() has no way to authenticate a caller itself (it
        // has no session cookie to read) — these headers are how it learns
        // who this connection is, now that this route has already verified
        // it. A plain Request's headers are immutable, so forwarding the
        // identity means building a new Request rather than mutating this
        // one.
        const forwarded = new Request(request, {
          headers: new Headers(request.headers),
        });
        forwarded.headers.set("X-Chalk-Doc-Id", params.docId);
        forwarded.headers.set("X-Chalk-Member-Did", did);

        const stub = env.DOC.get(env.DOC.idFromName(params.docId));
        return stub.fetch(forwarded);
      },
    },
  },
});

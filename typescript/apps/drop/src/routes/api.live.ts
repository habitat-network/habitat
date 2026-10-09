import { createFileRoute } from "@tanstack/react-router";
import { env } from "cloudflare:workers";
import { useAppSession } from "@/server/session";
import { syncHub } from "@/server/syncHub";

// A WebSocket that pings "changed" whenever the current org's synced file
// list changes, so open browsers refetch it. SyncHub holds the socket; this
// route only authenticates the member and scopes them to their org.
export const Route = createFileRoute("/api/live")({
  server: {
    handlers: {
      GET: async ({ request }) => {
        const session = await useAppSession();
        const { did, currentOrg } = session.data;
        if (!did || !currentOrg) {
          return new Response("unauthorized", { status: 401 });
        }
        const forwarded = new Request(request, {
          headers: new Headers(request.headers),
        });
        forwarded.headers.set("X-Drop-Org-Did", currentOrg);
        return syncHub(env).fetch(forwarded);
      },
    },
  },
});

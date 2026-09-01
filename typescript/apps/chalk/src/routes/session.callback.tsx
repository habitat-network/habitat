import { env } from "cloudflare:workers";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { z } from "zod";
import { useAppSession } from "@/server/session";
import { SapClient } from "@/server/sapClient";

// beforeLoad runs in both the client and server environments (e.g. on
// client-side navigation), so it can't call useAppSession()/useSession()
// directly — that's a server-only API (@tanstack/react-start/server) and
// the build's import-protection plugin rejects pulling it into the client
// bundle. Wrap the session write in a server function instead; only its
// RPC stub reaches the client.
const setSessionDidFn = createServerFn({ method: "POST" })
  .validator((input: { did: string }) => input)
  .handler(async ({ data }) => {
    const session = await useAppSession();
    await session.update({ did: data.did });
    // Best-effort: confirm sap has fully discovered this member's spaces
    // right away rather than waiting on its periodic re-crawl. A hiccup
    // here shouldn't block sign-in — sap's own periodic recrawlLoop is the
    // fallback if this doesn't get through.
    try {
      await new SapClient(env, data.did).recrawl();
    } catch (err) {
      console.error("[session.callback] recrawl", err);
    }
  });

// sap redirects the browser here (as this route's URL is what chalk told
// sap's /org/add to use as return_to) once the PDS OAuth handshake
// completes, with the resolved member DID as a query param.
export const Route = createFileRoute("/session/callback")({
  validateSearch: z.object({
    did: z.string().optional(),
  }),
  beforeLoad: async ({ search }) => {
    if (!search.did) {
      throw redirect({ to: "/login" });
    }
    await setSessionDidFn({ data: { did: search.did } });
    throw redirect({ to: "/" });
  },
});

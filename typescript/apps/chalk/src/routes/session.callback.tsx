import { env } from "cloudflare:workers";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { z } from "zod";
import { useAppSession } from "@/server/session";
import { consumeLoginNonce } from "@/server/functions.server";
import { SapClient, redeemLogin } from "@/server/sapClient";

// beforeLoad runs in both the client and server environments (e.g. on
// client-side navigation), so it can't call useAppSession()/useSession()
// directly — that's a server-only API (@tanstack/react-start/server) and
// the build's import-protection plugin rejects pulling it into the client
// bundle. Wrap the session write in a server function instead; only its
// RPC stub reaches the client.
//
// This server function is reachable by anyone, so it takes nothing it
// trusts from its caller: the DID comes from sap (redeeming the one-time
// code sap put on the callback URL), and the login must have been started
// from this same browser (consumeLoginNonce).
const completeLoginFn = createServerFn({ method: "POST" })
  .validator((input: { code: string }) => input)
  .handler(async ({ data }) => {
    const { did, state } = await redeemLogin(env, data.code);
    await consumeLoginNonce(state);
    const session = await useAppSession();
    // A fresh login starts in Personal mode: a currentOrg left over from
    // whoever was signed in before must not carry over to this member.
    await session.update({ did, currentOrg: undefined });
    // Best-effort: confirm sap has fully discovered this member's spaces
    // right away rather than waiting on its periodic re-crawl. A hiccup
    // here shouldn't block sign-in — sap's own periodic recrawlLoop is the
    // fallback if this doesn't get through.
    try {
      await new SapClient(env, did).recrawl();
    } catch (err) {
      console.error("[session.callback] recrawl", err);
    }
  });

// sap redirects the browser here (as this route's URL is what chalk told
// sap's /session/add to use as return_to) once the PDS OAuth handshake
// completes, with a one-time code to redeem for the member DID.
export const Route = createFileRoute("/session/callback")({
  validateSearch: z.object({
    code: z.string().optional(),
  }),
  beforeLoad: async ({ search }) => {
    if (!search.code) {
      throw redirect({ to: "/login" });
    }
    await completeLoginFn({ data: { code: search.code } });
    throw redirect({ to: "/" });
  },
});

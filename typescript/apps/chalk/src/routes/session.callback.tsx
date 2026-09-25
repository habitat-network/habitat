import { env } from "cloudflare:workers";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { z } from "zod";
import { completeLogin } from "@/server/functions.server";

// beforeLoad runs in both the client and server environments (e.g. on
// client-side navigation), so it can't call useAppSession()/useSession()
// directly — that's a server-only API (@tanstack/react-start/server) and
// the build's import-protection plugin rejects pulling it into the client
// bundle. Wrap the session write in a server function instead; only its
// RPC stub reaches the client.
//
// It takes sap's single-use code, not a DID: this server function is
// callable directly by anyone, so the DID written to the session must come
// from sap (see completeLogin), never from the caller.
const completeLoginFn = createServerFn({ method: "POST" })
  .validator((input: { code: string }) => input)
  .handler(async ({ data }) => {
    await completeLogin(env, data.code);
  });

// sap redirects the browser here (as this route's URL is what chalk told
// sap's /session/add to use as return_to) once the PDS OAuth handshake
// completes, with a single-use code redeemable for the member DID.
export const Route = createFileRoute("/session/callback")({
  validateSearch: z.object({
    code: z.string().optional(),
  }),
  beforeLoad: async ({ search }) => {
    if (!search.code) {
      throw redirect({ to: "/login" });
    }
    try {
      await completeLoginFn({ data: { code: search.code } });
    } catch (err) {
      // Forged, expired, or already-redeemed code: no session was written,
      // so send the member back to sign in again.
      console.error("[session.callback]", err);
      throw redirect({ to: "/login" });
    }
    throw redirect({ to: "/" });
  },
});

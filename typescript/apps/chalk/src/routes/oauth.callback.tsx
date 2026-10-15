import { createFileRoute, redirect } from "@tanstack/react-router";
import { z } from "zod";
import { completeOAuth } from "@/server/functions";

// pear redirects here at the end of both OAuth flows (member sign-in and org
// connect) with the usual code/state/iss query. completeOAuth does the token
// exchange server-side — it's a server function rather than this route's own
// server handler so its session-cookie write rides on the server function
// response, the same way Drop's oauth.callback does.
export const Route = createFileRoute("/oauth/callback")({
  validateSearch: z.record(z.string(), z.string()),
  beforeLoad: async ({ search }) => {
    if (search.error) throw redirect({ to: "/login" });
    let next: Awaited<ReturnType<typeof completeOAuth>>;
    try {
      next = await completeOAuth({ data: { params: search } });
    } catch (err) {
      console.error("[oauth.callback]", err);
      throw redirect({ to: "/login" });
    }
    if (next.to === "/session/org-callback") {
      throw redirect({
        to: "/session/org-callback",
        search: { did: next.orgDid },
      });
    }
    throw redirect({ to: next.to });
  },
});

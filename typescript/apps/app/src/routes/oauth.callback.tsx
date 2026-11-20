import { createFileRoute, redirect } from "@tanstack/react-router";
import { z } from "zod";
import { completeOAuth } from "@/server/fns/auth";

// pear redirects here at the end of the OAuth flow with the usual
// code/state/iss query, for a member signing in and for an admin connecting
// a group alike. completeOAuth does the token exchange server-side — it's a
// server function rather than this route's own server handler so its
// session-cookie write rides on the server function response, the same way
// chalk's session.callback does — and says where to go next.
export const Route = createFileRoute("/oauth/callback")({
  validateSearch: z.record(z.string(), z.string()),
  beforeLoad: async ({ search }) => {
    if (search.error) {
      throw redirect({
        to: "/login",
        search: { error: search.error_description ?? search.error },
      });
    }
    let to: string;
    try {
      ({ to } = await completeOAuth({ data: { params: search } }));
    } catch (err) {
      throw redirect({
        to: "/login",
        search: {
          error: err instanceof Error ? err.message : "Sign-in failed",
        },
      });
    }
    throw redirect({ href: to });
  },
});

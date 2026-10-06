import { createFileRoute, Outlet } from "@tanstack/react-router";
import { ensureValidDid } from "@atproto/syntax";

// Everything under /orgs/$org is scoped to one organization; the param is
// validated once here for all of its pages.
export const Route = createFileRoute("/_requireAuth/orgs/$org")({
  params: {
    parse: ({ org }) => {
      ensureValidDid(org);
      return { org };
    },
  },
  component: Outlet,
});

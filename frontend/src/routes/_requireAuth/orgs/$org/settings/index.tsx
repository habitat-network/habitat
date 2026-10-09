import { createFileRoute, redirect } from "@tanstack/react-router";

// The bare settings URL has no page of its own; it redirects straight to the
// members page, the sidebar's first real destination.
export const Route = createFileRoute("/_requireAuth/orgs/$org/settings/")({
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/orgs/$org/settings/members",
      params: { org: params.org },
    });
  },
});

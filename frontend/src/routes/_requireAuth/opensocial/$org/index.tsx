import { createFileRoute, redirect } from "@tanstack/react-router";

// The bare org URL has no page of its own; it redirects straight to the
// members tab, the sidebar's first real destination.
export const Route = createFileRoute("/_requireAuth/opensocial/$org/")({
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/opensocial/$org/members",
      params: { org: params.org },
    });
  },
});

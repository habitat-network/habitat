import { createFileRoute, redirect } from "@tanstack/react-router";

// An org has no page of its own beyond its settings (search lives on the
// home page, scoped to the selected org), so the bare org URL goes there.
export const Route = createFileRoute("/_requireAuth/orgs/$org/")({
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/orgs/$org/settings",
      params: { org: params.org },
    });
  },
});

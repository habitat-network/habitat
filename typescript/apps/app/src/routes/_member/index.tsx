import { createFileRoute, redirect } from "@tanstack/react-router";
import { myGroupsQuery } from "@/lib/queries/groups";

// The home page is the member's first group, or joining one if they have
// none yet.
export const Route = createFileRoute("/_member/")({
  beforeLoad: async ({ context }) => {
    const groups = await context.queryClient.ensureQueryData(myGroupsQuery);
    const first = groups[0];
    if (!first) throw redirect({ to: "/join" });
    throw redirect({ to: "/g/$org/files", params: { org: first.did } });
  },
});

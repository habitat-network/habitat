import { createFileRoute } from "@tanstack/react-router";
import { myOrgsQueryOptions } from "@/queries/opensocial";
import { getLastOrg } from "@/lib/selectedOrg";

// The index page only routes: to the org the user last visited if they still
// belong to it, else their first org, else the orgs page so they can create
// or join one.
export const Route = createFileRoute("/_requireAuth/")({
  async loader({ context }) {
    const { authManager, queryClient } = context;
    const orgs = await queryClient.ensureQueryData(
      myOrgsQueryOptions(authManager),
    );
    const last = getLastOrg();
    const org = orgs.find((o) => o.did === last)?.did ?? orgs[0]?.did;
    if (!org) throw Route.redirect({ to: "/orgs" });
    throw Route.redirect({ to: "/orgs/$org", params: { org } });
  },
});

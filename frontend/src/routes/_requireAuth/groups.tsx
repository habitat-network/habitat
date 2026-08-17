import { createFileRoute, Outlet } from "@tanstack/react-router";
import { getConfigQueryOptions } from "@/queries/org";
import { requestCrawl } from "@/queries/groups";

// Gates every /groups/* route behind network.habitat.org.requestCrawl:
// the home server doesn't crawl any org on its own startup, so it only
// becomes aware of (and authenticates against) an org once a member of that
// org loads this route. Members of the "everyone" org (no real org) have
// nothing for home to crawl, so they skip straight through.
export const Route = createFileRoute("/_requireAuth/groups")({
  async beforeLoad({ context }) {
    const { authManager, queryClient } = context;
    const meta = await queryClient.ensureQueryData(
      getConfigQueryOptions(authManager),
    );
    if (!meta.orgId) return;
    const result = await requestCrawl(authManager, meta.orgId);
    if (result.status === "authError") {
      throw new Error(
        `home server failed to authenticate org: ${result.message}`,
      );
    }
  },
  component: () => <Outlet />,
});

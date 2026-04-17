import type { AuthManager } from "internal";
import { getConfigQueryOptions } from "internal";
import Header from "@/components/header";
import { type QueryClient } from "@tanstack/react-query";
import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import { TanStackRouterDevtools } from "@tanstack/react-router-devtools";
import { AtpAgent } from "@atproto/api";

interface RouterContext {
  queryClient: QueryClient;
  authManager: AuthManager;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  async beforeLoad({ context }) {
    await context.authManager.maybeExchangeCode();
  },
  async loader({ context }) {
    const config = await context.queryClient.fetchQuery(
      getConfigQueryOptions(__HABITAT_DOMAIN__),
    );

    const authInfo = context.authManager.getAuthInfo();
    if (!authInfo) {
      return { profile: undefined, orgDomain: config.orgDomain };
    }
    const actor = authInfo.did;

    const agent = new AtpAgent({ service: "https://public.api.bsky.app" });
    const response = await agent.getProfile({ actor: actor });

    const profile = response.data;
    return { profile, orgDomain: config.orgDomain };
  },
  staleTime: 1000 * 60 * 60,
  component() {
    const { authManager } = Route.useRouteContext();
    const { profile, orgDomain } = Route.useLoaderData();
    return (
      <div className="flex flex-col items-center w-full justify-stretch gap-4">
        <Header profile={profile} orgDomain={orgDomain ?? undefined} onLogout={authManager.logout} />
        <div className="container px-4 flex flex-col">
          <Outlet />
        </div>
        <TanStackRouterDevtools />
      </div>
    );
  },
});

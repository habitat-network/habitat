import type { AuthManager } from "internal";
import { getConfigQueryOptions } from "@/queries/org";
import Header from "@/components/header";
import { type QueryClient } from "@tanstack/react-query";
import { AtpAgent } from "@atproto/api";
import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import { TanStackRouterDevtools } from "@tanstack/react-router-devtools";
import { Toaster } from "sonner";

interface RouterContext {
  queryClient: QueryClient;
  authManager: AuthManager;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  async beforeLoad({ context }) {
    await context.authManager.maybeExchangeCode();
  },
  async loader({ context }) {
    const authInfo = context.authManager.getAuthInfo();
    if (!authInfo) {
      return { profile: undefined, org: undefined };
    }

    const [config, profileResult] = await Promise.allSettled([
      context.queryClient.fetchQuery(getConfigQueryOptions(context.authManager)),
      new AtpAgent({ service: "https://public.api.bsky.app" }).getProfile({
        actor: authInfo.did,
      }),
    ]);

    const profile =
      profileResult.status === "fulfilled"
        ? profileResult.value.data
        : { did: authInfo.did };

    return {
      profile,
      org: config.status === "fulfilled" ? config.value : undefined,
    };
  },
  staleTime: 1000 * 60 * 60,
  component() {
    const { authManager } = Route.useRouteContext();
    const { profile, org } = Route.useLoaderData();
    return (
      <div className="flex flex-col items-center w-full justify-stretch gap-4">
        {<Header profile={profile} org={org} onLogout={authManager.logout} />}
        <div className="container px-4 flex flex-col">
          <Outlet />
        </div>
        <TanStackRouterDevtools />
        <Toaster />
      </div>
    );
  },
});

import type { AuthManager } from "internal";
import Header from "@/components/header";
import { type QueryClient } from "@tanstack/react-query";
import { AtpAgent } from "@atproto/api";
import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import { TanStackRouterDevtools } from "@tanstack/react-router-devtools";
import { Toaster } from "internal/components/ui";

interface RouterContext {
  queryClient: QueryClient;
  authManager: AuthManager;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  async beforeLoad({ context }) {
    await context.authManager.init();
  },
  async loader({ context }) {
    const authInfo = context.authManager.getAuthInfo();
    if (!authInfo) {
      return { profile: undefined };
    }

    const profileResult = await new AtpAgent({
      service: "https://public.api.bsky.app",
    })
      .getProfile({ actor: authInfo.did })
      .then(
        (r) => r.data,
        () => ({ did: authInfo.did }),
      );

    return { profile: profileResult };
  },
  staleTime: 1000 * 60 * 60,
  component() {
    const { authManager } = Route.useRouteContext();
    const { profile } = Route.useLoaderData();
    return (
      <div className="flex flex-col items-center w-full justify-stretch gap-4">
        {
          <Header
            profile={profile}
            authManager={authManager}
            onLogout={() => authManager.logout()}
          />
        }
        <div className="container px-4 flex flex-col">
          <Outlet />
        </div>
        <TanStackRouterDevtools />
        <Toaster />
      </div>
    );
  },
});

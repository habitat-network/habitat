import type { AuthManager } from "internal/authManager.ts";
import Header from "@/components/header";
import { type QueryClient } from "@tanstack/react-query";
import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import { TanStackRouterDevtools } from "@tanstack/react-router-devtools";

interface RouterContext {
  queryClient: QueryClient;
  authManager: AuthManager;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  async loader({ context }) {
    const authInfo = context.authManager.getAuthInfo();
    if (!authInfo) {
      return { handle: null };
    }
    const actor = authInfo.did;
    const resp = await context.authManager.fetch(
      "/xrpc/app.bsky.actor.getProfile?actor=" + actor,
    );
    const data: { handle: string } | undefined = await resp?.json();
    return { handle: data?.handle };
  },
  staleTime: 1000 * 60 * 60,
  component() {
    const { authManager } = Route.useRouteContext();
    const { handle } = Route.useLoaderData();
    return (
      <>
        <Header handle={handle} onLogout={authManager.logout} />
        <Outlet />
        <TanStackRouterDevtools />
      </>
    );
  },
});

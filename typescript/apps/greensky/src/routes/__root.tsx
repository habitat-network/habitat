import type { AuthManager } from "internal/authManager.ts";
import { type QueryClient } from "@tanstack/react-query";
import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import { TanStackRouterDevtools } from "@tanstack/react-router-devtools";

interface RouterContext {
  queryClient: QueryClient;
  authManager: AuthManager;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  staleTime: 1000 * 60 * 60,
  component() {
    return (
      <>
        <Outlet />
        <TanStackRouterDevtools />
      </>
    );
  },
});

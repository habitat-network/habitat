import type { AuthManager } from "internal/authManager";
import type { HabitatClient } from "internal/habitatClient";
import Header from "@/components/header";
import { type QueryClient } from "@tanstack/react-query";
import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import { TanStackRouterDevtools } from "@tanstack/react-router-devtools";

export interface RouterContext {
  queryClient: QueryClient;
  authManager: AuthManager;
  habitatClient?: HabitatClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  staleTime: 1000 * 60 * 60,
  component() {
    const { authManager } = Route.useRouteContext();
    return (
      <>
        <Header authManager={authManager} />
        <Outlet />
        <TanStackRouterDevtools />
      </>
    );
  },
});

import type { AuthManager } from "internal";
import { type QueryClient } from "@tanstack/react-query";
import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";

interface RouterContext {
  queryClient: QueryClient;
  authManager: AuthManager;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component() {
    return <Outlet />;
  },
});

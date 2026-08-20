import { createRouter as createTanStackRouter } from "@tanstack/react-router";
import { setupRouterSsrQueryIntegration } from "@tanstack/react-router-ssr-query";
import { getContext } from "./integrations/tanstack-query/root-provider";
import { routeTree } from "./routeTree.gen";

export function createRouter() {
  const context = getContext();
  const router = createTanStackRouter({
    routeTree,
    context,
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0,
    scrollRestoration: true,
  });

  // Wires the router's pending/loader state up to the QueryClient so
  // prefetched queries (e.g. _requireAuth's `docs` prefetchQuery) hydrate
  // from the server render instead of refetching on the client.
  setupRouterSsrQueryIntegration({ router, queryClient: context.queryClient });

  return router;
}

// TanStack Start's default client/server entries import `getRouter` from
// this file (see @tanstack/start-client-core's hydrateStart) — this is the
// framework-required export name, distinct from the `createRouter` factory
// itself.
export { createRouter as getRouter };

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createRouter>;
  }
}

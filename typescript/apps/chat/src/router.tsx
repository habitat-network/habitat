import { createRouter as createTanStackRouter } from "@tanstack/react-router";
import { QueryClient } from "@tanstack/react-query";
import { routeTree } from "./routeTree.gen";

export function createRouter() {
  const queryClient = new QueryClient();
  return createTanStackRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: "intent",
    scrollRestoration: true,
  });
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

import {
  Link,
  createRouter as createTanStackRouter,
} from "@tanstack/react-router";
import { routeTree } from "./routeTree.gen";

import { setupRouterSsrQueryIntegration } from "@tanstack/react-router-ssr-query";

import { getContext } from "./integrations/tanstack-query/root-provider";

export function getRouter() {
  const context = getContext();

  const router = createTanStackRouter({
    routeTree,
    context,
    scrollRestoration: true,
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0,
    defaultNotFoundComponent: () => (
      <div className="mx-auto max-w-sm p-8 text-center">
        <h1 className="text-lg font-semibold">Page not found</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          That page doesn't exist.
        </p>
        <Link to="/" className="mt-4 inline-block text-sm underline">
          Go home
        </Link>
      </div>
    ),
  });

  setupRouterSsrQueryIntegration({ router, queryClient: context.queryClient });

  return router;
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof getRouter>;
  }
}

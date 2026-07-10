import { type QueryClient } from "@tanstack/react-query";
import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import { TanStackRouterDevtools } from "@tanstack/react-router-devtools";
import type { DocsServerFetcher } from "@/docsServerFetcher";

interface RouterContext {
  queryClient: QueryClient;
  fetcher: DocsServerFetcher;
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

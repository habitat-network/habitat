import type { QueryClient } from "@tanstack/react-query";
import {
  Outlet,
  createRootRouteWithContext,
  HeadContent,
  Scripts,
} from "@tanstack/react-router";
import { TanStackRouterDevtools } from "@tanstack/react-router-devtools";

interface RouterContext {
  queryClient: QueryClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: "Chalk" },
    ],
  }),
  component() {
    return (
      <html lang="en">
        <head>
          <HeadContent />
        </head>
        <body>
          <Outlet />
          <TanStackRouterDevtools />
          <Scripts />
        </body>
      </html>
    );
  },
});

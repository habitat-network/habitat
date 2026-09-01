import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  createRootRouteWithContext,
  HeadContent,
  Scripts,
} from "@tanstack/react-router";
import { TanStackDevtools } from "@tanstack/react-devtools";
import { Toaster } from "internal/components/ui";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";
import "../index.css";

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
    const { queryClient } = Route.useRouteContext();
    return (
      <html lang="en">
        <head>
          <HeadContent />
        </head>
        <body>
          <QueryClientProvider client={queryClient}>
            <Outlet />
          </QueryClientProvider>
          <Toaster />
          <TanStackDevtools
            plugins={[
              {
                name: "TanStack Router",
                render: <TanStackRouterDevtoolsPanel />,
              },
            ]}
          />
          <Scripts />
        </body>
      </html>
    );
  },
});

import type { QueryClient } from "@tanstack/react-query";
import { QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  createRootRouteWithContext,
  HeadContent,
  Scripts,
} from "@tanstack/react-router";
import { Toaster } from "internal/components/ui";
import PostHogProvider from "../integrations/posthog/provider";
import "../index.css";

interface RouterContext {
  queryClient: QueryClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: "Brain" },
    ],
  }),
  component() {
    const { queryClient } = Route.useRouteContext();
    return (
      <html lang="en">
        <head>
          <HeadContent />
        </head>
        <body className="bg-background text-foreground antialiased">
          <PostHogProvider>
            <QueryClientProvider client={queryClient}>
              <Outlet />
            </QueryClientProvider>
          </PostHogProvider>
          <Toaster />
          <Scripts />
        </body>
      </html>
    );
  },
});

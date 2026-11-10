import type { QueryClient } from "@tanstack/react-query";
import { QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  createRootRouteWithContext,
  HeadContent,
  Scripts,
} from "@tanstack/react-router";
import { TanStackDevtools } from "@tanstack/react-devtools";
import { aiDevtoolsPlugin } from "@tanstack/react-ai-devtools";
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
          {import.meta.env.DEV && (
            <TanStackDevtools
              plugins={[aiDevtoolsPlugin()]}
              eventBusConfig={{ connectToServerBus: true }}
            />
          )}
          <Scripts />
        </body>
      </html>
    );
  },
});

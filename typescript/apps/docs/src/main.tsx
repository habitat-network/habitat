import { StrictMode } from "react";
import ReactDOM from "react-dom/client";
import {
  RouterProvider,
  createHashHistory,
  createRouter,
} from "@tanstack/react-router";
// Import the generated route tree
import { routeTree } from "./routeTree.gen";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { reportWebVitals, AuthManager } from "internal";
import posthog from "posthog-js";
import { PostHogProvider } from "@posthog/react";

const authManager = new AuthManager(
  "Habitat Docs",
  __DOMAIN__,
  __HABITAT_DOMAIN__,
  () => {
    router.navigate({ to: "/login" });
  },
);
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 60 * 60,
    },
  },
});

const domainUrl = new URL(`https://${__DOMAIN__}`);

// Create a new router instance
const router = createRouter({
  routeTree,
  context: {
    queryClient,
    authManager,
  },
  defaultPreload: "intent",
  scrollRestoration: true,
  defaultStructuralSharing: true,
  defaultPreloadStaleTime: 0,
  basepath: __HASH_ROUTING__ ? undefined : domainUrl.pathname,
  history: __HASH_ROUTING__ ? createHashHistory() : undefined,
});

// Register the router instance for type safety
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

console.log(
  import.meta.env.VITE_PUBLIC_POSTHOG_TOKEN,
  import.meta.env.VITE_PUBLIC_POSTHOG_HOST,
);

posthog.init(import.meta.env.VITE_PUBLIC_POSTHOG_TOKEN, {
  api_host: import.meta.env.VITE_PUBLIC_POSTHOG_HOST,
  defaults: "2026-01-30",
});

// Render the app
const rootElement = document.getElementById("app");
if (rootElement && !rootElement.innerHTML) {
  const root = ReactDOM.createRoot(rootElement);
  root.render(
    <StrictMode>
      <PostHogProvider client={posthog}>
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>
      </PostHogProvider>
    </StrictMode>,
  );
}

// If you want to start measuring performance in your app, pass a function
// to log results (for example: reportWebVitals(console.log))
// or send to an analytics endpoint. Learn more: https://bit.ly/CRA-vitals
reportWebVitals();

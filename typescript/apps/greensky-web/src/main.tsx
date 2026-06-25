import { StrictMode } from "react";
import ReactDOM from "react-dom/client";
import {
  RouterProvider,
  createHashHistory,
  createRouter,
} from "@tanstack/react-router";
import { routeTree } from "./routeTree.gen";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { reportWebVitals, AuthManager } from "internal";

const authManager = new AuthManager(
  "Greensky",
  __DOMAIN__,
  __HABITAT_DOMAIN__,
  () => {
    router.navigate({ to: "/login" });
  },
);

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 30,
    },
  },
});

const domainUrl = new URL(`https://${__DOMAIN__}`);

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

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

const rootElement = document.getElementById("app");
if (rootElement && !rootElement.innerHTML) {
  const root = ReactDOM.createRoot(rootElement);
  root.render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </StrictMode>,
  );
}

reportWebVitals();

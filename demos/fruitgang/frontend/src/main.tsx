import { StrictMode } from "react";
import ReactDOM from "react-dom/client";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthManager } from "internal";
import { routeTree } from "./routeTree.gen";
import "./theme.css";

const authManager = new AuthManager(
  "Fruit Gang",
  __DOMAIN__,
  __HABITAT_DOMAIN__,
  () => { router.navigate({ to: "/login" }); }
);

const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 1000 * 30 } },
});

const router = createRouter({
  routeTree,
  context: { queryClient, authManager },
  defaultPreload: "intent",
  defaultPreloadStaleTime: 0,
});

declare module "@tanstack/react-router" {
  interface Register { router: typeof router; }
}

const root = document.getElementById("app");
if (root && !root.innerHTML) {
  ReactDOM.createRoot(root).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </StrictMode>
  );
}

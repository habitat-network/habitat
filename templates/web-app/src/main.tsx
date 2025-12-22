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
import reportWebVitals from "internal/reportWebVitals.ts";
import { AuthManager } from "internal/authManager.ts";

const authManager = new AuthManager(__DOMAIN__, __HABITAT_DOMAIN__, () => {
  router.navigate({ to: "/login" });
});
const queryClient = new QueryClient();

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

// Render the app
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

// If you want to start measuring performance in your app, pass a function
// to log results (for example: reportWebVitals(console.log))
// or send to an analytics endpoint. Learn more: https://bit.ly/CRA-vitals
reportWebVitals();

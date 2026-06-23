import { createRouter as createTanstackRouter } from "@tanstack/react-router";
import { routeTree } from "./routeTree.gen";

// pear mounts this app under `/ui/`, so the router uses `/ui` as its basepath.
// All pages are pre-rendered to static HTML and hydrate on the client; data is
// fetched at runtime from pear over same-origin XRPC calls.
export function createRouter() {
  return createTanstackRouter({
    routeTree,
    basepath: "/ui",
    defaultPreload: "intent",
    scrollRestoration: true,
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createRouter>;
  }
}

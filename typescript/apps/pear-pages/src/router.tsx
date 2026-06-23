import { createRouter as createTanstackRouter } from "@tanstack/react-router";
import { routeTree } from "./routeTree.gen";

// pear mounts this app under `/ui/`, so the router uses `/ui` as its basepath.
// All pages are pre-rendered to static HTML and hydrate on the client; data is
// fetched at runtime from pear over same-origin XRPC calls.
//
// TanStack Start's plugin discovers this entry by the `getRouter` export name
// specifically (see @tanstack/start-plugin-core's router-entry resolution) -
// it must be named exactly that, not just any router factory name.
export function getRouter() {
  return createTanstackRouter({
    routeTree,
    basepath: "/ui",
    defaultPreload: "intent",
    scrollRestoration: true,
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof getRouter>;
  }
}

import { createRouter } from "@tanstack/react-router";
import { routeTree } from "./routeTree.gen";

// pear mounts this app under `/ui/`, so the router uses `/ui` as its basepath.
export const router = createRouter({
  routeTree,
  basepath: "/ui",
  defaultPreload: "intent",
  scrollRestoration: true,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

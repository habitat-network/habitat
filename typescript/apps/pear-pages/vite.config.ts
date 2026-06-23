import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import viteTsConfigPaths from "vite-tsconfig-paths";
import tailwindcss from "@tailwindcss/vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";

// pear-pages produces the static pages that `pear` embeds and serves under the
// `/ui/` path (member login, instance admin login/settings). Everything is
// pre-rendered to static HTML at build time so the Go binary can embed the
// output and serve it without a Node runtime.
//
// `base` is `/ui/` so that asset URLs in the generated HTML resolve under the
// path pear mounts the app at. The dev server is reverse-proxied to by
// `pear:dev` (see HABITAT_UI_DEV_PROXY) so it must serve on the same base.
export default defineConfig({
  base: "/ui/",
  server: {
    port: 6010,
    host: true,
    allowedHosts: [".ts.net", ".local.habitat.network"],
  },
  plugins: [
    viteTsConfigPaths({ projects: ["./tsconfig.json"] }),
    tailwindcss(),
    tanstackStart({
      // Pre-render every page to static HTML so the output is a fully static
      // site that pear can embed. `crawlLinks` follows in-app links and the
      // explicit `pages` list guarantees the standalone routes are emitted.
      prerender: {
        enabled: true,
        crawlLinks: true,
      },
      pages: [
        { path: "/login/habitat" },
        { path: "/admin/login" },
        { path: "/admin" },
      ],
    }),
    viteReact(),
  ],
});

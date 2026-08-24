import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";

// pear-pages produces static pages that `pear` embeds and serves under /ui/.
// The app is a standard TanStack Router SPA using Vite. The output goes to
// `dist/` (see scripts/copy-to-embed.mjs) which the Go binary embeds.
export default defineConfig({
  base: "/ui/",
  server: {
    port: 6010,
    host: true,
    allowedHosts: [".ts.net", ".local.habitat.network"],
  },
  resolve: {
    tsconfigPaths: true,
  },
  plugins: [tanstackRouter(), tailwindcss(), viteReact()],
});

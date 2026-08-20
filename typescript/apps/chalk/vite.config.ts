import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import viteTsConfigPaths from "vite-tsconfig-paths";
import tailwindcss from "@tailwindcss/vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import { devtools } from "@tanstack/devtools-vite";
import { nitro } from "nitro/vite";

// TanStack Start's Vite plugin (tanstackStart) owns both the SSR/server-function
// build and file-based route generation (routeTree.gen.ts) — it supersedes the
// standalone @tanstack/router-plugin used by chalk's Vite-SPA siblings
// (docsv2, apps/docs). See @tanstack/react-start's bundled `react-start` skill
// (skills/react-start/SKILL.md) for the current setup shape; the plan's
// app.config.ts + @tanstack/react-start/config snippet is from an older
// version of the API and doesn't exist in the version pinned here.
export default defineConfig({
  server: {
    host: true,
    allowedHosts: [".ts.net", ".local.habitat.network"],
    port: process.env.SERVER_PORT
      ? parseInt(process.env.SERVER_PORT, 10)
      : undefined,
  },
  plugins: [
    // this is the plugin that enables path aliases
    viteTsConfigPaths({ projects: ["./tsconfig.json"] }),
    tailwindcss(),
    ...devtools({
      eventBusConfig: {
        port: parseInt(process.env.DEVTOOLS_PORT ?? "42069", 10),
      },
    }),
    // nitro provides the server build target (`.output/server/index.mjs`,
    // matching package.json's `start` script) that @tanstack/react-start's
    // SSR build runs on; @sentry/* is excluded from its rollup bundling
    // since Sentry isn't wired up here (scaffold default, kept in case it
    // is later).
    nitro({ rollupConfig: { external: [/^@sentry\//] } }),
    tanstackStart(), // must come before viteReact()
    viteReact(),
  ],
});

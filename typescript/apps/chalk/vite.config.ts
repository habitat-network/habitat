import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import { devtools } from "@tanstack/devtools-vite";
import { cloudflare } from "@cloudflare/vite-plugin";

// TanStack Start's Vite plugin (tanstackStart) owns both the SSR/server-function
// build and file-based route generation (routeTree.gen.ts) — it supersedes the
// standalone @tanstack/router-plugin used by chalk's Vite-SPA siblings
// (docsv2, apps/docs). See @tanstack/react-start's bundled `react-start` skill
// (skills/react-start/SKILL.md) for the current setup shape; the plan's
// app.config.ts + @tanstack/react-start/config snippet is from an older
// version of the API and doesn't exist in the version pinned here.
//
// Cloudflare deployment (this plan's whole point) does not go through Nitro
// at all in this version of Start: the `target: "cloudflare-module"` Nitro
// preset the plan describes does not exist here. Instead the `@cloudflare/
// vite-plugin`'s `cloudflare()` plugin builds the Workers output directly,
// and `wrangler.jsonc`'s `main` points at `src/server/entry.ts`, which
// re-exports Start's default request handler alongside every Durable
// Object class — see that file's comment for why a hand-written entry is
// needed instead of pointing `main` straight at the package export.
export default defineConfig({
  server: {
    host: true,
    allowedHosts: [".ts.net", ".local.habitat.network"],
    port: process.env.SERVER_PORT
      ? parseInt(process.env.SERVER_PORT, 10)
      : undefined,
  },
  resolve: {
    tsconfigPaths: true,
  },
  plugins: [
    cloudflare({ viteEnvironment: { name: "ssr" } }),
    tailwindcss(),
    ...devtools({
      eventBusConfig: {
        port: parseInt(process.env.DEVTOOLS_PORT ?? "42069", 10),
      },
    }),
    tanstackStart(), // must come before viteReact()
    viteReact(),
  ],
});

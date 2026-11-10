import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import { devtools } from "@tanstack/devtools-vite";
import { cloudflare } from "@cloudflare/vite-plugin";

// Same shape as chalk's vite.config.ts: TanStack Start owns the SSR/server
// function build and route generation, and `@cloudflare/vite-plugin` builds
// the Workers output directly. wrangler.jsonc's `main` points at
// src/server/entry.ts, which re-exports Start's handler beside the SyncHub
// Durable Object class.
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
    // Starts the devtools event bus (port 4206 by default) that the AI panel
    // uses to receive server-side chat events. Dev-only.
    ...devtools(),
    tanstackStart(), // must come before viteReact()
    viteReact(),
  ],
});

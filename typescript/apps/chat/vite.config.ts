import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
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
    tanstackStart(), // must come before viteReact()
    viteReact(),
  ],
});

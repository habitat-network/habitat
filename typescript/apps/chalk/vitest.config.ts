import { defineConfig } from "vitest/config";
import { cloudflareTest } from "@cloudflare/vitest-pool-workers";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";

// The plan this app follows was written against an older
// @cloudflare/vitest-pool-workers that exported `defineWorkersConfig` from a
// `/config` subpath. The pinned 0.22.0 replaced that with a `cloudflareTest`
// Vite plugin consumed by a normal vitest `defineConfig` instead — the
// `wrangler.configPath` option below is unchanged.
//
// `tanstackStart()` is required here too, not just in vite.config.ts: the
// worker entry (src/server/entry.ts) re-exports
// `@tanstack/react-start/server-entry`, whose module graph imports Start's
// virtual `#tanstack-router-entry` / `#tanstack-start-entry` subpaths. Those
// only resolve when the tanstackStart Vite plugin is present, so any test
// file that pulls in the worker entry needs it in this config too.
export default defineConfig({
  plugins: [
    tanstackStart(),
    cloudflareTest({
      wrangler: { configPath: "./wrangler.jsonc" },
    }),
  ],
});

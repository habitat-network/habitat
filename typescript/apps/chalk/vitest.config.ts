import path from "node:path";
import { defineConfig } from "vitest/config";
import {
  cloudflareTest,
  readD1Migrations,
} from "@cloudflare/vitest-pool-workers";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import { sqlRaw } from "./vite-sql-raw.ts";

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
//
// D1 starts empty in the workers runtime for every test file. This config
// file runs in Node (it has filesystem access), so it reads the drizzle
// migrations here and threads them into the worker as a TEST_MIGRATIONS
// binding; test/applyMigrations.ts (a setupFile, so it runs inside the
// worker before each test file) applies them via `applyD1Migrations`.
const migrations = await readD1Migrations(
  path.join(import.meta.dirname, "drizzle"),
);

export default defineConfig({
  test: {
    setupFiles: ["./test/applyMigrations.ts"],
  },
  plugins: [
    sqlRaw(),
    tanstackStart(),
    cloudflareTest({
      wrangler: { configPath: "./wrangler.jsonc" },
      miniflare: {
        bindings: { TEST_MIGRATIONS: migrations },
      },
    }),
  ],
});

import path from "node:path";
import { defineConfig } from "vitest/config";
import {
  cloudflareTest,
  readD1Migrations,
} from "@cloudflare/vitest-pool-workers";

// Mirrors chalk's vitest.config.ts: tests run inside workerd against a real
// (local) D1, whose schema is threaded in as TEST_MIGRATIONS and applied by
// test/applyMigrations.ts before each test file.
const migrations = await readD1Migrations(
  path.join(import.meta.dirname, "drizzle"),
);

export default defineConfig({
  resolve: {
    // tsconfigPaths resolves `@/` and the workspace packages' own path
    // aliases inside src/; tsconfig.json doesn't include test/, so the
    // tests' own `@/` imports need the explicit alias.
    tsconfigPaths: true,
    alias: { "@": path.join(import.meta.dirname, "src") },
  },
  test: {
    setupFiles: ["./test/applyMigrations.ts"],
  },
  plugins: [
    cloudflareTest({
      // The tests exercise the stores directly, not the Worker entry, so
      // they don't need Start's virtual modules (and its tanstackStart()
      // plugin) the way chalk's do.
      main: "./test/worker.ts",
      wrangler: { configPath: "./wrangler.jsonc" },
      miniflare: {
        bindings: {
          TEST_MIGRATIONS: migrations,
          DROP_CREDENTIALS_KEY: "dGVzdC1vbmx5LWNyZWRlbnRpYWxzLWtleS0zMmJ5dGU=",
        },
      },
    }),
  ],
});

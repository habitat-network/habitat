import { applyD1Migrations, type D1Migration } from "cloudflare:test";
import { env } from "cloudflare:workers";

declare global {
  namespace Cloudflare {
    interface Env {
      TEST_MIGRATIONS: D1Migration[];
    }
  }
}

// D1 in the vitest-pool-workers runtime starts empty for every test file; the
// migrations to apply are computed in Node (vitest.config.ts, via
// readD1Migrations) and threaded in as the TEST_MIGRATIONS binding since
// this setup file runs inside the worker and has no filesystem access.
await applyD1Migrations(env.DB, env.TEST_MIGRATIONS);

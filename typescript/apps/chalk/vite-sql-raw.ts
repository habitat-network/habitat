import type { Plugin } from "vite";

// drizzle-kit's `durable-sqlite` driver emits a `migrations.js` barrel that
// does `import m0000 from './0000_doc_room.sql'` — a Durable Object has no
// filesystem to read migrations from at runtime, so the SQL has to be
// inlined into the bundle. Vite has no built-in loader for `.sql`, so this
// turns each one into a module default-exporting its text.
//
// Shared by vite.config.ts and vitest.config.ts: dev, build, and the
// workers-pool tests all resolve that import through Vite.
export function sqlRaw(): Plugin {
  return {
    name: "chalk:sql-raw",
    transform(src, id) {
      if (!id.endsWith(".sql")) return null;
      return { code: `export default ${JSON.stringify(src)};`, map: null };
    },
  };
}

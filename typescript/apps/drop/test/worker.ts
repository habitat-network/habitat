// Test-only Worker entry (see vitest.config.ts): the store tests don't need
// Start's request handler, but wrangler.jsonc's Durable Object binding still
// needs its class exported from the entry module.
export { SyncHub } from "../src/server/syncHub";

export default {
  fetch: () => new Response("not found", { status: 404 }),
} satisfies ExportedHandler<Env>;

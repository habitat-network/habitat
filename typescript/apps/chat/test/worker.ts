// Test-only Worker entry (see vitest.config.ts): the tests call modules
// directly and don't need Start's request handler.
export default {
  fetch: () => new Response("not found", { status: 404 }),
} satisfies ExportedHandler<Env>;

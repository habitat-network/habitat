// Worker entry point, pointed to by wrangler.jsonc's `main`.
//
// `@tanstack/react-start/server-entry` is itself a valid Workers module
// default-exporting a `fetch` handler, and could be used as `main` directly
// — but a Workers module's Durable Object bindings must name classes
// exported by that same entry module, and a package export can't be
// extended with our own exports. So this file builds its own default export
// around Start's handler and adds every DO class beside it.
import startEntry from "@tanstack/react-start/server-entry";

export { DocRoom } from "./rooms/docRoom";
export { SapChannel } from "./rooms/sapChannel";

export default {
  // Only the request is forwarded: Start's handler is `(request, opts?)`,
  // where `opts` is its own RequestOptions — not Workers' `env`/`ctx`. It
  // reads bindings from `cloudflare:workers` at module scope like the rest
  // of this app does, so it needs nothing else from the invocation.
  fetch: (request) => startEntry.fetch(request),

  // `scheduled` has to be a method on the default export, not a sibling
  // named export — workerd resolves handlers off the default export object
  // only, and rejects a cron delivery with "Expected 'default' export ... to
  // define a `scheduled()` function" otherwise.
  //
  // The cron trigger (wrangler.jsonc's `triggers.crons`) is what guarantees
  // recovery from eviction; SapChannel's own close-alarm only makes ordinary
  // reconnects fast rather than up to a minute. Both are idempotent.
  scheduled: (_controller, env, ctx) => {
    ctx.waitUntil(env.SAP.get(env.SAP.idFromName("default")).ensureConnected());
  },
} satisfies ExportedHandler<Env>;

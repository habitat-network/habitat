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

export default {
  // Only the request is forwarded: Start's handler is `(request, opts?)`,
  // where `opts` is its own RequestOptions — not Workers' `env`/`ctx`. It
  // reads bindings from `cloudflare:workers` at module scope like the rest
  // of this app does, so it needs nothing else from the invocation.
  fetch: (request) => startEntry.fetch(request),
} satisfies ExportedHandler<Env>;

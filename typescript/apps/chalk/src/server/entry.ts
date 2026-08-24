// Worker entry point, pointed to by wrangler.jsonc's `main`.
//
// `@tanstack/react-start/server-entry` is itself a valid Workers module
// default-exporting a `fetch` handler, and could be used as `main` directly
// — but a Workers module's Durable Object bindings must name classes
// exported by that same entry module, and a package export can't be
// extended with our own exports. So this file re-exports Start's handler
// as its own default export and adds every DO class beside it.
export { default } from "@tanstack/react-start/server-entry";

export { PingRoom } from "./rooms/ping";
export { DocRoom } from "./rooms/docRoom";
export { SapChannel } from "./rooms/sapChannel";

// The cron trigger (wrangler.jsonc's `triggers.crons`) is what guarantees
// recovery from eviction; SapChannel's own close-alarm only makes ordinary
// reconnects fast rather than up to a minute. Both are idempotent.
export async function scheduled(
  _controller: ScheduledController,
  env: Env,
  ctx: ExecutionContext,
): Promise<void> {
  ctx.waitUntil(env.SAP.get(env.SAP.idFromName("default")).ensureConnected());
}

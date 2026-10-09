// Worker entry point, pointed to by wrangler.jsonc's `main`. Same shape as
// chalk's: a Durable Object binding must name a class exported by the entry
// module itself, so this wraps Start's handler and exports SyncHub beside it.
import startEntry from "@tanstack/react-start/server-entry";

export { SyncHub } from "./syncHub";

export default {
  fetch: (request) => startEntry.fetch(request),
} satisfies ExportedHandler<Env>;

// Worker entry point, pointed to by wrangler.jsonc's `main`. Wraps Start's
// fetch handler and adds the cron trigger's scheduled handler, which runs
// the sync passes that have come due (see src/server/sync.ts).
import startEntry from "@tanstack/react-start/server-entry";
import { runDueSpaces } from "./sync";

export default {
  fetch: (request) => startEntry.fetch(request),
  scheduled: (_controller, env, ctx) => {
    ctx.waitUntil(runDueSpaces(env));
  },
} satisfies ExportedHandler<Env>;

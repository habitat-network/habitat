// Worker entry point, pointed to by wrangler.jsonc's `main`.
//
// `@tanstack/react-start/server-entry` is itself a valid Workers module, but
// a Workers module's Durable Object bindings must name classes exported by
// that same entry module, and a package export can't be extended with our
// own exports. So this file builds its own default export around Start's
// handler and adds DocRoom and OrgStream beside it.
import startEntry from "@tanstack/react-start/server-entry";
import { Effect } from "effect";
import { isRunRequest } from "@/lib/apps";
import { runBackground } from "./effect/run";

export { DocRoom } from "./docs/rooms/docRoom";
export { OrgStream } from "./stream/orgStream";

// Requests to the run domain are Apps (src/server/apps/runner.ts), never
// the Habitat app: each app is answered by its own code, on its own origin.
export default {
  fetch: (request, env) =>
    isRunRequest(new URL(request.url), env.APP_RUN_DOMAIN)
      ? import("./apps/runner").then((runner) => runner.runApp(request, env))
      : startEntry.fetch(request),
  // The cron trigger makes sure every connected group's stream is running.
  // Imported lazily so this module's static graph stays just Start and the
  // Durable Objects.
  scheduled: (_controller, env, ctx) => {
    ctx.waitUntil(
      runBackground(
        "stream.followConnectedOrgs",
        Effect.promise(() =>
          import("./stream").then((stream) => stream.followConnectedOrgs(env)),
        ),
      ),
    );
  },
} satisfies ExportedHandler<Env>;

// Worker entry point, pointed to by wrangler.jsonc's `main`.
//
// `@tanstack/react-start/server-entry` is itself a valid Workers module, but
// a Workers module's Durable Object bindings must name classes exported by
// that same entry module, and a package export can't be extended with our
// own exports. So this file builds its own default export around Start's
// handler and adds DocRoom beside it.
import startEntry from "@tanstack/react-start/server-entry";
import { Effect } from "effect";
import { runBackground } from "./effect/run";

export { DocRoom } from "./docs/rooms/docRoom";

export default {
  fetch: (request) => startEntry.fetch(request),
  // The cron trigger runs the sync passes that have come due. Imported
  // lazily so this module's static graph stays just Start and DocRoom.
  scheduled: (_controller, env, ctx) => {
    ctx.waitUntil(
      runBackground(
        "sync.runDueSpaces",
        Effect.promise(() =>
          import("./sync").then((sync) => sync.runDueSpaces(env)),
        ),
      ),
    );
  },
} satisfies ExportedHandler<Env>;

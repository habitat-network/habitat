import { createFileRoute } from "@tanstack/react-router";
import { SpaceRef } from "@atproto/syntax";
import { Effect } from "effect";
import { hasDocAccess } from "@/server/docs/docs.server";
import { Forbidden } from "@/server/effect/errors";
import { currentMember, requireOrg } from "@/server/effect/member";
import { runHandler } from "@/server/effect/run";
import { AppEnv, Habitat } from "@/server/effect/services";

// Upgrades a browser's WebSocket request into DocRoom's own Hibernation-API
// socket (src/server/docs/rooms/docRoom.ts's fetch()). This route only does
// the access check DocRoom itself doesn't, then forwards the upgrade; the
// 101 Response comes back from the Durable Object untouched. $docId is the
// doc's full space URI, URL-encoded as one path segment.
export const Route = createFileRoute("/ws/$docId")({
  server: {
    handlers: {
      GET: ({ request, params }) =>
        runHandler(
          "docs.connect",
          Effect.gen(function* () {
            const did = yield* currentMember;
            // Every doc belongs to a group, and pear keeps reporting a removed
            // member's group-derived relations, so being a current member of
            // the doc's group is checked here too, before asking pear.
            const org = yield* Effect.try({
              try: () => SpaceRef.parse(params.docId).spaceDid,
              catch: () =>
                new Forbidden({ message: "You don't have access to this doc" }),
            });
            yield* requireOrg(org);
            const habitat = yield* Habitat;
            const allowed = yield* Effect.promise(() =>
              hasDocAccess(habitat.client(did), did, params.docId),
            );
            if (!allowed) {
              return yield* new Forbidden({
                message: "You don't have access to this doc",
              });
            }
            // DocRoom has no session cookie to read, so these headers are
            // how it learns who this already-checked connection is.
            const forwarded = new Request(request, {
              headers: new Headers(request.headers),
            });
            forwarded.headers.set("X-Chalk-Doc-Id", params.docId);
            forwarded.headers.set("X-Chalk-Member-Did", did);
            const env = yield* AppEnv;
            const stub = env.DOC.get(env.DOC.idFromName(params.docId));
            return yield* Effect.promise(() => stub.fetch(forwarded));
          }),
        ),
    },
  },
});

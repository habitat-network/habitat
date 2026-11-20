import { createFileRoute } from "@tanstack/react-router";
import type { DidString } from "@atproto/syntax";
import { Effect } from "effect";
import { fileByUri } from "@/db";
import { BadRequest, HabitatError, NotFound } from "@/server/effect/errors";
import { requireOrg } from "@/server/effect/member";
import { runHandler } from "@/server/effect/run";
import { AppEnv, Database, messageOf } from "@/server/effect/services";
import { getSpaceBlob } from "@/server/sync";

// GET /api/download?org=<group DID>&uri=<file record uri> serves a file's
// bytes, read from the group's space host on each request (the app keeps no
// copy). Readable by members of the file's group only.
export const Route = createFileRoute("/api/download")({
  server: {
    handlers: {
      GET: ({ request }) =>
        runHandler(
          "files.download",
          Effect.gen(function* () {
            const params = new URL(request.url).searchParams;
            const { org } = yield* requireOrg(params.get("org") ?? "");
            const uri = params.get("uri");
            if (!uri) return yield* new BadRequest({ message: "Missing uri" });

            const db = yield* Database;
            const env = yield* AppEnv;
            const file = yield* Effect.promise(() => fileByUri(db, uri));
            if (!file || file.orgDid !== org) {
              return yield* new NotFound({ message: "File not found" });
            }
            const bytes = yield* Effect.tryPromise({
              try: () =>
                getSpaceBlob(
                  env,
                  file.space,
                  file.repo as DidString,
                  file.blobCid,
                ),
              catch: (cause) =>
                new HabitatError({
                  nsid: "com.atproto.space.getBlob",
                  status: 502,
                  message: `Couldn't fetch the file: ${messageOf(cause)}`,
                }),
            });
            const headers = new Headers();
            headers.set("content-type", file.mimeType);
            headers.set("content-length", String(bytes.byteLength));
            headers.set(
              "content-disposition",
              `attachment; filename*=UTF-8''${encodeURIComponent(file.name)}`,
            );
            headers.set("cache-control", "private, max-age=3600");
            return new Response(bytes as Uint8Array<ArrayBuffer>, { headers });
          }),
        ),
    },
  },
});

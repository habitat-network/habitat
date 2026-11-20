import { createFileRoute } from "@tanstack/react-router";
import type { DidString } from "@atproto/syntax";
import { Effect } from "effect";
import { BadRequest, HabitatError } from "@/server/effect/errors";
import { requireOrg } from "@/server/effect/member";
import { runHandler } from "@/server/effect/run";
import {
  AppEnv,
  attempt,
  Habitat,
  toHabitatError,
} from "@/server/effect/services";
import {
  createFileSpace,
  FileTooLargeError,
  putFileRecord,
  uploadBlob,
} from "@/server/files/files.server";
import { requireConnected } from "@/server/groups";
import { syncNow } from "@/server/sync";
import { MAX_FILE_BYTES, formatBytes } from "@/lib/files";

// POST /api/upload?org=<group DID>&name=<file name> with the file's bytes as
// the body (and its type as Content-Type). A plain route rather than a
// server function so the browser can stream the body and report progress.
//
// The file gets its own group space (readable by every member), its blob,
// and the record naming it, all written with the group's own session. The
// list is only ever filled in by the syncer, so this waits briefly for the
// new space's first pass; if that failed, syncError says why.
export const Route = createFileRoute("/api/upload")({
  server: {
    handlers: {
      POST: ({ request }) =>
        runHandler(
          "files.upload",
          Effect.gen(function* () {
            const params = new URL(request.url).searchParams;
            const { did, org } = yield* requireOrg(params.get("org") ?? "");
            yield* requireConnected(org);
            const name = params.get("name")?.trim();
            if (!name)
              return yield* new BadRequest({ message: "Missing file name" });

            const bytes = new Uint8Array(
              yield* Effect.promise(() => request.arrayBuffer()),
            );
            if (bytes.byteLength > MAX_FILE_BYTES) {
              return yield* new HabitatError({
                nsid: "network.habitat.repo.uploadBlob",
                status: 413,
                message: `Files can be at most ${formatBytes(MAX_FILE_BYTES)}`,
              });
            }
            const mimeType =
              request.headers.get("content-type") || "application/octet-stream";

            const habitat = yield* Habitat;
            const env = yield* AppEnv;
            const session = yield* habitat.session(org);
            const space = yield* attempt(org, "files.createSpace", () =>
              createFileSpace(session, org),
            );
            const { blob } = yield* Effect.tryPromise({
              try: () => uploadBlob(session, bytes, mimeType),
              catch: (cause) =>
                cause instanceof FileTooLargeError
                  ? new HabitatError({
                      nsid: "network.habitat.repo.uploadBlob",
                      status: 413,
                      message: "That file is too large",
                    })
                  : toHabitatError(
                      org,
                      "network.habitat.repo.uploadBlob",
                      cause,
                    ),
            });
            yield* attempt(org, "files.putRecord", () =>
              putFileRecord(session, org, space, {
                name,
                blob,
                uploadedBy: did as DidString,
              }),
            );
            yield* Effect.logInfo("uploaded file", {
              space,
              bytes: bytes.byteLength,
              mimeType,
            });
            const { syncError } = yield* Effect.promise(() =>
              syncNow(env, space),
            );
            if (syncError) {
              yield* Effect.logWarning("first sync of uploaded file failed", {
                space,
                syncError,
              });
            }
            return Response.json({ space, syncError });
          }),
        ),
    },
  },
});

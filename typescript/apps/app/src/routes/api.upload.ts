import { createFileRoute } from "@tanstack/react-router";
import type { DidString } from "@atproto/syntax";
import { Effect } from "effect";
import { BadRequest, HabitatError } from "@/server/effect/errors";
import { requireOrg } from "@/server/effect/member";
import { runHandler } from "@/server/effect/run";
import { attempt, Habitat, toHabitatError } from "@/server/effect/services";
import {
  createFileSpace,
  FileTooLargeError,
  putFileRecord,
  uploadBlob,
} from "@/server/files/files.server";
import { requireConnected } from "@/server/groups";
import { MAX_FILE_BYTES, formatBytes } from "@/lib/files";

// POST /api/upload?org=<group DID>&name=<file name> with the file's bytes as
// the body (and its type as Content-Type). A plain route rather than a
// server function so the browser can stream the body and report progress.
//
// The file gets its own group space (readable by every member), its blob,
// and the record naming it, all written with the group's own session. The
// list is only ever filled in from the group's stream, so this returns once
// the stream has indexed the new record; if that took too long, syncError
// says so.
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
            // Returns once the group's stream has indexed the record, so
            // the file list already shows it.
            const { indexed } = yield* putFileRecord(org, space, {
              name,
              blob,
              uploadedBy: did as DidString,
            });
            yield* Effect.logInfo("uploaded file", {
              space,
              bytes: bytes.byteLength,
              mimeType,
              indexed,
            });
            const syncError = indexed
              ? undefined
              : "Uploaded, but it may take a moment to appear in the list";
            return Response.json({ space, syncError });
          }),
        ),
    },
  },
});

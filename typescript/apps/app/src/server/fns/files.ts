import { createServerFn } from "@tanstack/react-start";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { Effect, Schema } from "effect";
import { fileByUri, filesForOrg, type FileSummary } from "@/db";
import { deleteFileRecord } from "@/server/files/files.server";
import { NotFound } from "@/server/effect/errors";
import { requireOrg } from "@/server/effect/member";
import { runEffect } from "@/server/effect/run";
import { AppEnv, attempt, Database, Habitat } from "@/server/effect/services";
import { getProfiles, requireConnected } from "@/server/groups";
import { syncNow } from "@/server/sync";

// Files: Drop's shared file bucket. Each uploaded file is its own group
// space (type network.habitat.drop, readable by every member) holding one
// network.habitat.drop.file record, written with the group's own session.
// The list is the syncer's output, never the app's own writes. Uploading and
// downloading bytes are server routes (routes/api.upload.ts and
// routes/api.download.ts), so the browser can stream them.

export interface FileView extends FileSummary {
  uploaderName: string | null;
}

const OrgInput = Schema.Struct({ org: Schema.String });

// listFiles returns the group's synced files, newest first, with who
// uploaded each.
export const listFiles = createServerFn({ method: "GET" })
  .validator(Schema.decodeUnknownSync(OrgInput))
  .handler(({ data }) =>
    runEffect(
      "files.list",
      Effect.gen(function* () {
        const { did, org } = yield* requireOrg(data.org);
        const db = yield* Database;
        const rows = yield* Effect.promise(() => filesForOrg(db, org));
        const uploaders = [
          ...new Set(rows.flatMap((r) => (r.uploadedBy ? [r.uploadedBy] : []))),
        ];
        const profiles = yield* getProfiles(did, org, uploaders);
        return rows.map((r): FileView => {
          const p = r.uploadedBy ? profiles.get(r.uploadedBy) : undefined;
          return { ...r, uploaderName: p?.displayName || p?.handle || null };
        });
      }),
    ),
  );

// deleteFile deletes a file's record, which is all deleting a file takes:
// the blob is no longer referenced, so pear garbage collects it. Any member
// may delete, matching who can see the file.
export const deleteFile = createServerFn({ method: "POST" })
  .validator(
    Schema.decodeUnknownSync(
      Schema.Struct({ org: Schema.String, uri: Schema.String }),
    ),
  )
  .handler(({ data }) =>
    runEffect(
      "files.delete",
      Effect.gen(function* () {
        const { org } = yield* requireOrg(data.org);
        yield* requireConnected(org);
        const db = yield* Database;
        const file = yield* Effect.promise(() => fileByUri(db, data.uri));
        // A file in another group looks the same as one that doesn't exist.
        if (!file || file.orgDid !== org) {
          return yield* new NotFound({ message: "File not found" });
        }
        const habitat = yield* Habitat;
        const env = yield* AppEnv;
        const session = yield* habitat.session(org);
        yield* attempt(org, "files.deleteRecord", () =>
          deleteFileRecord(
            session,
            org as DidString,
            file.space as SpaceRefString,
          ),
        );
        yield* Effect.promise(() => syncNow(env, file.space as SpaceRefString));
        yield* Effect.logInfo("deleted file", { file: data.uri });
      }),
    ),
  );

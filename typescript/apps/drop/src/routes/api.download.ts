import { createFileRoute } from "@tanstack/react-router";
import { env } from "cloudflare:workers";
import type { DidString } from "@atproto/syntax";
import { fileByUri, getDb } from "@/db";
import { getFileBlob } from "@/server/sync";
import { useAppSession } from "@/server/session";

// GET /api/download?uri=<file record uri> serves a file's bytes, read from
// the org's space host on each request (Drop keeps no copy; files are capped
// at 500 KiB). Readable by members of the file's org only: the session's
// current org was checked against pear when it was selected (see selectOrg).
export const Route = createFileRoute("/api/download")({
  server: {
    handlers: {
      GET: async ({ request }) => {
        const session = await useAppSession();
        const { did, currentOrg } = session.data;
        if (!did) return new Response("unauthorized", { status: 401 });
        const uri = new URL(request.url).searchParams.get("uri");
        if (!uri) return new Response("missing uri", { status: 400 });

        const file = await fileByUri(getDb(env), uri);
        if (!file || file.orgDid !== currentOrg) {
          return new Response("not found", { status: 404 });
        }
        let bytes: Uint8Array;
        try {
          bytes = await getFileBlob(
            env,
            file.space,
            file.repo as DidString,
            file.blobCid,
          );
        } catch (err) {
          console.error("[drop] download", err);
          return new Response("couldn't fetch file", { status: 502 });
        }

        const headers = new Headers();
        headers.set("content-type", file.mimeType);
        headers.set("content-length", String(bytes.byteLength));
        headers.set(
          "content-disposition",
          `attachment; filename*=UTF-8''${encodeURIComponent(file.name)}`,
        );
        headers.set("cache-control", "private, max-age=3600");
        return new Response(bytes as Uint8Array<ArrayBuffer>, { headers });
      },
    },
  },
});

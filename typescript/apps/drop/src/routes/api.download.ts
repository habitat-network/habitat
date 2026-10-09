import { createFileRoute } from "@tanstack/react-router";
import { env } from "cloudflare:workers";
import { fileByUri, getDb } from "@/db";
import { useAppSession } from "@/server/session";

// GET /api/download?uri=<file record uri> streams a file from R2, where the
// sync sink mirrors every file's blob. Readable by members of the file's org
// only: the session's current org was checked against pear when it was
// selected (see selectOrg).
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
        const object = await env.FILES.get(file.blobCid);
        if (!object) return new Response("not found", { status: 404 });

        const headers = new Headers();
        headers.set("content-type", file.mimeType);
        headers.set("content-length", String(object.size));
        headers.set(
          "content-disposition",
          `attachment; filename*=UTF-8''${encodeURIComponent(file.name)}`,
        );
        headers.set("cache-control", "private, max-age=3600");
        return new Response(object.body, { headers });
      },
    },
  },
});

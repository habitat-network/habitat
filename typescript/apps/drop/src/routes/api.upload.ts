import { createFileRoute } from "@tanstack/react-router";
import { env } from "cloudflare:workers";
import type { DidString } from "@atproto/syntax";
import { useAppSession } from "@/server/session";
import { upload } from "@/server/sync";
import { MAX_FILE_BYTES } from "@/lib/files";

// POST /api/upload?name=<file name> with the file's bytes as the body (and
// its type as Content-Type). A plain route rather than a server function so
// the browser can stream the body and report upload progress.
export const Route = createFileRoute("/api/upload")({
  server: {
    handlers: {
      POST: async ({ request }) => {
        const session = await useAppSession();
        const { did, currentOrg } = session.data;
        if (!did)
          return Response.json({ message: "Sign in first" }, { status: 401 });
        if (!currentOrg) {
          return Response.json(
            { message: "Pick an organization first" },
            { status: 400 },
          );
        }
        const name = new URL(request.url).searchParams.get("name")?.trim();
        if (!name)
          return Response.json(
            { message: "Missing file name" },
            { status: 400 },
          );

        const bytes = new Uint8Array(await request.arrayBuffer());
        if (bytes.byteLength > MAX_FILE_BYTES) {
          return Response.json(
            { message: "Files can be at most 500 KB" },
            { status: 413 },
          );
        }
        const mimeType =
          request.headers.get("content-type") || "application/octet-stream";
        try {
          const result = await upload(env, {
            orgDid: currentOrg as DidString,
            memberDid: did as DidString,
            name,
            mimeType,
            bytes,
          });
          return Response.json(result);
        } catch (err) {
          console.error("[drop] upload", err);
          return Response.json(
            { message: err instanceof Error ? err.message : "Upload failed" },
            { status: 502 },
          );
        }
      },
    },
  },
});

import { createFileRoute } from "@tanstack/react-router";
import { env } from "cloudflare:workers";
import { notifySpaceDeleted, notifyWrite } from "@/server/sync";

// Inbound space notifications from space hosts. The body and Authorization
// header go to the syncer untouched: it verifies the service-auth JWT
// (issuer is the space authority, audience is Drop's service DID) itself.
export const Route = createFileRoute("/xrpc/$nsid")({
  server: {
    handlers: {
      POST: async ({ request, params }) => {
        const authorization = request.headers.get("authorization") ?? undefined;
        let body: unknown;
        try {
          body = await request.json();
        } catch {
          return xrpcError(400, "InvalidRequest", "body must be JSON");
        }
        try {
          switch (params.nsid) {
            case "com.atproto.space.notifyWrite":
              await notifyWrite(env, body, authorization);
              break;
            case "com.atproto.space.notifySpaceDeleted":
              await notifySpaceDeleted(env, body, authorization);
              break;
            default:
              return xrpcError(501, "MethodNotImplemented", params.nsid);
          }
        } catch (err) {
          return xrpcError(
            401,
            "AuthenticationRequired",
            err instanceof Error ? err.message : String(err),
          );
        }
        return new Response(null, { status: 200 });
      },
    },
  },
});

function xrpcError(status: number, error: string, message: string) {
  return Response.json({ error, message }, { status });
}

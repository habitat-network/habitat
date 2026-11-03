import { createFileRoute } from "@tanstack/react-router";
import { env } from "cloudflare:workers";
import { notificationErrorStatus } from "internal/spaceSync";
import { notifySpaceDeleted, notifyWrite } from "@/server/sync";

// Inbound space notifications from space hosts. The body and Authorization
// header go to the syncer untouched: it verifies the service-auth JWT
// (issuer is the space authority, audience is Brain's service DID) itself.
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
          // Space authorities retry a notification on 5xx and give up on
          // 4xx, so only a rejected caller or a malformed body is a 4xx.
          const status = notificationErrorStatus(err);
          return xrpcError(
            status,
            XRPC_ERRORS[status],
            err instanceof Error ? err.message : String(err),
          );
        }
        return new Response(null, { status: 200 });
      },
    },
  },
});

const XRPC_ERRORS = {
  400: "InvalidRequest",
  401: "AuthenticationRequired",
  500: "InternalServerError",
} as const;

function xrpcError(status: number, error: string, message: string) {
  return Response.json({ error, message }, { status });
}

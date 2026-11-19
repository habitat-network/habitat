import { createFileRoute } from "@tanstack/react-router";
import { env } from "cloudflare:workers";
import { notificationErrorStatus } from "internal/spaceSync";
import { confirmRegistration, notifySpaceUpdate } from "@/server/sync";

// Inbound space notifications from space hosts. The body goes to the syncer
// untouched: the token in it is the authentication.
export const Route = createFileRoute("/xrpc/$nsid")({
  server: {
    handlers: {
      POST: async ({ request, params }) => {
        let body: unknown;
        try {
          body = await request.json();
        } catch {
          return xrpcError(400, "InvalidRequest", "body must be JSON");
        }
        try {
          switch (params.nsid) {
            case "com.atproto.space.confirmRegistration":
              return Response.json(await confirmRegistration(env, body));
            case "com.atproto.space.notifySpaceUpdate":
              await notifySpaceUpdate(env, body);
              break;
            default:
              return xrpcError(501, "MethodNotImplemented", params.nsid);
          }
        } catch (err) {
          // Space hosts retry a notification on 5xx and give up on
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

import { createFileRoute } from "@tanstack/react-router";
import { env } from "cloudflare:workers";
import { serviceDid } from "@/server/sync";

// Drop's did:web document, which identifies it to the space hosts it syncs
// with. Notifications are delivered to the endpoint it registers with each
// credential request (src/routes/xrpc.$nsid.ts), not one named here.
export const Route = createFileRoute("/.well-known/did.json")({
  server: {
    handlers: {
      GET: () => {
        const id = serviceDid(env);
        return Response.json({
          "@context": ["https://www.w3.org/ns/did/v1"],
          id,
        });
      },
    },
  },
});

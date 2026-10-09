import { createFileRoute } from "@tanstack/react-router";
import { env } from "cloudflare:workers";
import { serviceDid } from "@/server/syncHub";

// Drop's did:web document. registerNotify subscribes this DID to a space's
// writes, and the space host delivers notifyWrite/notifySpaceDeleted to the
// service endpoint named here (src/routes/xrpc.$nsid.ts).
export const Route = createFileRoute("/.well-known/did.json")({
  server: {
    handlers: {
      GET: () => {
        const id = serviceDid(env);
        return Response.json({
          "@context": ["https://www.w3.org/ns/did/v1"],
          id,
          service: [
            {
              id: "#atproto_space_syncer",
              type: "AtprotoSpaceSyncer",
              serviceEndpoint: env.DROP_BASE_URL.replace(/\/+$/, ""),
            },
          ],
        });
      },
    },
  },
});

import { createFileRoute } from "@tanstack/react-router";
import { env } from "cloudflare:workers";

// The app's did:web document: its own identifier, derived from where it is
// served.
function serviceDid(env: Env): string {
  return `did:web:${new URL(env.APP_BASE_URL).host}`;
}

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

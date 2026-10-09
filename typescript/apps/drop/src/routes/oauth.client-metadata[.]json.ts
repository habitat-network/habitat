import { createFileRoute } from "@tanstack/react-router";
import { env } from "cloudflare:workers";
import { clientMetadata } from "@/server/oauth";

// Drop's OAuth client_id is this document's URL (atproto OAuth client ID
// metadata); pear fetches it to learn the redirect URIs and scopes.
export const Route = createFileRoute("/oauth/client-metadata.json")({
  server: {
    handlers: {
      GET: () => Response.json(clientMetadata(env.DROP_BASE_URL)),
    },
  },
});

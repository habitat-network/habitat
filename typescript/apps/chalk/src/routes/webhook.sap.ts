import { createFileRoute } from "@tanstack/react-router";
import { env } from "cloudflare:workers";
import { handleSapWebhook } from "@/server/webhook";

// sap POSTs each outbox message here as it's synced (cmd/sap/webhook.go),
// replacing the SapChannel Durable Object's outbound WebSocket to sap's
// /channel — that connection stayed open for the object's entire lifetime,
// which Durable Object "duration" billing charges for continuously (wall
// clock, not CPU time) regardless of how idle it is. A webhook POST is an
// ordinary, momentary Worker request instead.
export const Route = createFileRoute("/webhook/sap")({
  server: {
    handlers: {
      POST: ({ request }) => handleSapWebhook(request, env),
    },
  },
});

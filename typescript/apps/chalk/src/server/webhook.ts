import { processOutboxMessage } from "./outbox";
import type { OutboxMessage } from "./spaceUri";

// handleSapWebhook receives one outbox message POSTed by sap
// (cmd/sap/webhook.go's webhookConsumer). sap retries a non-2xx response
// with backoff and never advances past a message it hasn't gotten a 2xx
// for, so any failure here (bad auth, malformed body, a processing error)
// must return a non-2xx rather than throw uncaught — sap redelivers the
// same message either way, and processOutboxMessage is idempotent, so
// redelivery is always safe.
//
// sap's webhook consumer sends no custom headers (it's a bare POST), so
// auth is carried in the URL itself: CHALK_SAP_WEBHOOK_SECRET must match a
// `secret` query param baked into the SAP_WEBHOOK_URL configured on sap's
// side. Unset (local dev, where sap's webhook points at a loopback chalk
// nothing else can reach), no check is made.
export async function handleSapWebhook(
  request: Request,
  env: Env,
): Promise<Response> {
  const secret = env.CHALK_SAP_WEBHOOK_SECRET;
  if (secret) {
    const provided = new URL(request.url).searchParams.get("secret");
    if (provided !== secret) {
      return new Response("unauthorized", { status: 401 });
    }
  }
  let msg: OutboxMessage;
  try {
    msg = (await request.json()) as OutboxMessage;
  } catch (err) {
    console.error("[webhook] malformed body", err);
    return new Response("bad request", { status: 400 });
  }
  try {
    await processOutboxMessage(env, msg);
  } catch (err) {
    console.error("[webhook] process outbox message", msg.id, err);
    return new Response("error processing message", { status: 500 });
  }
  return new Response(null, { status: 200 });
}

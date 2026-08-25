import { env } from "cloudflare:test";
import { expect, it } from "vitest";
import { handleSapWebhook } from "../src/server/webhook";

// A message with no well-formed space-record URI: processOutboxMessage
// returns immediately without touching D1 or DOC, so these tests can focus
// on handleSapWebhook's own HTTP-layer behavior (auth, body parsing, status
// codes) without needing to set up a doc first.
const IGNORED_MESSAGE = { id: 1, uri: "not-a-uri", value: {} };

function post(body: unknown, url = "https://chalk.test/webhook/sap") {
  return new Request(url, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

it("processes the message and returns 200 when no secret is configured", async () => {
  const noSecretEnv = { ...env, CHALK_SAP_WEBHOOK_SECRET: undefined } as Env;
  const res = await handleSapWebhook(post(IGNORED_MESSAGE), noSecretEnv);
  expect(res.status).toBe(200);
});

it("rejects a request missing the secret when one is configured", async () => {
  const secretEnv = { ...env, CHALK_SAP_WEBHOOK_SECRET: "s3cret" } as Env;
  const res = await handleSapWebhook(post(IGNORED_MESSAGE), secretEnv);
  expect(res.status).toBe(401);
});

it("rejects a request with the wrong secret", async () => {
  const secretEnv = { ...env, CHALK_SAP_WEBHOOK_SECRET: "s3cret" } as Env;
  const res = await handleSapWebhook(
    post(IGNORED_MESSAGE, "https://chalk.test/webhook/sap?secret=wrong"),
    secretEnv,
  );
  expect(res.status).toBe(401);
});

it("accepts a request with the matching secret", async () => {
  const secretEnv = { ...env, CHALK_SAP_WEBHOOK_SECRET: "s3cret" } as Env;
  const res = await handleSapWebhook(
    post(IGNORED_MESSAGE, "https://chalk.test/webhook/sap?secret=s3cret"),
    secretEnv,
  );
  expect(res.status).toBe(200);
});

it("returns 400 on a malformed body", async () => {
  const noSecretEnv = { ...env, CHALK_SAP_WEBHOOK_SECRET: undefined } as Env;
  const req = new Request("https://chalk.test/webhook/sap", {
    method: "POST",
    body: "not json",
  });
  const res = await handleSapWebhook(req, noSecretEnv);
  expect(res.status).toBe(400);
});

import { test, expect } from "../harness/fixtures";

test("pear is healthy and serves its DID document", async ({ api }) => {
  const health = await api.get("/health");
  expect(health.ok).toBe(true);
  expect(await health.text()).toBe("ok");

  const did = await api.get("/.well-known/did.json");
  expect(did.ok).toBe(true);
  expect((await did.json()).id).toMatch(/^did:web:/);
});

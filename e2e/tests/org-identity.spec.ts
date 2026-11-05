import { test, expect } from "../harness/fixtures";

// Covers the identity half of the old Go TestMintThenLookup: a newly created
// org's DID resolves to a DID document through pear's host-routed endpoint.
test("a new org's DID resolves to its DID document", async ({
  api,
  createOrg,
}) => {
  const org = await createOrg();

  const didHost = org.did.replace("did:web:", "");
  const res = await api.getAsHost(didHost, "/.well-known/did.json");
  expect(res.status).toBe(200);
  expect(res.headers.get("content-type")).toBe("application/did+ld+json");
  expect((await res.json()).id).toBe(org.did);
});

test("an email-domain org cannot be created twice for one domain", async ({
  api,
  createOrg,
}) => {
  const org = await createOrg();
  await expect(
    api.createOrg({ handle: `${org.handle}x`, domain: org.domain }),
  ).rejects.toThrow(/409|domain/i);
});

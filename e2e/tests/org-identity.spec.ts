import { test, expect } from "../harness/fixtures";

// Covers the identity half of the old Go TestMintThenLookup: a newly created
// identity resolves handle -> DID and DID -> DID document through pear's
// host-routed endpoints.
test("a new org admin resolves by handle and by DID", async ({
  api,
  createOrg,
}) => {
  const org = await createOrg();

  const handleRes = await api.getAsHost(
    org.adminHandle,
    "/.well-known/atproto-did",
  );
  expect(handleRes.status).toBe(200);
  expect((await handleRes.text()).trim()).toBe(org.adminDid);

  const didHost = org.adminDid.replace("did:web:", "");
  const docRes = await api.getAsHost(didHost, "/.well-known/did.json");
  expect(docRes.status).toBe(200);
  expect(docRes.headers.get("content-type")).toBe("application/did+ld+json");
  const doc = await docRes.json();
  expect(doc.id).toBe(org.adminDid);
  expect(doc.alsoKnownAs).toEqual([`at://${org.adminHandle}`]);
});

import { env } from "cloudflare:test";
import { beforeEach, expect, it } from "vitest";
import {
  getDb,
  upsertDoc,
  upsertDocAccess,
  upsertDocOrgAccess,
  deleteDocOrgAccess,
  upsertConnectedOrg,
  docsFor,
  docByUri,
} from "../src/db";

const URI = "at://did:web:alice.example/space/network.habitat.docs/abc";
const ALICE = "did:web:alice.example";
const BOB = "did:web:bob.example";

beforeEach(async () => {
  await env.DB.exec("DELETE FROM docs");
  await env.DB.exec("DELETE FROM doc_access");
  await env.DB.exec("DELETE FROM doc_org_access");
  await env.DB.exec("DELETE FROM connected_orgs");
});

it("returns a subject's docs newest first", async () => {
  const db = getDb(env);
  await upsertDoc(db, {
    spaceUri: URI,
    docId: URI,
    ownerDid: ALICE,
    title: "Untitled",
  });
  await upsertDocAccess(db, {
    uri: `${URI}/${ALICE}/network.habitat.relationship.userRelation/self`,
    spaceUri: URI,
    subjectDid: ALICE,
    relation: "owner",
  });
  const rows = await docsFor(db, ALICE);
  expect(rows).toEqual([
    {
      docId: URI,
      uri: URI,
      ownerDid: ALICE,
      title: "Untitled",
    },
  ]);
});

it("excludes docs the subject has no grant on", async () => {
  const db = getDb(env);
  await upsertDoc(db, {
    spaceUri: URI,
    docId: URI,
    ownerDid: ALICE,
    title: "Untitled",
  });
  await upsertDocAccess(db, {
    uri: `${URI}/${ALICE}/network.habitat.relationship.userRelation/self`,
    spaceUri: URI,
    subjectDid: ALICE,
    relation: "owner",
  });
  expect(await docsFor(db, BOB)).toEqual([]);
});

it("upserts on conflict rather than duplicating", async () => {
  const db = getDb(env);
  const doc = {
    spaceUri: URI,
    docId: URI,
    ownerDid: ALICE,
    title: "Untitled",
  };
  await upsertDoc(db, doc);
  await upsertDoc(db, { ...doc, title: "Renamed" });
  await upsertDocAccess(db, {
    uri: `${URI}/${ALICE}/network.habitat.relationship.userRelation/self`,
    spaceUri: URI,
    subjectDid: ALICE,
    relation: "owner",
  });
  expect(await docsFor(db, ALICE)).toHaveLength(1);
  expect((await docByUri(db, URI))?.title).toBe("Renamed");
});

it("returns undefined for an unknown uri", async () => {
  expect(await docByUri(getDb(env), "at://nope/space/x/y")).toBeUndefined();
});

const ORG = "did:web:org.example";
const ORG_DOC = "at://did:web:org.example/space/network.habitat.docs/abc";

async function seedOrgDoc(db: ReturnType<typeof getDb>) {
  await upsertDoc(db, {
    spaceUri: ORG_DOC,
    docId: ORG_DOC,
    ownerDid: ORG,
    title: "Org doc",
  });
}

const orgDocSummary = {
  docId: ORG_DOC,
  uri: ORG_DOC,
  ownerDid: ORG,
  title: "Org doc",
};

it("personal mode excludes an org's docs even with a personal grant", async () => {
  const db = getDb(env);
  await seedOrgDoc(db);
  // The grant a doc's creator gets in org mode. It must not drag the org
  // doc into their personal list.
  await upsertDocAccess(db, {
    uri: `${ORG_DOC}/${ALICE}/network.habitat.relationship.userRelation/self`,
    spaceUri: ORG_DOC,
    subjectDid: ALICE,
    relation: "manager",
  });
  await upsertConnectedOrg(db, {
    memberDid: ALICE,
    orgDid: ORG,
    orgName: "Org",
  });
  expect(await docsFor(db, ALICE)).toEqual([]);
});

it("personal mode keeps another person's doc shared with the subject", async () => {
  const db = getDb(env);
  await upsertDoc(db, {
    spaceUri: URI,
    docId: URI,
    ownerDid: ALICE,
    title: "Untitled",
  });
  await upsertDocAccess(db, {
    uri: `${URI}/${BOB}/network.habitat.relationship.userRelation/self`,
    spaceUri: URI,
    subjectDid: BOB,
    relation: "reader",
  });
  // ALICE is a person, not a connected org, so her doc stays listed.
  await upsertConnectedOrg(db, {
    memberDid: BOB,
    orgDid: ORG,
    orgName: "Org",
  });
  expect(await docsFor(db, BOB)).toEqual([
    { docId: URI, uri: URI, ownerDid: ALICE, title: "Untitled" },
  ]);
});

it("org mode hides an org doc nobody has been granted", async () => {
  const db = getDb(env);
  await seedOrgDoc(db);
  // Org docs are created with no access roles, so owning the doc is not by
  // itself permission to see it — without a grant it must stay hidden.
  expect(await docsFor(db, BOB, ORG)).toEqual([]);
});

it("org mode includes a doc shared with the whole org", async () => {
  const db = getDb(env);
  await seedOrgDoc(db);
  await upsertDocOrgAccess(db, {
    uri: `${ORG_DOC}/${ORG}/network.habitat.relationship.spaceRelation/self`,
    spaceUri: ORG_DOC,
    orgDid: ORG,
    relation: "reader",
  });
  expect(await docsFor(db, BOB, ORG)).toEqual([orgDocSummary]);
});

it("org mode includes a doc the subject holds a personal grant on", async () => {
  const db = getDb(env);
  await seedOrgDoc(db);
  // What the doc's creator gets: a manager grant, no org-wide share.
  await upsertDocAccess(db, {
    uri: `${ORG_DOC}/${ALICE}/network.habitat.relationship.userRelation/self`,
    spaceUri: ORG_DOC,
    subjectDid: ALICE,
    relation: "manager",
  });
  expect(await docsFor(db, ALICE, ORG)).toEqual([orgDocSummary]);
  expect(await docsFor(db, BOB, ORG)).toEqual([]);
});

it("org mode lists a doc once when granted both ways", async () => {
  const db = getDb(env);
  await seedOrgDoc(db);
  await upsertDocOrgAccess(db, {
    uri: `${ORG_DOC}/${ORG}/network.habitat.relationship.spaceRelation/self`,
    spaceUri: ORG_DOC,
    orgDid: ORG,
    relation: "reader",
  });
  await upsertDocAccess(db, {
    uri: `${ORG_DOC}/${ALICE}/network.habitat.relationship.userRelation/self`,
    spaceUri: ORG_DOC,
    subjectDid: ALICE,
    relation: "manager",
  });
  expect(await docsFor(db, ALICE, ORG)).toEqual([orgDocSummary]);
});

it("org mode drops a doc once its org-wide grant is revoked", async () => {
  const db = getDb(env);
  await seedOrgDoc(db);
  const relationUri = `${ORG_DOC}/${ORG}/network.habitat.relationship.spaceRelation/self`;
  await upsertDocOrgAccess(db, {
    uri: relationUri,
    spaceUri: ORG_DOC,
    orgDid: ORG,
    relation: "reader",
  });
  await deleteDocOrgAccess(db, relationUri);
  expect(await docsFor(db, BOB, ORG)).toEqual([]);
});

it("org mode excludes personal docs and other orgs' docs", async () => {
  const db = getDb(env);
  await upsertDoc(db, {
    spaceUri: URI,
    docId: URI,
    ownerDid: ALICE,
    title: "Personal doc",
  });
  await upsertDocAccess(db, {
    uri: `${URI}/${ALICE}/network.habitat.relationship.userRelation/self`,
    spaceUri: URI,
    subjectDid: ALICE,
    relation: "owner",
  });
  const otherOrgDoc =
    "at://did:web:other.example/space/network.habitat.docs/xyz";
  await upsertDoc(db, {
    spaceUri: otherOrgDoc,
    docId: otherOrgDoc,
    ownerDid: "did:web:other.example",
    title: "Other org's doc",
  });
  await upsertDocOrgAccess(db, {
    uri: `${otherOrgDoc}/did:web:other.example/network.habitat.relationship.spaceRelation/self`,
    spaceUri: otherOrgDoc,
    orgDid: "did:web:other.example",
    relation: "reader",
  });
  expect(await docsFor(db, ALICE, ORG)).toEqual([]);
});

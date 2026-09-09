import { env } from "cloudflare:test";
import { beforeEach, expect, it } from "vitest";
import {
  getDb,
  upsertDoc,
  upsertDocAccess,
  upsertDocOrgAccess,
  deleteDocOrgAccess,
  docsForAccessor,
  docsForOrg,
  docByUri,
} from "../src/db";

const URI = "at://did:web:alice.example/space/network.habitat.docs/abc";
const ALICE = "did:web:alice.example";
const BOB = "did:web:bob.example";

beforeEach(async () => {
  await env.DB.exec("DELETE FROM docs");
  await env.DB.exec("DELETE FROM doc_access");
  await env.DB.exec("DELETE FROM doc_org_access");
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
  const rows = await docsForAccessor(db, ALICE);
  expect(rows).toEqual([
    {
      docId: URI,
      uri: URI,
      ownerDid: ALICE,
      title: "Untitled",
      isOrg: false,
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
  expect(await docsForAccessor(db, BOB)).toEqual([]);
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
  expect(await docsForAccessor(db, ALICE)).toHaveLength(1);
  expect((await docByUri(db, URI))?.title).toBe("Renamed");
});

it("returns undefined for an unknown uri", async () => {
  expect(await docByUri(getDb(env), "at://nope/space/x/y")).toBeUndefined();
});

it("stamps isOrg on the row and reflects it back", async () => {
  const db = getDb(env);
  await upsertDoc(db, {
    spaceUri: URI,
    docId: URI,
    ownerDid: "did:web:org.example",
    title: "Untitled",
    isOrg: true,
  });
  expect((await docByUri(db, URI))?.isOrg).toBe(true);
});

it("defaults isOrg to false when not given", async () => {
  const db = getDb(env);
  await upsertDoc(db, {
    spaceUri: URI,
    docId: URI,
    ownerDid: ALICE,
    title: "Untitled",
  });
  expect((await docByUri(db, URI))?.isOrg).toBe(false);
});

it("leaves isOrg untouched on a re-upsert that doesn't specify it", async () => {
  const db = getDb(env);
  await upsertDoc(db, {
    spaceUri: URI,
    docId: URI,
    ownerDid: "did:web:org.example",
    title: "Untitled",
    isOrg: true,
  });
  // Mirrors docRoom.ts's content-flush upsert: re-indexes title without an
  // opinion on isOrg. This must not silently reset it to false.
  await upsertDoc(db, {
    spaceUri: URI,
    docId: URI,
    ownerDid: "did:web:org.example",
    title: "Renamed",
  });
  expect((await docByUri(db, URI))?.isOrg).toBe(true);
});

const ORG = "did:web:org.example";
const ORG_DOC = "at://did:web:org.example/space/network.habitat.docs/abc";

async function seedOrgDoc(db: ReturnType<typeof getDb>) {
  await upsertDoc(db, {
    spaceUri: ORG_DOC,
    docId: ORG_DOC,
    ownerDid: ORG,
    title: "Org doc",
    isOrg: true,
  });
}

const orgDocSummary = {
  docId: ORG_DOC,
  uri: ORG_DOC,
  ownerDid: ORG,
  title: "Org doc",
  isOrg: true,
};

it("docsForOrg hides an org doc nobody has been granted", async () => {
  const db = getDb(env);
  await seedOrgDoc(db);
  // Org docs are created with no access roles, so owning the doc is not by
  // itself permission to see it — without a grant it must stay hidden.
  expect(await docsForOrg(db, ORG, BOB)).toEqual([]);
});

it("docsForOrg includes a doc shared with the whole org", async () => {
  const db = getDb(env);
  await seedOrgDoc(db);
  await upsertDocOrgAccess(db, {
    uri: `${ORG_DOC}/${ORG}/network.habitat.relationship.spaceRelation/self`,
    spaceUri: ORG_DOC,
    orgDid: ORG,
    relation: "reader",
  });
  expect(await docsForOrg(db, ORG, BOB)).toEqual([orgDocSummary]);
});

it("docsForOrg includes a doc the subject holds a personal grant on", async () => {
  const db = getDb(env);
  await seedOrgDoc(db);
  // What the doc's creator gets: a manager grant, no org-wide share.
  await upsertDocAccess(db, {
    uri: `${ORG_DOC}/${ALICE}/network.habitat.relationship.userRelation/self`,
    spaceUri: ORG_DOC,
    subjectDid: ALICE,
    relation: "manager",
  });
  expect(await docsForOrg(db, ORG, ALICE)).toEqual([orgDocSummary]);
  expect(await docsForOrg(db, ORG, BOB)).toEqual([]);
});

it("docsForOrg lists a doc once when granted both ways", async () => {
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
  expect(await docsForOrg(db, ORG, ALICE)).toEqual([orgDocSummary]);
});

it("docsForOrg drops a doc once its org-wide grant is revoked", async () => {
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
  expect(await docsForOrg(db, ORG, BOB)).toEqual([]);
});

it("docsForOrg excludes personal docs and other orgs' docs", async () => {
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
    isOrg: true,
  });
  await upsertDocOrgAccess(db, {
    uri: `${otherOrgDoc}/did:web:other.example/network.habitat.relationship.spaceRelation/self`,
    spaceUri: otherOrgDoc,
    orgDid: "did:web:other.example",
    relation: "reader",
  });
  expect(await docsForOrg(db, ORG, ALICE)).toEqual([]);
});

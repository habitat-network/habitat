import { env } from "cloudflare:test";
import { beforeEach, expect, it } from "vitest";
import {
  getDb,
  upsertDoc,
  upsertDocAccess,
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

it("docsForOrg returns every doc owned by the org regardless of doc_access", async () => {
  const db = getDb(env);
  const orgDoc = "at://did:web:org.example/space/network.habitat.docs/abc";
  await upsertDoc(db, {
    spaceUri: orgDoc,
    docId: orgDoc,
    ownerDid: "did:web:org.example",
    title: "Org doc",
    isOrg: true,
  });
  // No doc_access row for this doc at all — org-mode listing must not
  // require one.
  const rows = await docsForOrg(db, "did:web:org.example");
  expect(rows).toEqual([
    {
      docId: orgDoc,
      uri: orgDoc,
      ownerDid: "did:web:org.example",
      title: "Org doc",
      isOrg: true,
    },
  ]);
});

it("docsForOrg excludes personal docs and other orgs' docs", async () => {
  const db = getDb(env);
  await upsertDoc(db, {
    spaceUri: URI,
    docId: URI,
    ownerDid: ALICE,
    title: "Personal doc",
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
  expect(await docsForOrg(db, "did:web:org.example")).toEqual([]);
});

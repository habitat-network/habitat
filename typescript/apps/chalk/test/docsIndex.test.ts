import { env } from "cloudflare:test";
import { beforeEach, expect, it } from "vitest";
import {
  getDb,
  upsertDoc,
  upsertDocAccess,
  docsForAccessor,
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

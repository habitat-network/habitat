import { env } from "cloudflare:test";
import { beforeEach, expect, it } from "vitest";
import { getDb, upsertDoc, docsForOwner, docByUri } from "../src/db";

const URI = "at://did:web:alice.example/space/network.habitat.docs/abc";

beforeEach(async () => {
  await env.DB.exec("DELETE FROM docs");
});

it("returns a member's docs newest first", async () => {
  const db = getDb(env);
  await upsertDoc(db, { spaceUri: URI, docId: URI, ownerDid: "did:web:alice.example", title: "Untitled" });
  const rows = await docsForOwner(db, "did:web:alice.example");
  expect(rows).toEqual([
    { docId: URI, uri: URI, ownerDid: "did:web:alice.example", title: "Untitled" },
  ]);
});

it("upserts on conflict rather than duplicating", async () => {
  const db = getDb(env);
  const doc = { spaceUri: URI, docId: URI, ownerDid: "did:web:alice.example", title: "Untitled" };
  await upsertDoc(db, doc);
  await upsertDoc(db, { ...doc, title: "Renamed" });
  expect(await docsForOwner(db, "did:web:alice.example")).toHaveLength(1);
  expect((await docByUri(db, URI))?.title).toBe("Renamed");
});

it("returns undefined for an unknown uri", async () => {
  expect(await docByUri(getDb(env), "at://nope/space/x/y")).toBeUndefined();
});

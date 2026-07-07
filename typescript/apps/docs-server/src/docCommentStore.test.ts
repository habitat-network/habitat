import { DatabaseSync } from "node:sqlite";
import { describe, expect, it } from "vitest";
import { DocCommentStore } from "./docCommentStore";

const DOC = "ats://did:web:org.example/network.habitat.docs/doc1";
const CSPACE = "ats://did:web:org.example/network.habitat.docs.comments/doc1";

function newStore(): DocCommentStore {
  return new DocCommentStore(new DatabaseSync(":memory:"));
}

describe("DocCommentStore", () => {
  it("returns a top-level comment with its range and no replies", () => {
    const store = newStore();
    store.upsertComment({
      docSpace: DOC,
      uri: `${CSPACE}/did:web:alice/network.habitat.docs.comment/c1`,
      author: "did:web:alice",
      body: "first",
      createdAt: "2026-07-07T00:00:00.000Z",
      range: { start: "s", end: "e" },
    });

    const threads = store.threadsForDoc(DOC);

    expect(threads).toHaveLength(1);
    expect(threads[0].body).toBe("first");
    expect(threads[0].author).toBe("did:web:alice");
    expect(threads[0].range).toEqual({ start: "s", end: "e" });
    expect(threads[0].replies).toEqual([]);
  });

  it("nests replies under their parent, oldest first, excluded from top level", () => {
    const store = newStore();
    const parentUri = `${CSPACE}/did:web:alice/network.habitat.docs.comment/c1`;
    store.upsertComment({
      docSpace: DOC,
      uri: parentUri,
      author: "did:web:alice",
      body: "parent",
      createdAt: "2026-07-07T00:00:00.000Z",
      range: { start: "s", end: "e" },
    });
    store.upsertComment({
      docSpace: DOC,
      uri: `${CSPACE}/did:web:bob/network.habitat.docs.comment/r2`,
      author: "did:web:bob",
      body: "second reply",
      createdAt: "2026-07-07T00:00:02.000Z",
      parentUri,
    });
    store.upsertComment({
      docSpace: DOC,
      uri: `${CSPACE}/did:web:bob/network.habitat.docs.comment/r1`,
      author: "did:web:bob",
      body: "first reply",
      createdAt: "2026-07-07T00:00:01.000Z",
      parentUri,
    });

    const threads = store.threadsForDoc(DOC);

    expect(threads).toHaveLength(1);
    expect(threads[0].replies.map((r) => r.body)).toEqual([
      "first reply",
      "second reply",
    ]);
  });

  it("upsert on the same uri overwrites the previous body", () => {
    const store = newStore();
    const uri = `${CSPACE}/did:web:alice/network.habitat.docs.comment/c1`;
    const base = {
      docSpace: DOC,
      uri,
      author: "did:web:alice",
      createdAt: "2026-07-07T00:00:00.000Z",
      range: { start: "s", end: "e" },
    };
    store.upsertComment({ ...base, body: "before" });
    store.upsertComment({ ...base, body: "after" });

    const threads = store.threadsForDoc(DOC);
    expect(threads).toHaveLength(1);
    expect(threads[0].body).toBe("after");
  });

  it("orders top-level comments oldest first", () => {
    const store = newStore();
    store.upsertComment({
      docSpace: DOC,
      uri: `${CSPACE}/did:web:alice/network.habitat.docs.comment/b`,
      author: "did:web:alice",
      body: "newer",
      createdAt: "2026-07-07T00:00:05.000Z",
    });
    store.upsertComment({
      docSpace: DOC,
      uri: `${CSPACE}/did:web:alice/network.habitat.docs.comment/a`,
      author: "did:web:alice",
      body: "older",
      createdAt: "2026-07-07T00:00:01.000Z",
    });

    const threads = store.threadsForDoc(DOC);
    expect(threads.map((t) => t.body)).toEqual(["older", "newer"]);
  });
});

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";
import {
  commentsSpaceUri,
  docSpaceUriForComments,
  ensureCommentsSpace,
  parseCommentRecordUri,
  parseCommentsRecordUri,
  removeComment,
  removeReply,
  resolveThread,
  writeComment,
  writeReply,
  COMMENT_REPLY_COLLECTION,
} from "../src/server/comments.server";
import { SapClient } from "../src/server/sapClient";
import { env as cfEnv } from "cloudflare:test";
import {
  commentsForDoc,
  getDb,
  repliesForDoc,
  resolutionsForDoc,
  upsertComment,
} from "../src/db";

const DOC = "at://did:web:alice.example/space/network.habitat.docs/abc";
const COMMENTS_SPACE =
  "at://did:web:alice.example/space/network.habitat.docs.comments/abc";
const ALICE = "did:web:alice.example";
const BOB = "did:web:bob.example";

describe("commentsSpaceUri", () => {
  it("derives the comments space from the doc space: same owner and key, different type", () => {
    expect(commentsSpaceUri(DOC)).toBe(COMMENTS_SPACE);
  });

  it("returns undefined for a malformed docId", () => {
    expect(commentsSpaceUri("not-a-uri")).toBeUndefined();
  });
});

describe("docSpaceUriForComments", () => {
  it("inverts commentsSpaceUri", () => {
    expect(docSpaceUriForComments(COMMENTS_SPACE)).toBe(DOC);
  });

  it("returns undefined for a space that isn't a comments space", () => {
    expect(docSpaceUriForComments(DOC)).toBeUndefined();
  });
});

describe("parseCommentRecordUri / parseCommentsRecordUri", () => {
  it("parses a well-formed root comment record URI", () => {
    expect(
      parseCommentRecordUri(
        `${COMMENTS_SPACE}/${BOB}/network.habitat.docs.comment/3jzfcijpj2z2a`,
      ),
    ).toEqual({
      spaceUri: COMMENTS_SPACE,
      docSpaceUri: DOC,
      repo: BOB,
      rkey: "3jzfcijpj2z2a",
    });
  });

  it("rejects a record in the wrong collection", () => {
    expect(
      parseCommentRecordUri(
        `${COMMENTS_SPACE}/${BOB}/network.habitat.docs.crdt/self`,
      ),
    ).toBeUndefined();
  });

  it("rejects a record whose space isn't a comments space", () => {
    expect(
      parseCommentRecordUri(`${DOC}/${BOB}/network.habitat.docs.comment/1`),
    ).toBeUndefined();
  });

  it("rejects a malformed URI", () => {
    expect(parseCommentRecordUri("not-a-uri")).toBeUndefined();
  });

  it("parseCommentsRecordUri parses a reply record given the reply collection", () => {
    expect(
      parseCommentsRecordUri(
        `${COMMENTS_SPACE}/${BOB}/${COMMENT_REPLY_COLLECTION}/xyz`,
        COMMENT_REPLY_COLLECTION,
      ),
    ).toEqual({
      spaceUri: COMMENTS_SPACE,
      docSpaceUri: DOC,
      repo: BOB,
      rkey: "xyz",
    });
  });

  it("parseCommentsRecordUri rejects a comment record when asked for the reply collection", () => {
    expect(
      parseCommentsRecordUri(
        `${COMMENTS_SPACE}/${BOB}/network.habitat.docs.comment/1`,
        COMMENT_REPLY_COLLECTION,
      ),
    ).toBeUndefined();
  });
});

const testEnv = {
  CHALK_SAP_INTERNAL_URL: "http://sap-internal.test",
} as Env;

describe("ensureCommentsSpace", () => {
  const server = setupServer();
  beforeEach(() => server.listen({ onUnhandledRequest: "error" }));
  afterEach(() => {
    server.resetHandlers();
    server.close();
  });

  it("creates a personal comments space and grants doc readers/writers as its readers/writers", async () => {
    const setRelationBodies: unknown[] = [];
    let createBody: unknown;
    let trackedSpace: unknown;
    server.use(
      http.post(
        "http://sap-internal.test/proxy/network.habitat.simplespace.createSpace",
        async ({ request }) => {
          createBody = await request.json();
          return HttpResponse.json({ uri: COMMENTS_SPACE });
        },
      ),
      http.post(
        "http://sap-internal.test/proxy/network.habitat.relationship.setSpaceRelation",
        async ({ request }) => {
          setRelationBodies.push(await request.json());
          return HttpResponse.json({ uri: `${COMMENTS_SPACE}/rel` });
        },
      ),
      http.post("http://sap-internal.test/space/track", async ({ request }) => {
        trackedSpace = await request.json();
        return new HttpResponse(null, { status: 200 });
      }),
    );
    const client = new SapClient(testEnv, ALICE);
    const result = await ensureCommentsSpace(client, client, DOC, {
      ownerDid: ALICE,
      isOrg: false,
    });
    expect(result).toBe(COMMENTS_SPACE);
    expect(createBody).toEqual({
      did: ALICE,
      type: "network.habitat.docs.comments",
      skey: "abc",
    });
    expect(setRelationBodies).toEqual([
      {
        subject: DOC,
        subjectRole: "reader",
        relation: "reader",
        space: COMMENTS_SPACE,
      },
      {
        subject: DOC,
        subjectRole: "writer",
        relation: "writer",
        space: COMMENTS_SPACE,
      },
    ]);
    // sap otherwise has no way to discover the comments space until some
    // member's next session crawl — see ensureCommentsSpace's comment.
    expect(trackedSpace).toEqual({ space: COMMENTS_SPACE });
  });

  it("creates an org comments space via community.opensocial.createSpace, proxied to the org", async () => {
    let proxyHeader: string | null = null;
    let createBody: unknown;
    server.use(
      http.post(
        "http://sap-internal.test/proxy/community.opensocial.createSpace",
        async ({ request }) => {
          createBody = await request.json();
          proxyHeader = request.headers.get("Atproto-Proxy");
          return HttpResponse.json({ uri: COMMENTS_SPACE });
        },
      ),
      http.post(
        "http://sap-internal.test/proxy/network.habitat.relationship.setSpaceRelation",
        () => HttpResponse.json({ uri: `${COMMENTS_SPACE}/rel` }),
      ),
      http.post(
        "http://sap-internal.test/space/track",
        () => new HttpResponse(null, { status: 200 }),
      ),
    );
    const client = new SapClient(testEnv, ALICE);
    const managementClient = new SapClient(testEnv, "did:web:org.example");
    const result = await ensureCommentsSpace(client, managementClient, DOC, {
      ownerDid: "did:web:org.example",
      isOrg: true,
    });
    expect(result).toBe(COMMENTS_SPACE);
    expect(createBody).toEqual({
      org: "did:web:org.example",
      type: "network.habitat.docs.comments",
      skey: "abc",
      roles: [],
    });
    expect(proxyHeader).toBe("did:web:org.example#habitat");
  });

  it("is safe to call again once the space and relations already exist", async () => {
    server.use(
      http.post(
        "http://sap-internal.test/proxy/network.habitat.simplespace.createSpace",
        () =>
          HttpResponse.json({ error: "SpaceAlreadyExists" }, { status: 400 }),
      ),
      http.post(
        "http://sap-internal.test/proxy/network.habitat.relationship.setSpaceRelation",
        () => HttpResponse.json({ uri: `${COMMENTS_SPACE}/rel` }),
      ),
      http.post(
        "http://sap-internal.test/space/track",
        () => new HttpResponse(null, { status: 200 }),
      ),
    );
    const client = new SapClient(testEnv, ALICE);
    const result = await ensureCommentsSpace(client, client, DOC, {
      ownerDid: ALICE,
      isOrg: false,
    });
    expect(result).toBe(COMMENTS_SPACE);
  });

  it("returns undefined for a malformed docId without making any calls", async () => {
    server.use(
      http.post("http://sap-internal.test/proxy/*", () => {
        throw new Error("should not be called");
      }),
    );
    const client = new SapClient(testEnv, ALICE);
    expect(
      await ensureCommentsSpace(client, client, "not-a-uri", {
        ownerDid: ALICE,
        isOrg: false,
      }),
    ).toBeUndefined();
  });
});

describe("writeComment / writeReply / resolveThread / removeComment / removeReply", () => {
  const server = setupServer();
  beforeEach(async () => {
    server.listen({ onUnhandledRequest: "error" });
    await cfEnv.DB.exec("DELETE FROM comments");
    await cfEnv.DB.exec("DELETE FROM comment_replies");
    await cfEnv.DB.exec("DELETE FROM comment_resolutions");
  });
  afterEach(() => {
    server.resetHandlers();
    server.close();
  });

  function db() {
    return getDb(cfEnv);
  }

  it("writeComment creates the comments space, writes the record with its CRDT anchor, and mirrors it (with cid) into D1", async () => {
    server.use(
      http.post(
        "http://sap-internal.test/proxy/network.habitat.simplespace.createSpace",
        () => HttpResponse.json({ uri: COMMENTS_SPACE }),
      ),
      http.post(
        "http://sap-internal.test/proxy/network.habitat.relationship.setSpaceRelation",
        () => HttpResponse.json({ uri: `${COMMENTS_SPACE}/rel` }),
      ),
      http.post(
        "http://sap-internal.test/proxy/network.habitat.space.putRecord",
        async ({ request }) => {
          const body = (await request.json()) as { record: unknown };
          expect(body.record).toMatchObject({
            anchorStart: { $bytes: "c3RhcnQtcmVsLXBvcw" },
            anchorEnd: { $bytes: "ZW5kLXJlbC1wb3M" },
          });
          return HttpResponse.json({
            uri: `${COMMENTS_SPACE}/${ALICE}/network.habitat.docs.comment/1`,
            cid: "bafycomment1",
          });
        },
      ),
      http.post(
        "http://sap-internal.test/space/track",
        () => new HttpResponse(null, { status: 200 }),
      ),
    );
    const client = new SapClient(testEnv, ALICE);
    const view = await writeComment(client, client, db(), ALICE, DOC, {
      body: "great point",
      anchorStart: "c3RhcnQtcmVsLXBvcw",
      anchorEnd: "ZW5kLXJlbC1wb3M",
      ownerDid: ALICE,
      isOrg: false,
    });
    expect(view).toMatchObject({
      authorDid: ALICE,
      body: "great point",
      anchorStart: "c3RhcnQtcmVsLXBvcw",
      anchorEnd: "ZW5kLXJlbC1wb3M",
      cid: "bafycomment1",
    });
    const rows = await commentsForDoc(db(), DOC);
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ uri: view.uri, cid: "bafycomment1" });
  });

  it("writeReply writes a commentReply record referencing the root by strongRef, and mirrors it into D1", async () => {
    let putBody: unknown;
    server.use(
      http.post(
        "http://sap-internal.test/proxy/network.habitat.space.putRecord",
        async ({ request }) => {
          putBody = await request.json();
          return HttpResponse.json({
            uri: `${COMMENTS_SPACE}/${BOB}/network.habitat.docs.commentReply/1`,
          });
        },
      ),
    );
    const client = new SapClient(testEnv, BOB);
    const root = {
      uri: `${COMMENTS_SPACE}/${ALICE}/network.habitat.docs.comment/1`,
      cid: "bafycomment1",
    };
    const reply = await writeReply(client, db(), BOB, DOC, {
      comment: root,
      body: "I agree",
    });
    expect(putBody).toMatchObject({
      space: COMMENTS_SPACE,
      collection: "network.habitat.docs.commentReply",
      record: expect.objectContaining({ comment: root, body: "I agree" }),
    });
    expect(reply).toMatchObject({
      commentUri: root.uri,
      authorDid: BOB,
      body: "I agree",
    });
    const rows = await repliesForDoc(db(), DOC);
    expect(rows).toHaveLength(1);
    expect(rows[0].commentUri).toBe(root.uri);
  });

  it("resolveThread writes a commentResolution record referencing the root by strongRef, into the resolver's own repo — not the root author's", async () => {
    const root = {
      uri: `${COMMENTS_SPACE}/${BOB}/network.habitat.docs.comment/1`,
      cid: "bafycomment1",
    };
    // Bob wrote the thread's root comment...
    await upsertComment(db(), {
      uri: root.uri,
      cid: root.cid,
      docSpaceUri: DOC,
      authorDid: BOB,
      body: "first",
      anchorStart: "a",
      anchorEnd: "b",
      createdAt: 1000,
    });

    let putBody: unknown;
    server.use(
      http.post(
        "http://sap-internal.test/proxy/network.habitat.space.putRecord",
        async ({ request }) => {
          putBody = await request.json();
          return HttpResponse.json({
            uri: `${COMMENTS_SPACE}/${ALICE}/network.habitat.docs.commentResolution/1`,
          });
        },
      ),
    );

    // ...but Alice, who never commented, is the one resolving it.
    const client = new SapClient(testEnv, ALICE);
    await resolveThread(client, db(), ALICE, DOC, root, true);

    expect(putBody).toMatchObject({
      space: COMMENTS_SPACE,
      repo: ALICE,
      collection: "network.habitat.docs.commentResolution",
      record: expect.objectContaining({ comment: root, resolved: true }),
    });

    const rows = await resolutionsForDoc(db(), DOC);
    expect(rows).toEqual([
      expect.objectContaining({
        docSpaceUri: DOC,
        commentUri: root.uri,
        resolverDid: ALICE,
        resolved: true,
      }),
    ]);

    // The root comment record itself is untouched — resolving never
    // rewrites it (it can't; Alice doesn't own Bob's repo).
    const comments = await commentsForDoc(db(), DOC);
    expect(comments).toHaveLength(1);
    expect(comments[0].body).toBe("first");
  });

  it("resolveThread rejects a docId that isn't a well-formed space URI", async () => {
    const client = new SapClient(testEnv, ALICE);
    const root = { uri: `${COMMENTS_SPACE}/${ALICE}/x/1`, cid: "c" };
    await expect(
      resolveThread(client, db(), ALICE, "not-a-uri", root, true),
    ).rejects.toThrow("invalid docId");
  });

  it("removeComment deletes the caller's own record and its local mirror row", async () => {
    const uri = `${COMMENTS_SPACE}/${ALICE}/network.habitat.docs.comment/1`;
    await upsertComment(db(), {
      uri,
      cid: "cid1",
      docSpaceUri: DOC,
      authorDid: ALICE,
      body: "oops",
      anchorStart: "a",
      anchorEnd: "b",
    });
    let deleteBody: unknown;
    server.use(
      http.post(
        "http://sap-internal.test/proxy/network.habitat.space.deleteRecord",
        async ({ request }) => {
          deleteBody = await request.json();
          return new HttpResponse(null, { status: 200 });
        },
      ),
    );
    const client = new SapClient(testEnv, ALICE);
    await removeComment(client, db(), ALICE, uri);
    expect(deleteBody).toEqual({
      space: COMMENTS_SPACE,
      repo: ALICE,
      collection: "network.habitat.docs.comment",
      rkey: "1",
    });
    expect(await commentsForDoc(db(), DOC)).toEqual([]);
  });

  it("removeComment refuses to delete another author's comment", async () => {
    const uri = `${COMMENTS_SPACE}/${BOB}/network.habitat.docs.comment/1`;
    const client = new SapClient(testEnv, ALICE);
    await expect(removeComment(client, db(), ALICE, uri)).rejects.toThrow(
      "forbidden",
    );
  });

  it("removeReply refuses to delete another author's reply", async () => {
    const uri = `${COMMENTS_SPACE}/${BOB}/${COMMENT_REPLY_COLLECTION}/1`;
    const client = new SapClient(testEnv, ALICE);
    await expect(removeReply(client, db(), ALICE, uri)).rejects.toThrow(
      "forbidden",
    );
  });
});

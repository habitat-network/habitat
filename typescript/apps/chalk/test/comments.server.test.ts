import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";
import {
  commentsSpaceUri,
  docSpaceUriForComments,
  ensureCommentsSpace,
  parseCommentRecordUri,
  removeComment,
  setResolved,
  writeComment,
} from "../src/server/comments.server";
import { SapClient } from "../src/server/sapClient";
import { env as cfEnv } from "cloudflare:test";
import {
  commentsForDoc,
  commentsInThread,
  getDb,
  upsertComment,
} from "../src/db";

const DOC =
  "at://did:web:alice.example/space/network.habitat.docs/abc";
const COMMENTS_SPACE =
  "at://did:web:alice.example/space/network.habitat.docs.comments/abc";

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

describe("parseCommentRecordUri", () => {
  it("parses a well-formed comment record URI", () => {
    expect(
      parseCommentRecordUri(
        `${COMMENTS_SPACE}/did:web:bob.example/network.habitat.docs.comment/3jzfcijpj2z2a`,
      ),
    ).toEqual({
      spaceUri: COMMENTS_SPACE,
      docSpaceUri: DOC,
      repo: "did:web:bob.example",
      rkey: "3jzfcijpj2z2a",
    });
  });

  it("rejects a record in the wrong collection", () => {
    expect(
      parseCommentRecordUri(
        `${COMMENTS_SPACE}/did:web:bob.example/network.habitat.docs.crdt/self`,
      ),
    ).toBeUndefined();
  });

  it("rejects a record whose space isn't a comments space", () => {
    expect(
      parseCommentRecordUri(
        `${DOC}/did:web:bob.example/network.habitat.docs.comment/1`,
      ),
    ).toBeUndefined();
  });

  it("rejects a malformed URI", () => {
    expect(parseCommentRecordUri("not-a-uri")).toBeUndefined();
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
    );
    const client = new SapClient(testEnv, "did:web:alice.example");
    const result = await ensureCommentsSpace(client, DOC, {
      ownerDid: "did:web:alice.example",
      isOrg: false,
    });
    expect(result).toBe(COMMENTS_SPACE);
    expect(createBody).toEqual({
      did: "did:web:alice.example",
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
    );
    const client = new SapClient(testEnv, "did:web:alice.example");
    const result = await ensureCommentsSpace(client, DOC, {
      ownerDid: "did:web:org.example",
      isOrg: true,
    });
    expect(result).toBe(
      "at://did:web:alice.example/space/network.habitat.docs.comments/abc",
    );
    expect(createBody).toEqual({
      org: "did:web:org.example",
      type: "network.habitat.docs.comments",
      skey: "abc",
      roles: ["admin", "member"],
    });
    expect(proxyHeader).toBe("did:web:org.example#habitat");
  });

  it("is safe to call again once the space and relations already exist", async () => {
    server.use(
      http.post(
        "http://sap-internal.test/proxy/network.habitat.simplespace.createSpace",
        () =>
          HttpResponse.json(
            { error: "SpaceAlreadyExists" },
            { status: 400 },
          ),
      ),
      http.post(
        "http://sap-internal.test/proxy/network.habitat.relationship.setSpaceRelation",
        () => HttpResponse.json({ uri: `${COMMENTS_SPACE}/rel` }),
      ),
    );
    const client = new SapClient(testEnv, "did:web:alice.example");
    const result = await ensureCommentsSpace(client, DOC, {
      ownerDid: "did:web:alice.example",
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
    const client = new SapClient(testEnv, "did:web:alice.example");
    expect(
      await ensureCommentsSpace(client, "not-a-uri", {
        ownerDid: "did:web:alice.example",
        isOrg: false,
      }),
    ).toBeUndefined();
  });
});

describe("writeComment / setResolved / removeComment", () => {
  const server = setupServer();
  beforeEach(async () => {
    server.listen({ onUnhandledRequest: "error" });
    await cfEnv.DB.exec("DELETE FROM comments");
  });
  afterEach(() => {
    server.resetHandlers();
    server.close();
  });

  function db() {
    return getDb(cfEnv);
  }

  it("writeComment creates the comments space, writes the record, and mirrors it into D1", async () => {
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
        () =>
          HttpResponse.json({
            uri: `${COMMENTS_SPACE}/did:web:alice.example/network.habitat.docs.comment/1`,
          }),
      ),
    );
    const client = new SapClient(testEnv, "did:web:alice.example");
    const view = await writeComment(
      client,
      db(),
      "did:web:alice.example",
      DOC,
      {
        threadId: "t1",
        body: "great point",
        ownerDid: "did:web:alice.example",
        isOrg: false,
      },
    );
    expect(view).toMatchObject({
      threadId: "t1",
      authorDid: "did:web:alice.example",
      body: "great point",
      resolved: false,
    });
    const rows = await commentsForDoc(db(), DOC);
    expect(rows).toHaveLength(1);
    expect(rows[0].uri).toBe(view.uri);
  });

  it("setResolved rewrites the caller's own record and stamps every row in the local mirror", async () => {
    const aliceUri = `${COMMENTS_SPACE}/did:web:alice.example/network.habitat.docs.comment/1`;
    const bobUri = `${COMMENTS_SPACE}/did:web:bob.example/network.habitat.docs.comment/1`;
    await upsertComment(db(), {
      uri: aliceUri,
      docSpaceUri: DOC,
      threadId: "t1",
      authorDid: "did:web:alice.example",
      body: "first",
      createdAt: 1000,
    });
    await upsertComment(db(), {
      uri: bobUri,
      docSpaceUri: DOC,
      threadId: "t1",
      authorDid: "did:web:bob.example",
      body: "reply",
      createdAt: 2000,
    });

    const putBodies: unknown[] = [];
    server.use(
      http.post(
        "http://sap-internal.test/proxy/network.habitat.space.putRecord",
        async ({ request }) => {
          putBodies.push(await request.json());
          return HttpResponse.json({ uri: aliceUri });
        },
      ),
    );

    const client = new SapClient(testEnv, "did:web:alice.example");
    const thread = await commentsInThread(db(), DOC, "t1");
    await setResolved(
      client,
      db(),
      "did:web:alice.example",
      DOC,
      "t1",
      true,
      thread,
    );

    // Only alice's own record is rewritten upstream — pear has no way to
    // let her rewrite bob's.
    expect(putBodies).toHaveLength(1);
    expect(putBodies[0]).toMatchObject({
      repo: "did:web:alice.example",
      rkey: "1",
      record: expect.objectContaining({ resolved: true }),
    });

    // But the local mirror reflects resolved on every row of the thread.
    const rows = await commentsInThread(db(), DOC, "t1");
    expect(rows.every((r) => r.resolved)).toBe(true);
  });

  it("removeComment deletes the caller's own record and its local mirror row", async () => {
    const uri = `${COMMENTS_SPACE}/did:web:alice.example/network.habitat.docs.comment/1`;
    await upsertComment(db(), {
      uri,
      docSpaceUri: DOC,
      threadId: "t1",
      authorDid: "did:web:alice.example",
      body: "oops",
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
    const client = new SapClient(testEnv, "did:web:alice.example");
    await removeComment(client, db(), "did:web:alice.example", uri);
    expect(deleteBody).toEqual({
      space: COMMENTS_SPACE,
      repo: "did:web:alice.example",
      collection: "network.habitat.docs.comment",
      rkey: "1",
    });
    expect(await commentsForDoc(db(), DOC)).toEqual([]);
  });

  it("removeComment refuses to delete another author's comment", async () => {
    const uri = `${COMMENTS_SPACE}/did:web:bob.example/network.habitat.docs.comment/1`;
    const client = new SapClient(testEnv, "did:web:alice.example");
    await expect(
      removeComment(client, db(), "did:web:alice.example", uri),
    ).rejects.toThrow("forbidden");
  });
});

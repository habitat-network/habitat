import { DatabaseSync } from "node:sqlite";
import { describe, expect, it } from "vitest";
import { DocCommentStore } from "./docCommentStore";
import { createApp } from "./server";
import type { DerivedConfig } from "./config";
import type { PearClient } from "./pearClient";
import type { DocCrdtStore } from "./docCrdtStore";
import type { DocMetadataStore } from "./docMetadataStore";
import type { OrgDirectory } from "./orgDirectory";
import type { ServiceAuthVerifier } from "./serviceAuth";

const CALLER = "did:web:alice";
const ORG = "did:web:org.example";
const DOC_ID = "doc1";
const DOC_SPACE = `ats://${ORG}/network.habitat.docs/${DOC_ID}`;
const COMMENT_SPACE = `ats://${ORG}/network.habitat.docs.comments/${DOC_ID}`;

const config = {
  domain: "docs-server.test",
  port: 0,
  db: ":memory:",
  did: "did:web:docs-server.test",
  serviceId: "docs",
  sapUrl: "http://sap.test",
} satisfies DerivedConfig;

const fakeVerifier = {
  verify: async () => CALLER,
} as unknown as ServiceAuthVerifier;

function harness(allowed: boolean) {
  const comments = new DocCommentStore(new DatabaseSync(":memory:"));
  const pear = {
    spaceUri: (skey: string, org: string) =>
      `ats://${org}/network.habitat.docs/${skey}`,
    commentSpaceUri: (skey: string, org: string) =>
      `ats://${org}/network.habitat.docs.comments/${skey}`,
    check: async () => allowed,
    ensureCommentSpace: async () => COMMENT_SPACE,
  } as unknown as PearClient;
  const orgs = { orgForUser: () => ORG } as unknown as OrgDirectory;
  const app = createApp(
    config,
    pear,
    {} as DocCrdtStore,
    {} as DocMetadataStore,
    orgs,
    comments,
    fakeVerifier,
  );
  return { app, comments };
}

function req(app: ReturnType<typeof harness>["app"]) {
  return app.request(
    `/xrpc/network.habitat.docs.listComments?docId=${DOC_ID}`,
    { headers: { Authorization: "Bearer test" } },
  );
}

describe("listComments route", () => {
  it("returns grouped threads for a doc the caller can read", async () => {
    const { app, comments } = harness(true);
    comments.upsertComment({
      docSpace: DOC_SPACE,
      uri: `${COMMENT_SPACE}/${CALLER}/network.habitat.docs.comment/c1`,
      author: CALLER,
      body: "hello",
      createdAt: "2026-07-07T00:00:00.000Z",
      range: { start: "s", end: "e" },
    });

    const res = await req(app);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { comments: { body: string }[] };
    expect(body.comments).toHaveLength(1);
    expect(body.comments[0].body).toBe("hello");
  });

  it("rejects a caller who cannot read the comment space", async () => {
    const { app } = harness(false);
    const res = await req(app);
    expect(res.status).toBe(403);
  });
});

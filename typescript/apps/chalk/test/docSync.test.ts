import { DatabaseSync } from "node:sqlite";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as Y from "yjs";
import { DocStore } from "../src/server/docStore";
import { DocPubSub } from "../src/server/pubsub";
import { DocSync, parseSpaceRecordUri } from "../src/server/docSync";

describe("parseSpaceRecordUri", () => {
  it("parses a well-formed space-record URI", () => {
    expect(
      parseSpaceRecordUri(
        "at://did:plc:owner/space/network.habitat.docs/abc/did:plc:member/network.habitat.docs.crdt/self",
      ),
    ).toEqual({
      spaceUri: "at://did:plc:owner/space/network.habitat.docs/abc",
      owner: "did:plc:owner",
      type: "network.habitat.docs",
      skey: "abc",
      repo: "did:plc:member",
      collection: "network.habitat.docs.crdt",
    });
  });

  it("returns undefined for a malformed URI", () => {
    expect(parseSpaceRecordUri("not-a-uri")).toBeUndefined();
  });
});

describe("DocSync.handleOutboxMessage", () => {
  let store: DocStore;
  let pubsub: DocPubSub;
  let sync: DocSync;

  beforeEach(() => {
    store = new DocStore(new DatabaseSync(":memory:"));
    pubsub = new DocPubSub();
    store.upsertDoc({
      spaceUri: "at://did:plc:owner/space/network.habitat.docs/abc",
      docId: "abc",
      ownerDid: "did:plc:owner",
      title: "Untitled",
    });
    sync = new DocSync({
      sapWsUrl: "ws://unused.test",
      store,
      pubsub,
      ownerClientFor: () =>
        ({
          call: vi.fn(async () => ({})),
          uploadBlob: vi.fn(async () => ({ blob: {}, cid: "cid1" })),
        }) as unknown as import("../src/server/sapClient").SapClient,
      render: (ydoc) => ({
        title: ydoc.getText("t").toString().slice(0, 10) || "Untitled",
        markdown: ydoc.getText("t").toString(),
      }),
    });
  });

  it("merges a member crdt record into the space's Y.Doc and publishes it", async () => {
    const memberDoc = new Y.Doc();
    memberDoc.getText("t").insert(0, "hello");
    const update = Buffer.from(Y.encodeStateAsUpdateV2(memberDoc)).toString(
      "base64",
    );

    const published: string[] = [];
    const consume = (async () => {
      for await (const doc of pubsub.subscribe(
        "at://did:plc:owner/space/network.habitat.docs/abc",
      )) {
        published.push(doc.getText("t").toString());
        break;
      }
    })();

    await sync.handleOutboxMessage({
      id: 1,
      uri: "at://did:plc:owner/space/network.habitat.docs/abc/did:plc:member/network.habitat.docs.crdt/self",
      value: { blob: update },
    });

    await consume;
    expect(published).toEqual(["hello"]);
    expect(
      store
        .mergedState("at://did:plc:owner/space/network.habitat.docs/abc")
        ?.getText("t")
        .toString(),
    ).toBe("hello");
  });

  it("ignores messages for unrelated collections", async () => {
    await expect(
      sync.handleOutboxMessage({
        id: 2,
        uri: "at://did:plc:owner/space/network.habitat.docs/abc/did:plc:member/some.other.collection/self",
        value: {},
      }),
    ).resolves.toBeUndefined();
  });
});

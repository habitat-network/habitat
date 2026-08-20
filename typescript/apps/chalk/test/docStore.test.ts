import { beforeEach, describe, expect, it } from "vitest";
import * as Y from "yjs";
import { DocStore } from "../src/server/docStore";
import { createTestDb } from "./testDb";

describe("DocStore", () => {
  let store: DocStore;
  beforeEach(() => {
    store = new DocStore(createTestDb());
  });

  it("round-trips doc metadata by owner and by URI", () => {
    store.upsertDoc({
      spaceUri: "at://did:plc:owner/space/network.habitat.docs/abc",
      docId: "abc",
      ownerDid: "did:plc:owner",
      title: "My Doc",
    });
    expect(store.docsForOwner("did:plc:owner")).toEqual([
      {
        docId: "abc",
        uri: "at://did:plc:owner/space/network.habitat.docs/abc",
        ownerDid: "did:plc:owner",
        title: "My Doc",
      },
    ]);
    expect(
      store.docsByUris(["at://did:plc:owner/space/network.habitat.docs/abc"]),
    ).toHaveLength(1);
    expect(store.docsByUris(["at://nope"])).toEqual([]);
  });

  it("persists and reloads merged Yjs state for a space", () => {
    const spaceUri = "at://did:plc:owner/space/network.habitat.docs/abc";
    expect(store.mergedState(spaceUri)).toBeUndefined();

    const ydoc = new Y.Doc();
    ydoc.getText("content").insert(0, "hello");
    store.persistMerged(spaceUri, ydoc);

    const reloaded = store.mergedState(spaceUri);
    expect(reloaded?.getText("content").toString()).toBe("hello");
  });
});

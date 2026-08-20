import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as Y from "yjs";
import { DocStore } from "../src/server/docStore";
import { DocPubSub } from "../src/server/pubsub";
import { createTestDb } from "./testDb";

const call = vi.fn(async () => ({}));
const uploadBlob = vi.fn(async () => ({ blob: {}, cid: "cid1" }));
let getBlobBytes = new Uint8Array();
const getBlob = vi.fn(async () => getBlobBytes);

// docSync.ts constructs its own SapClient(ownerDid) directly rather than
// taking one as an injected option (sap always resolves via a bare DID
// under WithSingleSessionPerUser, so there's nothing left to inject), so
// exercising it here means mocking the module instead.
vi.mock("../src/server/sapClient", () => ({
  SapClient: vi.fn().mockImplementation(function SapClient() {
    return { call, uploadBlob, getBlob };
  }),
}));

const { DocSync, parseSpaceRecordUri } = await import("../src/server/docSync");

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
  let sync: InstanceType<typeof DocSync>;

  beforeEach(() => {
    call.mockClear();
    uploadBlob.mockClear();
    getBlob.mockClear();
    store = new DocStore(createTestDb());
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
      render: (ydoc) => ({
        title: ydoc.getText("t").toString().slice(0, 10) || "Untitled",
        markdown: ydoc.getText("t").toString(),
      }),
    });
  });

  it("merges a member crdt record into the space's Y.Doc and publishes it", async () => {
    const memberDoc = new Y.Doc();
    memberDoc.getText("t").insert(0, "hello");
    getBlobBytes = Y.encodeStateAsUpdateV2(memberDoc);

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
      value: { blob: { ref: { $link: "cid1" } } },
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

  it("applyEdit merges and publishes an update without fetching from sap", async () => {
    const memberDoc = new Y.Doc();
    memberDoc.getText("t").insert(0, "hi");

    const published: string[] = [];
    const consume = (async () => {
      for await (const doc of pubsub.subscribe(
        "at://did:plc:owner/space/network.habitat.docs/abc",
      )) {
        published.push(doc.getText("t").toString());
        break;
      }
    })();

    sync.applyEdit(
      "at://did:plc:owner/space/network.habitat.docs/abc",
      Y.encodeStateAsUpdateV2(memberDoc),
    );

    await consume;
    expect(published).toEqual(["hi"]);
    expect(getBlob).not.toHaveBeenCalled();
    expect(
      store
        .mergedState("at://did:plc:owner/space/network.habitat.docs/abc")
        ?.getText("t")
        .toString(),
    ).toBe("hi");
  });
});

describe("DocSync merges into persisted state after a restart", () => {
  // this.ydocs starts empty on every process start, but an incoming update
  // is an *incremental* diff relative to whatever state the sender already
  // had, not a full snapshot. A fresh DocSync (simulating chalk restarting
  // with a doc that was already persisted by a previous process) must seed
  // its in-memory doc from that persisted state before merging, or the
  // merge reconstructs only the new fragment and clobbers the previously
  // saved content — exactly the "edits don't survive a refresh" bug this
  // guards against.
  it("preserves previously-persisted content when merging a new incremental edit", () => {
    const store = new DocStore(createTestDb());
    const pubsub = new DocPubSub();
    const spaceUri = "at://did:plc:owner/space/network.habitat.docs/abc";
    store.upsertDoc({
      spaceUri,
      docId: "abc",
      ownerDid: "did:plc:owner",
      title: "Existing",
    });

    // Simulate content already persisted by a previous process — DocSync's
    // own in-memory ydocs map knows nothing about this yet.
    const priorState = new Y.Doc();
    priorState.getText("t").insert(0, "existing content");
    store.persistMerged(spaceUri, priorState);

    const sync = new DocSync({
      sapWsUrl: "ws://unused.test",
      store,
      pubsub,
      render: (ydoc) => ({
        title: ydoc.getText("t").toString().slice(0, 10) || "Untitled",
        markdown: ydoc.getText("t").toString(),
      }),
    });

    // A new editor session starts from the persisted content (as
    // subscribeDoc would seed it) and sends only its incremental edit.
    const editorDoc = new Y.Doc();
    Y.applyUpdateV2(editorDoc, Y.encodeStateAsUpdateV2(priorState));
    const before = Y.encodeStateVector(editorDoc);
    editorDoc.getText("t").insert("existing content".length, " plus new");
    const incrementalUpdate = Y.diffUpdateV2(
      Y.encodeStateAsUpdateV2(editorDoc),
      before,
    );

    sync.applyEdit(spaceUri, incrementalUpdate);

    expect(store.mergedState(spaceUri)?.getText("t").toString()).toBe(
      "existing content plus new",
    );
  });
});

describe("DocSync republish", () => {
  // Republishing writes the merged snapshot to the owner's own crdt
  // record, which is itself a network.habitat.docs.crdt write — so it
  // comes back through handleOutboxMessage looking like a fresh delta.
  // DocSync itself doesn't guard against that (it republishes
  // unconditionally on every debounced flush); what stops it from looping
  // forever is pear's PutRecord skipping the write — and the notifyWrite
  // it would cause — whenever it doesn't actually change the record's CID
  // (see internal/spaces/store.go's TestPutRecordSkipsNotifyWhenCidUnchanged),
  // so a no-op republish here never produces another outbox event to react
  // to. That's out of reach of a chalk-only test; this just pins that a
  // genuine subsequent change still republishes correctly.
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("republishes again for a genuinely new merged state", async () => {
    call.mockClear();
    uploadBlob.mockClear();
    getBlob.mockClear();

    const store = new DocStore(createTestDb());
    const pubsub = new DocPubSub();
    store.upsertDoc({
      spaceUri: "at://did:plc:owner/space/network.habitat.docs/abc",
      docId: "abc",
      ownerDid: "did:plc:owner",
      title: "Untitled",
    });

    const sync = new DocSync({
      sapWsUrl: "ws://unused.test",
      store,
      pubsub,
      render: (ydoc) => ({
        title: ydoc.getText("t").toString().slice(0, 10) || "Untitled",
        markdown: ydoc.getText("t").toString(),
      }),
    });

    const memberDoc = new Y.Doc();
    memberDoc.getText("t").insert(0, "hello");
    getBlobBytes = Y.encodeStateAsUpdateV2(memberDoc);

    await sync.handleOutboxMessage({
      id: 1,
      uri: "at://did:plc:owner/space/network.habitat.docs/abc/did:plc:member/network.habitat.docs.crdt/self",
      value: { blob: { ref: { $link: "cid1" } } },
    });
    await vi.advanceTimersByTimeAsync(2000); // republish's idle debounce

    expect(uploadBlob).toHaveBeenCalledTimes(1);
    expect(call).toHaveBeenCalledTimes(2); // crdt + markdown putRecord

    // A second, genuinely different edit from the member.
    memberDoc.getText("t").insert(5, " world");
    getBlobBytes = Y.encodeStateAsUpdateV2(memberDoc);

    await sync.handleOutboxMessage({
      id: 2,
      uri: "at://did:plc:owner/space/network.habitat.docs/abc/did:plc:member/network.habitat.docs.crdt/self",
      value: { blob: { ref: { $link: "cid2" } } },
    });
    await vi.advanceTimersByTimeAsync(2000);

    expect(uploadBlob).toHaveBeenCalledTimes(2); // republished again
    expect(call).toHaveBeenCalledTimes(4);
  });
});

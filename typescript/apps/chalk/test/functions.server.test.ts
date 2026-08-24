import os from "node:os";
import path from "node:path";
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import * as Y from "yjs";
import type { DocStore } from "../src/server/docStore";
import type { DebounceQueue } from "../src/server/debounceQueue";

const sessionData: { did?: string } = {};
vi.mock("../src/server/session", () => ({
  useAppSession: vi.fn(async () => ({
    data: sessionData,
    update: vi.fn(async (patch: Record<string, unknown>) =>
      Object.assign(sessionData, patch),
    ),
    clear: vi.fn(async () => {
      delete sessionData.did;
    }),
  })),
}));

const call = vi.fn(async () => ({}));
const uploadBlob = vi.fn(async () => ({ blob: {}, cid: "cid1" }));
// functions.server.ts constructs its own SapClient(memberDid) inside
// memberEditQueue's flush rather than taking one as an injected dependency
// (same reasoning as docSync.ts — see docSync.test.ts), so exercising that
// flush here means mocking the module instead.
vi.mock("../src/server/sapClient", () => ({
  SapClient: vi.fn().mockImplementation(function SapClient() {
    return { call, uploadBlob, getBlob: vi.fn() };
  }),
}));

// functions.server.ts constructs its composition root (sqlite DocStore,
// DocSync's websocket loop) at module-evaluation time, so its env vars must
// be set before the module is imported — a dynamic import after setting
// them, rather than a static import, which ES modules hoist above this
// code. CHALK_SAP_INTERNAL_URL points at a port nothing listens on so
// DocSync's connect attempt fails fast (connection refused) instead of
// hanging on DNS resolution for an unreachable host.
let requireSession: () => Promise<{ did: string }>;
let store: DocStore;
let memberEditQueue: DebounceQueue<string, Uint8Array[]>;
let memberEditKey: (docId: string, memberDid: string) => string;

beforeAll(async () => {
  process.env.CHALK_DB = path.join(
    os.tmpdir(),
    `chalk-functions-test-${process.pid}-${Date.now()}.db`,
  );
  process.env.CHALK_SAP_INTERNAL_URL = "http://127.0.0.1:1";
  ({ requireSession, store, memberEditQueue, memberEditKey } =
    await import("../src/server/functions.server"));
});

describe("requireSession", () => {
  beforeEach(() => {
    delete sessionData.did;
  });

  it("returns the caller when a session DID is set", async () => {
    sessionData.did = "did:plc:member1";
    await expect(requireSession()).resolves.toEqual({ did: "did:plc:member1" });
  });

  it("throws when no session DID is set", async () => {
    await expect(requireSession()).rejects.toThrow();
  });
});

describe("memberEditQueue flush", () => {
  beforeEach(() => {
    call.mockClear();
    uploadBlob.mockClear();
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("uploads a blob that preserves previously-persisted content, not just the queued diffs", async () => {
    const spaceUri = "at://did:plc:owner/space/network.habitat.docs/xyz";
    const memberDid = "did:plc:member2";

    // Content already persisted before this batch of edits — memberEditQueue
    // only receives the *incremental* diffs generated after this point (see
    // useYDoc.ts), so the flush has to seed from this rather than start
    // empty, or the uploaded blob loses everything that predates the batch.
    const priorState = new Y.Doc();
    priorState.getText("t").insert(0, "existing content");
    store.persistMerged(spaceUri, priorState);

    const editorDoc = new Y.Doc();
    Y.applyUpdateV2(editorDoc, Y.encodeStateAsUpdateV2(priorState));
    const before = Y.encodeStateVector(editorDoc);
    editorDoc.getText("t").insert("existing content".length, " plus new");
    const incrementalUpdate = Y.diffUpdateV2(
      Y.encodeStateAsUpdateV2(editorDoc),
      before,
    );

    memberEditQueue.push(memberEditKey(spaceUri, memberDid), [
      incrementalUpdate,
    ]);
    // Past the flush's idle debounce; see MEMBER_EDIT_IDLE_MS in
    // functions.server.ts.
    await vi.advanceTimersByTimeAsync(5000);

    expect(uploadBlob).toHaveBeenCalledTimes(1);
    const uploaded = uploadBlob.mock.calls[0]?.[0] as Uint8Array;
    const result = new Y.Doc();
    Y.applyUpdateV2(result, uploaded);
    expect(result.getText("t").toString()).toBe("existing content plus new");
  });
});

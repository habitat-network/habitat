import { DatabaseSync } from "node:sqlite";
import { describe, expect, it, vi } from "vitest";
import { DocCrdtStore } from "./docCrdtStore";
import {
  DOCS_SPACE_TYPE,
  COMMENT_SPACE_TYPE,
  type PearClient,
} from "./pearClient";

const ORG = "did:web:org.example";
const DOC_ID = "doc1";
const DOC_SPACE = `ats://${ORG}/${DOCS_SPACE_TYPE}/${DOC_ID}`;
const COMMENT_SPACE = `ats://${ORG}/${COMMENT_SPACE_TYPE}/${DOC_ID}`;

function fakePear() {
  return {
    createSpace: vi
      .fn()
      .mockImplementationOnce(async () => ({ uri: DOC_SPACE, skey: DOC_ID }))
      .mockImplementationOnce(async () => ({
        uri: COMMENT_SPACE,
        skey: DOC_ID,
      })),
    putRecord: vi.fn().mockResolvedValue({ uri: `${DOC_SPACE}/x`, cid: "c" }),
    grantRole: vi.fn().mockResolvedValue(undefined),
    writeUsersetTuple: vi.fn().mockResolvedValue(undefined),
  } as unknown as PearClient & {
    createSpace: ReturnType<typeof vi.fn>;
    writeUsersetTuple: ReturnType<typeof vi.fn>;
    grantRole: ReturnType<typeof vi.fn>;
  };
}

describe("DocCrdtStore.createDoc", () => {
  it("creates the doc space, a comment space with the doc's skey, and derived-permission tuples", async () => {
    const pear = fakePear();
    const store = new DocCrdtStore(pear, new DatabaseSync(":memory:"));

    const result = await store.createDoc("did:web:alice", ORG);

    expect(result).toEqual({ uri: DOC_SPACE, docId: DOC_ID });

    // Doc space first (auto skey), then the comment space with the same skey.
    expect(pear.createSpace).toHaveBeenNthCalledWith(1, ORG, DOCS_SPACE_TYPE);
    expect(pear.createSpace).toHaveBeenNthCalledWith(
      2,
      ORG,
      COMMENT_SPACE_TYPE,
      DOC_ID,
    );

    // Userset tuples: doc readers -> comment readers, doc writers -> comment writers.
    expect(pear.writeUsersetTuple).toHaveBeenCalledWith(
      ORG,
      DOC_SPACE,
      "reader",
      "reader",
      COMMENT_SPACE,
    );
    expect(pear.writeUsersetTuple).toHaveBeenCalledWith(
      ORG,
      DOC_SPACE,
      "writer",
      "writer",
      COMMENT_SPACE,
    );

    // Creator still gets owner on the doc space.
    expect(pear.grantRole).toHaveBeenCalledWith(
      ORG,
      DOC_SPACE,
      "did:web:alice",
      "owner",
    );
  });
});

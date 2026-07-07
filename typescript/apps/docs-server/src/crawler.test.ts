import { describe, expect, it } from "vitest";
import { parseSpaceRecordUri } from "./crawler";

describe("parseSpaceRecordUri", () => {
  it("extracts the authoring repo and rkey from a comment record URI", () => {
    const uri =
      "ats://did:web:org/network.habitat.docs.comments/doc1/did:web:alice/network.habitat.docs.comment/c1";
    const parsed = parseSpaceRecordUri(uri);
    expect(parsed).toEqual({
      spaceUri: "ats://did:web:org/network.habitat.docs.comments/doc1",
      owner: "did:web:org",
      type: "network.habitat.docs.comments",
      skey: "doc1",
      repo: "did:web:alice",
      collection: "network.habitat.docs.comment",
      rkey: "c1",
    });
  });

  it("returns undefined for a non-record URI", () => {
    expect(parseSpaceRecordUri("ats://did:web:org/type/skey")).toBeUndefined();
    expect(parseSpaceRecordUri("https://example.com")).toBeUndefined();
  });
});

import { describe, expect, it } from "vitest";
import { parseSpaceRecordUri } from "../src/server/spaceUri";

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

import { describe, expect, it } from "vitest";
import { explainSpaceReaders } from "./spaceAccess";

const members = [
  { did: "did:plc:admin", roles: ["admin", "member"] },
  { did: "did:plc:eng", roles: ["member", "engineering"] },
  { did: "did:plc:guest", roles: [] },
];

describe("explainSpaceReaders", () => {
  it("lists the members holding a role on the access record, with the roles that grant it", () => {
    expect(explainSpaceReaders(["engineering", "admin"], members)).toEqual([
      { did: "did:plc:admin", via: ["admin"] },
      { did: "did:plc:eng", via: ["engineering"] },
    ]);
  });

  it("names every matching role", () => {
    expect(explainSpaceReaders(["admin", "member"], members)[0]).toEqual({
      did: "did:plc:admin",
      via: ["admin", "member"],
    });
  });

  it("has no readers without an access record or with an empty one", () => {
    expect(explainSpaceReaders(null, members)).toEqual([]);
    expect(explainSpaceReaders([], members)).toEqual([]);
  });
});

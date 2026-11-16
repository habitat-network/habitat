import { describe, expect, it } from "vitest";
import { actorLabel } from "./Actor";

describe("actorLabel", () => {
  const did = "did:web:hello.test";

  it("prefers the display name", () => {
    expect(actorLabel({ did, handle: "a.test", displayName: "Alice" })).toBe(
      "Alice",
    );
  });

  it("falls back to the handle", () => {
    expect(actorLabel({ did, handle: "a.test" })).toBe("a.test");
    expect(actorLabel({ did, handle: "a.test", displayName: "  " })).toBe(
      "a.test",
    );
  });

  it("falls back to the DID", () => {
    expect(actorLabel({ did })).toBe(did);
    expect(actorLabel({ did, handle: "", displayName: "" })).toBe(did);
  });
});

import { describe, expect, it } from "vitest";
import { readRememberedLogin } from "./rememberedLogin";

// Builds a cookie value the way pear does: base64url(JSON) + "." + signature.
function cookieFor(payload: object): string {
  const b64 = btoa(JSON.stringify(payload))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
  return `habitat_login_hint=${b64}.sig`;
}

describe("readRememberedLogin", () => {
  it("decodes the DID and handle", () => {
    const cookies = `other=1; ${cookieFor({ did: "did:plc:abc", handle: "alice.test", iat: 1 })}`;
    expect(readRememberedLogin(cookies)).toEqual({
      did: "did:plc:abc",
      handle: "alice.test",
    });
  });

  it("ignores a missing or malformed cookie", () => {
    expect(readRememberedLogin("other=1")).toBeUndefined();
    expect(readRememberedLogin("habitat_login_hint=%%%.sig")).toBeUndefined();
    expect(
      readRememberedLogin(cookieFor({ did: "not-a-did" })),
    ).toBeUndefined();
  });
});

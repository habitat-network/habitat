import { XrpcError } from "@atproto/lex";
import { describe, expect, it } from "vitest";
import {
  HabitatError,
  NotAMember,
  SessionExpired,
  userMessage,
} from "@/server/effect/errors";
import { toHabitatError } from "@/server/effect/services";

const MEMBER = "did:web:alice.example";

describe("toHabitatError", () => {
  it("passes the typed errors through", () => {
    const expired = new SessionExpired({
      did: "did:web:org.example",
      message: "no session",
    });
    expect(toHabitatError(MEMBER, "x.y.z", expired)).toBe(expired);
  });

  it("reports a 401 from a group call as a refusal, not an expired session", () => {
    // Pear answers a denied action with 401 and the proxy strips the body.
    const denied = Object.assign(Object.create(XrpcError.prototype), {
      status: 401,
      error: "AuthenticationRequired",
      message: "Upstream server responded with a 401 error",
    });
    const nsid = "community.opensocial.ejectMember";
    expect(toHabitatError(MEMBER, nsid, denied)).toBeInstanceOf(SessionExpired);
    const proxied = toHabitatError(MEMBER, nsid, denied, true);
    expect(proxied).toBeInstanceOf(HabitatError);
    expect(proxied).toMatchObject({ status: 403, nsid });
  });

  it("uses status 0 when the request never got an answer", () => {
    const error = toHabitatError(
      MEMBER,
      "x.y.z",
      new TypeError("fetch failed"),
    );
    expect(error).toMatchObject({ _tag: "HabitatError", status: 0 });
  });
});

describe("userMessage", () => {
  it("passes Habitat's own message through", () => {
    expect(
      userMessage(
        new HabitatError({
          nsid: "a.b.c",
          status: 403,
          message: "Not allowed",
        }),
      ),
    ).toBe("Not allowed");
  });

  it("shows pear's explanation of a rejected request, not the raw response", () => {
    const rejected = new HabitatError({
      nsid: "network.habitat.mcp.addServer",
      status: 400,
      message: "network.habitat.mcp.addServer failed (400): {...}",
      detail: "url must be an absolute http or https URL: \"nope\"",
    });
    expect(userMessage(rejected)).toBe(
      "url must be an absolute http or https URL: \"nope\"",
    );
    expect(userMessage(new HabitatError({
          nsid: rejected.nsid,
          status: 500,
          message: rejected.message,
          detail: "internal detail",
        }))).toBe(
      rejected.message,
    );
  });

  it("doesn't say whether a group the member isn't in exists", () => {
    expect(userMessage(new NotAMember({ org: "did:web:org.example" }))).toBe(
      "You aren't a member of that group",
    );
  });
});

import { describe, expect, it } from "vitest";
import {
  HabitatError,
  NotAMember,
  SessionExpired,
  userMessage,
} from "@/server/effect/errors";
import { toHabitatError } from "@/server/effect/services";
import { UnauthorizedError } from "@/server/habitatClient";

const MEMBER = "did:web:alice.example";

describe("toHabitatError", () => {
  it("treats an UnauthorizedError anywhere in the cause chain as an expired session", () => {
    const cause = new Error("wrapped", {
      cause: new UnauthorizedError("did:web:org.example", "no session"),
    });
    const error = toHabitatError(
      MEMBER,
      "network.habitat.space.listSpaces",
      cause,
    );
    expect(error).toBeInstanceOf(SessionExpired);
    expect(error).toMatchObject({ did: "did:web:org.example" });
  });

  it("reads the status out of a HabitatClient failure message", () => {
    const error = toHabitatError(
      MEMBER,
      "network.habitat.repo.uploadBlob",
      new Error("network.habitat.repo.uploadBlob failed (413): too large"),
    );
    expect(error).toBeInstanceOf(HabitatError);
    expect(error).toMatchObject({
      status: 413,
      nsid: "network.habitat.repo.uploadBlob",
    });
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

  it("doesn't say whether a group the member isn't in exists", () => {
    expect(userMessage(new NotAMember({ org: "did:web:org.example" }))).toBe(
      "You aren't a member of that group",
    );
  });
});

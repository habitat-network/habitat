import { describe, expect, it } from "vitest";

import { HabitatIdentityResolverError } from "./errors.js";

describe("HabitatIdentityResolverError", () => {
  it("is an Error with a distinguishable name", () => {
    const error = new HabitatIdentityResolverError("boom");

    expect(error).toBeInstanceOf(Error);
    expect(error).toBeInstanceOf(HabitatIdentityResolverError);
    expect(error.name).toBe("HabitatIdentityResolverError");
    expect(error.message).toBe("boom");
  });

  it("defaults status and xrpcError to undefined", () => {
    const error = new HabitatIdentityResolverError("boom");

    expect(error.status).toBeUndefined();
    expect(error.xrpcError).toBeUndefined();
  });

  it("carries status and xrpcError when provided", () => {
    const error = new HabitatIdentityResolverError("not found", {
      status: 404,
      xrpcError: "DidNotFound",
    });

    expect(error.status).toBe(404);
    expect(error.xrpcError).toBe("DidNotFound");
  });

  it("preserves the underlying cause", () => {
    const cause = new TypeError("fetch failed");
    const error = new HabitatIdentityResolverError("unreachable", { cause });

    expect(error.cause).toBe(cause);
  });
});

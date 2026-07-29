import { describe, expect, it } from "vitest";

import type { IdentityResolver } from "@atproto-labs/identity-resolver";

import {
  DEFAULT_HABITAT_SERVICE_URL,
  HabitatIdentityResolver,
  HabitatIdentityResolverError,
} from "./index.js";

describe("package entry point", () => {
  it("exports the default Habitat service URL", () => {
    expect(DEFAULT_HABITAT_SERVICE_URL).toBe("https://pear.habitat.network");
  });

  it("exports a resolver assignable to IdentityResolver", () => {
    const resolver: IdentityResolver = new HabitatIdentityResolver();

    expect(resolver).toBeInstanceOf(HabitatIdentityResolver);
    expect(typeof resolver.resolve).toBe("function");
  });

  it("exports the error type", () => {
    expect(new HabitatIdentityResolverError("boom")).toBeInstanceOf(Error);
  });

  it("does not leak internal handle helpers", async () => {
    const entry: Record<string, unknown> = await import("./index.js");

    expect(entry.normalizeHandle).toBeUndefined();
    expect(entry.HANDLE_INVALID).toBeUndefined();
  });
});

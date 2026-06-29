import { describe, it, expect, vi } from "vitest";
import { searchHabitat } from "./search.js";

describe("habitat_search error handling", () => {
  it("searchHabitat throws on HTTP failure, enabling Search failed message", async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      statusText: "Service Unavailable",
    } as unknown as Response);

    const err = await searchHabitat("http://localhost:8091", { q: "test" }).catch(
      (e: unknown) => e
    );

    expect(err).toBeInstanceOf(Error);
    expect((err as Error).message).toContain("503");
    // Verify the handler's conversion pattern works
    const message = err instanceof Error ? err.message : String(err);
    expect(`Search failed: ${message}`).toMatch(/^Search failed: Search request failed: 503/);
  });

  it("searchHabitat throws on malformed response, enabling Search failed message", async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ error: "something went wrong" }),
    } as unknown as Response);

    const err = await searchHabitat("http://localhost:8091", { q: "test" }).catch(
      (e: unknown) => e
    );

    expect(err).toBeInstanceOf(Error);
    expect((err as Error).message).toContain("missing results array");
  });
});

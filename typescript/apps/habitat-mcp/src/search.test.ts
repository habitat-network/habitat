import { describe, it, expect, vi, beforeEach } from "vitest";
import { searchHabitat } from "./search.js";

describe("searchHabitat", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("formats results with record type, uri, and snippet", async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          results: [
            {
              uri: "at://did:web:org.example/network.habitat.doc/abc123",
              recordType: "network.habitat.doc",
              snippet: "relevant excerpt from the record",
            },
          ],
          cursor: "next-page-token",
        }),
    } as unknown as Response);

    const result = await searchHabitat("http://localhost:8091", { q: "test" });

    expect(result).toContain("Found 1 results.");
    expect(result).toContain("network.habitat.doc");
    expect(result).toContain("at://did:web:org.example/network.habitat.doc/abc123");
    expect(result).toContain('"relevant excerpt from the record"');
    expect(result).toContain("Next cursor: next-page-token");
  });

  it("returns no-results message when results array is empty", async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ results: [] }),
    } as unknown as Response);

    const result = await searchHabitat("http://localhost:8091", { q: "nothing" });

    expect(result).toBe('No results found for "nothing".');
  });

  it("passes q, limit, and cursor as query params", async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ results: [] }),
    } as unknown as Response);

    await searchHabitat("http://localhost:8091", {
      q: "hello",
      limit: 10,
      cursor: "abc",
    });

    const calledUrl = (global.fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
    expect(calledUrl).toContain("q=hello");
    expect(calledUrl).toContain("limit=10");
    expect(calledUrl).toContain("cursor=abc");
  });

  it("throws when HTTP response is not ok", async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      statusText: "Internal Server Error",
    } as unknown as Response);

    await expect(
      searchHabitat("http://localhost:8091", { q: "test" })
    ).rejects.toThrow("Search request failed: 500 Internal Server Error");
  });
});

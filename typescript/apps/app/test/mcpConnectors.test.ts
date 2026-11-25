import { describe, expect, it } from "vitest";
import { serverUrlError } from "@/lib/mcpConnectors";

describe("serverUrlError", () => {
  it("accepts absolute http and https URLs", () => {
    expect(serverUrlError("https://example.com/mcp")).toBeNull();
    expect(serverUrlError(" http://localhost:8080/mcp ")).toBeNull();
  });

  it("rejects text that isn't a URL", () => {
    expect(serverUrlError("not-a-url")).toMatch(/full URL/);
  });

  it("rejects other schemes", () => {
    expect(serverUrlError("ftp://example.com/mcp")).toMatch(/https:\/\//);
    expect(serverUrlError("javascript:alert(1)")).toMatch(/https:\/\//);
  });
});

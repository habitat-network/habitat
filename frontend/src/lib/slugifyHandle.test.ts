import { describe, expect, it } from "vitest";
import { slugifyHandle } from "./slugifyHandle";

describe("slugifyHandle", () => {
  it("lowercases and strips whitespace", () => {
    expect(slugifyHandle("Acme Corp")).toBe("acmecorp");
  });

  it("strips punctuation and symbols", () => {
    expect(slugifyHandle("Acme Corp!! (2024)")).toBe("acmecorp2024");
  });

  it("strips unicode/accented characters down to plain alphanumerics", () => {
    expect(slugifyHandle("Café")).toBe("caf");
  });

  it("truncates to 50 characters to satisfy the backend handle regex", () => {
    const longName = "a".repeat(60);
    expect(slugifyHandle(longName)).toHaveLength(50);
  });

  it("returns an empty string for input with no alphanumeric characters", () => {
    expect(slugifyHandle("!!! ---")).toBe("");
  });
});

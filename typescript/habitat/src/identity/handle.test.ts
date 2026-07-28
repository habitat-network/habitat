import { describe, expect, it } from "vitest";

import { HANDLE_INVALID, normalizeHandle } from "./handle.js";

describe("normalizeHandle", () => {
  it("lowercases a valid handle", () => {
    expect(normalizeHandle("Alice.Example.Com")).toBe("alice.example.com");
  });

  it("passes a Habitat handle through unchanged", () => {
    expect(normalizeHandle("acme.habitat.network")).toBe(
      "acme.habitat.network",
    );
  });

  it("passes handle.invalid through unchanged", () => {
    expect(normalizeHandle(HANDLE_INVALID)).toBe(HANDLE_INVALID);
  });

  it("rejects a single-segment handle", () => {
    expect(normalizeHandle("NotAHandle")).toBe(HANDLE_INVALID);
  });

  it("rejects an empty handle", () => {
    expect(normalizeHandle("")).toBe(HANDLE_INVALID);
  });

  it("rejects a segment starting with a hyphen", () => {
    expect(normalizeHandle("-bad.example.com")).toBe(HANDLE_INVALID);
  });

  it("rejects a segment ending with a hyphen", () => {
    expect(normalizeHandle("bad-.example.com")).toBe(HANDLE_INVALID);
  });

  it("rejects a numeric top-level segment", () => {
    expect(normalizeHandle("alice.example.123")).toBe(HANDLE_INVALID);
  });

  it("rejects characters outside the handle grammar", () => {
    expect(normalizeHandle("did:web:acme.habitat.network")).toBe(
      HANDLE_INVALID,
    );
    expect(normalizeHandle("alice_smith.example.com")).toBe(HANDLE_INVALID);
  });

  it("rejects a handle longer than 253 characters", () => {
    const tooLong = `${"a".repeat(60)}.${"b".repeat(60)}.${"c".repeat(60)}.${"d".repeat(60)}.example.com`;

    expect(tooLong.length).toBeGreaterThan(253);
    expect(normalizeHandle(tooLong)).toBe(HANDLE_INVALID);
  });
});

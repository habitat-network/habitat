import { beforeEach, describe, expect, it, vi } from "vitest";

const sessionData: { did?: string } = {};
vi.mock("../src/server/session", () => ({
  useAppSession: vi.fn(async () => ({
    data: sessionData,
    update: vi.fn(async (patch: Record<string, unknown>) =>
      Object.assign(sessionData, patch),
    ),
    clear: vi.fn(async () => {
      delete sessionData.did;
    }),
  })),
}));

const { requireSession } = await import("../src/server/functions.server");

describe("requireSession", () => {
  beforeEach(() => {
    delete sessionData.did;
  });

  it("returns the caller when a session DID is set", async () => {
    sessionData.did = "did:plc:member1";
    await expect(requireSession()).resolves.toEqual({ did: "did:plc:member1" });
  });

  it("throws when no session DID is set", async () => {
    await expect(requireSession()).rejects.toThrow();
  });
});

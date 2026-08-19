import os from "node:os";
import path from "node:path";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

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

vi.mock("@tanstack/react-start", () => ({
  createServerFn: () => ({
    validator: () => ({
      handler: (fn: (...args: unknown[]) => unknown) => fn,
    }),
    handler: (fn: (...args: unknown[]) => unknown) => fn,
  }),
}));

// functions.ts constructs its composition root (sqlite DocStore, DocSync's
// websocket loop) at module-evaluation time, so its env vars must be set
// before the module is imported — a dynamic import after setting them,
// rather than a static import, which ES modules hoist above this code.
// CHALK_SAP_INTERNAL_URL points at a port nothing listens on so DocSync's
// connect attempt fails fast (connection refused) instead of hanging on
// DNS resolution for an unreachable host.
let requireSession: () => Promise<{ did: string }>;

beforeAll(async () => {
  process.env.CHALK_DB = path.join(
    os.tmpdir(),
    `chalk-functions-test-${process.pid}-${Date.now()}.db`,
  );
  process.env.CHALK_SAP_INTERNAL_URL = "http://127.0.0.1:1";
  ({ requireSession } = await import("../src/server/functions"));
});

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

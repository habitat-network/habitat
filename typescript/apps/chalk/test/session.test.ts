import { describe, expect, it, vi } from "vitest";

const updateMock = vi.fn();
const clearMock = vi.fn();
const useSessionMock = vi.fn(async () => ({
  data: { did: "did:plc:existing" },
  update: updateMock,
  clear: clearMock,
}));

vi.mock("@tanstack/react-start/server", () => ({
  useSession: useSessionMock,
}));

describe("useAppSession", () => {
  it("configures useSession with the chalk cookie name and env secret", async () => {
    process.env.CHALK_SESSION_SECRET = "test-secret-at-least-32-characters!!";
    const { useAppSession } = await import("../src/server/session");
    const session = await useAppSession();

    expect(useSessionMock).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "chalk_session",
        password: "test-secret-at-least-32-characters!!",
        cookie: expect.objectContaining({
          httpOnly: true,
          sameSite: "lax",
        }),
      }),
    );
    expect(session.data.did).toBe("did:plc:existing");
  });

  it("throws if CHALK_SESSION_SECRET is unset", async () => {
    delete process.env.CHALK_SESSION_SECRET;
    vi.resetModules();
    const { useAppSession } = await import("../src/server/session");
    await expect(useAppSession()).rejects.toThrow(/CHALK_SESSION_SECRET/);
  });
});

import { describe, expect, it, vi } from "vitest";

const saveMock = vi.fn();
const destroyMock = vi.fn();
const getIronSessionMock = vi.fn(async () => ({
  did: "did:plc:existing",
  save: saveMock,
  destroy: destroyMock,
}));

vi.mock("iron-session", () => ({
  getIronSession: getIronSessionMock,
}));

vi.mock("@tanstack/react-start/server", () => ({
  getCookie: vi.fn(),
  setCookie: vi.fn(),
}));

describe("useAppSession", () => {
  it("configures iron-session with the chalk cookie name and env secret", async () => {
    process.env.CHALK_SESSION_SECRET = "test-secret-at-least-32-characters!!";
    const { useAppSession } = await import("../src/server/session");
    const session = await useAppSession();

    expect(getIronSessionMock).toHaveBeenCalledWith(
      expect.objectContaining({ read: expect.any(Function) }),
      expect.objectContaining({
        cookieName: "chalk_session",
        password: "test-secret-at-least-32-characters!!",
        cookieOptions: expect.objectContaining({
          httpOnly: true,
          sameSite: "lax",
        }),
      }),
    );
    expect(session.data.did).toBe("did:plc:existing");

    await session.update({ currentOrg: "did:plc:org" });
    expect(saveMock).toHaveBeenCalled();
    expect(session.data.currentOrg).toBe("did:plc:org");
    await session.clear();
    expect(destroyMock).toHaveBeenCalled();
  });

  it("throws if CHALK_SESSION_SECRET is unset", async () => {
    delete process.env.CHALK_SESSION_SECRET;
    vi.resetModules();
    const { useAppSession } = await import("../src/server/session");
    await expect(useAppSession()).rejects.toThrow(/CHALK_SESSION_SECRET/);
  });
});

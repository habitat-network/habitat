import { beforeEach, describe, expect, it, vi } from "vitest";
import { xrpc } from "@atproto/lex";
import type { DidString } from "@atproto/syntax";
import { network } from "api";

const sessionData: { did?: string } = {};
vi.mock("../src/server/session", () => ({
  useAppSession: vi.fn(async () => ({
    data: sessionData,
    clear: vi.fn(async () => {
      delete sessionData.did;
    }),
  })),
}));

// restore() hands back a session whose every call pear answers with
// `status`, or fails outright when `restoreFails` is set.
const pear = { status: 200, restoreFails: false };
vi.mock("../src/server/oauth", () => ({
  createOAuthClient: () => ({
    restore: async (did: string) => {
      if (pear.restoreFails) throw new Error("refresh token expired");
      return {
        did,
        fetchHandler: async () =>
          Response.json({ spaces: [] }, { status: pear.status }),
      };
    },
  }),
}));

const { memberSession, signOutIfUnauthorized } =
  await import("../src/server/functions.server");

const MEMBER = "did:plc:member1" as DidString;
const testEnv = {} as Env;

const listSpaces = async () => {
  const agent = await memberSession(testEnv, MEMBER);
  return xrpc(agent, network.habitat.space.listSpaces.main, { params: {} });
};

describe("memberSession", () => {
  beforeEach(() => {
    sessionData.did = MEMBER;
    pear.status = 200;
    pear.restoreFails = false;
  });

  it("calls through the member's OAuth session", async () => {
    const { body } = await listSpaces();
    expect(body.spaces).toEqual([]);
  });

  it("signs out when pear rejects the member's session with a 401", async () => {
    pear.status = 401;
    const err = await listSpaces().catch((e: unknown) => e);
    await expect(signOutIfUnauthorized(err)).rejects.toMatchObject({
      options: { to: "/login" },
    });
    expect(sessionData.did).toBeUndefined();
  });

  it("signs out when the session can't be restored", async () => {
    pear.restoreFails = true;
    await expect(memberSession(testEnv, MEMBER)).rejects.toMatchObject({
      options: { to: "/login" },
    });
    expect(sessionData.did).toBeUndefined();
  });

  it("leaves the login alone for other failures", async () => {
    pear.status = 500;
    const err = await listSpaces().catch((e: unknown) => e);
    await expect(signOutIfUnauthorized(err)).resolves.toBeUndefined();
    expect(sessionData.did).toBe(MEMBER);
  });
});

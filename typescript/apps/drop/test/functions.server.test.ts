import { env } from "cloudflare:workers";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { xrpc } from "@atproto/lex";
import type { DidString } from "@atproto/syntax";
import { network } from "api";
import { getDb } from "@/db";
import { connectedOrgs } from "@/db/schema";

const sessionData: { did?: string; currentOrg?: string } = {};
vi.mock("../src/server/session", () => ({
  useAppSession: vi.fn(async () => ({
    data: sessionData,
    clear: vi.fn(async () => {
      delete sessionData.did;
    }),
    update: vi.fn(async (patch: { currentOrg?: string }) => {
      Object.assign(sessionData, patch);
    }),
  })),
}));

// restore() hands back a session whose every call pear answers with
// `status`, or fails outright when `restoreFails` is set.
const pear = { status: 200, restoreFails: false };
vi.mock("../src/server/oauth", () => {
  class OrgSessionExpiredError extends Error {
    constructor(
      readonly orgDid: string,
      message: string,
    ) {
      super(message);
    }
  }
  return {
    OrgSessionExpiredError,
    findOrgSessionExpired: (err: unknown) => {
      for (let e = err; e instanceof Error; e = e.cause) {
        if (e instanceof OrgSessionExpiredError) return e;
      }
      return undefined;
    },
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
  };
});

const { memberSession, signOutIfUnauthorized, disconnectIfOrgSessionExpired } =
  await import("../src/server/functions.server");
const { OrgSessionExpiredError } = await import("../src/server/oauth");

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

describe("disconnectIfOrgSessionExpired", () => {
  const ORG = "did:web:org.example";
  const db = getDb(env);

  beforeEach(async () => {
    sessionData.did = MEMBER;
    sessionData.currentOrg = ORG;
    await db.delete(connectedOrgs);
    await db.insert(connectedOrgs).values({
      orgDid: ORG,
      name: "Org",
      connectedBy: "did:plc:admin",
      connectedAt: 1,
    });
  });

  it("disconnects the org and sends the member to the org picker", async () => {
    const err = new Error("wrapped", {
      cause: new OrgSessionExpiredError(ORG, "401"),
    });
    await expect(disconnectIfOrgSessionExpired(env, err)).rejects.toMatchObject(
      { options: { to: "/orgs" } },
    );
    expect(await db.select().from(connectedOrgs)).toEqual([]);
    expect(sessionData.currentOrg).toBeUndefined();
    // The member stays signed in.
    expect(sessionData.did).toBe(MEMBER);
  });

  it("returns for other failures, leaving the org connected", async () => {
    await expect(
      disconnectIfOrgSessionExpired(env, new Error("boom")),
    ).resolves.toBeUndefined();
    expect(await db.select().from(connectedOrgs)).toHaveLength(1);
    expect(sessionData.currentOrg).toBe(ORG);
  });
});

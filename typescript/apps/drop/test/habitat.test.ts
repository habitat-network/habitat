import type { Agent } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { describe, expect, it } from "vitest";
import { deleteFileRecord, fetchOrgProfiles } from "@/server/habitat";
import { FILE_COLLECTION } from "@/server/sink";

const ORG = "did:web:org.example" as DidString;
const SPACE = `at://${ORG}/space/network.habitat.drop/3kfile` as SpaceRefString;

describe("deleteFileRecord", () => {
  it("deletes the file record from the org's repo in the file's space", async () => {
    const calls: { path: string; body: unknown }[] = [];
    const agent = {
      did: ORG,
      fetchHandler: async (path: string, init: RequestInit) => {
        calls.push({ path, body: JSON.parse(init.body as string) });
        return Response.json({});
      },
    } as unknown as Agent;

    await deleteFileRecord(agent, ORG, SPACE);

    expect(calls).toEqual([
      {
        path: "/xrpc/com.atproto.space.deleteRecord",
        body: {
          space: SPACE,
          repo: ORG,
          collection: FILE_COLLECTION,
          rkey: "self",
        },
      },
    ]);
  });

  it("surfaces a failed delete", async () => {
    const agent = {
      did: ORG,
      fetchHandler: async () =>
        Response.json({ error: "SpaceNotFound" }, { status: 400 }),
    } as unknown as Agent;
    await expect(deleteFileRecord(agent, ORG, SPACE)).rejects.toThrow();
  });
});

describe("fetchOrgProfiles", () => {
  it("asks the org's Habitat service for the dids' profiles, in chunks of 100", async () => {
    const calls: { url: URL; proxy: string | null }[] = [];
    const agent = {
      did: "did:plc:member",
      fetchHandler: async (path: string, init: RequestInit) => {
        const url = new URL(path, "http://pds.test");
        calls.push({
          url,
          proxy: new Headers(init.headers).get("atproto-proxy"),
        });
        return Response.json({
          profiles: url.searchParams.getAll("dids").map((did) => ({
            did,
            handle: `${did.slice(-3)}.test`,
            displayName: "Name",
            avatarUrl: "https://cdn.test/a.jpg",
          })),
        });
      },
    } as unknown as Agent;
    const dids = Array.from(
      { length: 150 },
      (_, i) => `did:plc:u${String(i).padStart(3, "0")}`,
    );

    const profiles = await fetchOrgProfiles(agent, ORG, [...dids, dids[0]]);

    expect(calls.map((c) => c.url.pathname)).toEqual([
      "/xrpc/network.habitat.opensocial.getProfiles",
      "/xrpc/network.habitat.opensocial.getProfiles",
    ]);
    expect(calls.every((c) => c.proxy === `${ORG}#habitat`)).toBe(true);
    expect(calls.map((c) => c.url.searchParams.getAll("dids").length)).toEqual([
      100, 50,
    ]);
    expect(calls[0].url.searchParams.get("org")).toBe(ORG);
    expect(profiles).toHaveLength(150);
    expect(profiles[0]).toEqual({
      did: dids[0],
      handle: "000.test",
      displayName: "Name",
      avatar: "https://cdn.test/a.jpg",
    });
  });
});

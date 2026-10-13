import type { Agent } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { describe, expect, it } from "vitest";
import { deleteFileRecord } from "@/server/habitat";
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

import type { DidString, SpaceRefString } from "@atproto/syntax";
import { describe, expect, it, vi } from "vitest";

const { deleteRecordIndexed } = vi.hoisted(() => ({
  deleteRecordIndexed: vi.fn(),
}));
vi.mock("../src/server/stream", () => ({
  deleteRecordIndexed,
  putRecordIndexed: vi.fn(),
}));

import { deleteFileRecord } from "@/server/files/files.server";
import { FILE_COLLECTION } from "@/server/files/sink";

const ORG = "did:web:org.example" as DidString;
const SPACE = `at://${ORG}/space/network.habitat.drop/3kfile` as SpaceRefString;

describe("deleteFileRecord", () => {
  it("deletes the file record from the org's repo through the group's stream", async () => {
    deleteRecordIndexed.mockResolvedValueOnce({
      uri: "",
      cid: "",
      indexed: true,
    });
    await deleteFileRecord({} as Env, ORG, SPACE);
    expect(deleteRecordIndexed).toHaveBeenCalledWith(
      expect.anything(),
      ORG,
      ORG,
      {
        space: SPACE,
        repo: ORG,
        collection: FILE_COLLECTION,
        rkey: "self",
      },
    );
  });

  it("surfaces a failed delete", async () => {
    deleteRecordIndexed.mockRejectedValueOnce(new Error("SpaceNotFound"));
    await expect(deleteFileRecord({} as Env, ORG, SPACE)).rejects.toThrow();
  });
});

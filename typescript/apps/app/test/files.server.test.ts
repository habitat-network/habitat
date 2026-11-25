import type { DidString, SpaceRefString } from "@atproto/syntax";
import { Effect } from "effect";
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
const SPACE = `at:space/${ORG}/network.habitat.drop/3kfile` as SpaceRefString;

describe("deleteFileRecord", () => {
  it("deletes the file record from the org's repo through the group's stream", async () => {
    deleteRecordIndexed.mockReturnValueOnce(Effect.succeed({ indexed: true }));
    await Effect.runPromise(deleteFileRecord(ORG, SPACE));
    expect(deleteRecordIndexed).toHaveBeenCalledWith(ORG, ORG, {
      space: SPACE,
      repo: ORG,
      collection: FILE_COLLECTION,
      rkey: "self",
    });
  });

  it("surfaces a failed delete", async () => {
    deleteRecordIndexed.mockReturnValueOnce(
      Effect.fail(new Error("SpaceNotFound")),
    );
    await expect(
      Effect.runPromise(deleteFileRecord(ORG, SPACE)),
    ).rejects.toThrow();
  });
});

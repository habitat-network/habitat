import type { DidString, SpaceRefString } from "@atproto/syntax";
import { Effect } from "effect";
import { describe, expect, it, vi } from "vitest";

const { putRecordIndexed, deleteRecordIndexed } = vi.hoisted(() => ({
  putRecordIndexed: vi.fn(),
  deleteRecordIndexed: vi.fn(),
}));
vi.mock("../src/server/stream", () => ({
  putRecordIndexed,
  deleteRecordIndexed,
}));

import {
  decodeBundle,
  deleteAppRecord,
  encodeBundle,
  putAppRecord,
} from "@/server/apps/apps.server";
import { APP_COLLECTION } from "@/server/apps/sink";

const ORG = "did:web:org.example" as DidString;
const SPACE = `at:space/${ORG}/network.habitat.apps/3kapp` as SpaceRefString;
const BLOB = {
  $type: "blob",
  ref: { $link: "bafkcode" },
  mimeType: "application/json",
  size: 10,
};

describe("app bundles", () => {
  it("round-trip, and the same upload always makes the same bytes", () => {
    const a = encodeBundle({ modules: { "b.js": "2", "a.js": "1" } });
    const b = encodeBundle({ modules: { "a.js": "1", "b.js": "2" } });
    expect(a).toEqual(b);
    expect(decodeBundle(a)).toEqual({ modules: { "a.js": "1", "b.js": "2" } });
  });

  it("refuse malformed bytes", () => {
    const enc = (v: unknown) => new TextEncoder().encode(JSON.stringify(v));
    expect(() => decodeBundle(enc({}))).toThrow();
    expect(() => decodeBundle(enc({ modules: { "a.js": 1 } }))).toThrow();
  });
});

describe("putAppRecord", () => {
  it("writes the app's record into the group's repo through its stream", async () => {
    putRecordIndexed.mockReturnValueOnce(
      Effect.succeed({ uri: "u", cid: "c", indexed: true }),
    );
    await Effect.runPromise(
      putAppRecord(ORG, SPACE, {
        name: "Chat",
        slug: "chat",
        entrypoint: "index.js",
        code: BLOB,
        version: 2,
        compatibilityDate: "2026-08-01",
        createdBy: "did:plc:alice",
        createdAt: "2026-11-01T00:00:00.000Z",
        updatedBy: "did:plc:bob" as DidString,
      }),
    );
    expect(putRecordIndexed).toHaveBeenCalledWith(ORG, ORG, {
      space: SPACE,
      repo: ORG,
      collection: APP_COLLECTION,
      rkey: "self",
      record: expect.objectContaining({
        $type: APP_COLLECTION,
        name: "Chat",
        slug: "chat",
        code: BLOB,
        version: 2,
        createdBy: "did:plc:alice",
        updatedBy: "did:plc:bob",
        createdAt: "2026-11-01T00:00:00.000Z",
        updatedAt: expect.any(String),
      }),
    });
  });

  it("deletes the record to take an app down", async () => {
    deleteRecordIndexed.mockReturnValueOnce(Effect.succeed({ indexed: true }));
    await Effect.runPromise(deleteAppRecord(ORG, SPACE));
    expect(deleteRecordIndexed).toHaveBeenCalledWith(ORG, ORG, {
      space: SPACE,
      repo: ORG,
      collection: APP_COLLECTION,
      rkey: "self",
    });
  });
});

// @vitest-environment node
import { it } from "@effect/vitest";
import { Effect, Exit } from "effect";
import { describe, expect } from "vitest";
import { parseSpaceRef } from "./wire";

const SPACE = "at://did:plc:alice/space/com.example.board/main";

describe("parseSpaceRef", () => {
  it.effect("parses a space ref into its authority, type and skey", () =>
    Effect.gen(function* () {
      const ref = yield* parseSpaceRef(SPACE);
      expect([ref.spaceDid, ref.spaceType, ref.skey]).toEqual([
        "did:plc:alice",
        "com.example.board",
        "main",
      ]);
    }),
  );

  it.effect("rejects record URIs and public repo URIs", () =>
    Effect.gen(function* () {
      for (const bad of [
        `${SPACE}/did:plc:bob/com.example.post/1`,
        "at://did:plc:alice/com.example.post/1",
        "https://example.com",
      ]) {
        const exit = yield* Effect.exit(parseSpaceRef(bad));
        expect(Exit.isFailure(exit)).toBe(true);
      }
    }),
  );
});

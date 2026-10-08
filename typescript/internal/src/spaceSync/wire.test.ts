// @vitest-environment node
import { it } from "@effect/vitest";
import { toBase64 } from "@atproto/lex";
import { com } from "api";
import { Effect, Exit } from "effect";
import { describe, expect } from "vitest";
import { decodeLex, parseSpaceRef } from "./wire";

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

describe("wire decoding", () => {
  it.effect("decodes listRepoOps JSON with $bytes into a signed commit", () =>
    Effect.gen(function* () {
      const bytes = (n: number) => ({
        $bytes: toBase64(new Uint8Array(32).fill(n)),
      });
      const json = {
        ops: [
          {
            rev: "3jzfcijpj2z2a",
            collection: "com.example.post",
            rkey: "1",
            cid: null,
            prev: "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm",
          },
        ],
        commit: {
          ver: 1,
          hash: bytes(1),
          ikm: bytes(2),
          sig: bytes(3),
          mac: bytes(4),
          rev: "3jzfcijpj2z2a",
        },
      };
      const out = yield* decodeLex(
        com.atproto.space.listRepoOps.$output.schema,
      )(json);
      expect(out.commit?.hash).toBeInstanceOf(Uint8Array);
      expect(out.commit?.hash[0]).toBe(1);
      expect(out.ops[0].cid).toBeNull();
      expect(out.cursor).toBeUndefined();
    }),
  );

  it.effect("decodes a forwarded notifyWrite body", () =>
    Effect.gen(function* () {
      const body = {
        space: SPACE,
        repo: "did:plc:bob",
        repoRev: "3jzfcijpj2z2a",
        hash: { $bytes: toBase64(new Uint8Array(32)) },
        spaceRev: "3jzfcijpj2z2b",
      };
      const input = yield* decodeLex(
        com.atproto.space.notifyWrite.$input.schema,
      )(body);
      expect(input.spaceRev).toBe("3jzfcijpj2z2b");
      expect(input.prevSpaceRev).toBeUndefined();
      expect(input.hash).toBeInstanceOf(Uint8Array);
    }),
  );
});

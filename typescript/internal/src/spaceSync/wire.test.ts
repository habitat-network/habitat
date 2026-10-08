// @vitest-environment node
import { it } from "@effect/vitest";
import { toBase64 } from "@atproto/lex";
import { Effect, Exit, Schema } from "effect";
import { describe, expect } from "vitest";
import {
  ListRepoOpsOutput,
  NotifyWriteInput,
  lexJson,
  parseSpaceRef,
} from "./wire";

const SPACE = "at://did:plc:alice/space/com.example.board/main";

describe("parseSpaceRef", () => {
  it.effect("splits a space ref into authority, type and skey", () =>
    Effect.gen(function* () {
      const ref = yield* parseSpaceRef(SPACE);
      expect(ref).toEqual({
        authority: "did:plc:alice",
        type: "com.example.board",
        skey: "main",
      });
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
            rev: "3l2",
            collection: "com.example.post",
            rkey: "1",
            cid: null,
            prev: "bafyprev",
          },
        ],
        commit: {
          ver: 1,
          hash: bytes(1),
          ikm: bytes(2),
          sig: bytes(3),
          mac: bytes(4),
          rev: "3l2",
        },
      };
      const out = yield* Schema.decodeUnknownEffect(ListRepoOpsOutput)(
        lexJson(json),
      );
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
        repoRev: "3l2",
        hash: { $bytes: toBase64(new Uint8Array(32)) },
        spaceRev: "3l3",
      };
      const input = yield* Schema.decodeUnknownEffect(NotifyWriteInput)(
        lexJson(body),
      );
      expect(input.spaceRev).toBe("3l3");
      expect(input.prevSpaceRev).toBeUndefined();
      expect(input.hash).toBeInstanceOf(Uint8Array);
    }),
  );
});

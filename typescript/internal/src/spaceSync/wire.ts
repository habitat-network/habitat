import { jsonToLex } from "@atproto/lex";
import { Effect, Schema } from "effect";
import { InvalidSpaceRefError, XrpcError } from "./errors";

// Wire shapes follow the upstream alpha lexicons (../atproto/lexicons/com/atproto/space),
// not habitat's lexicons/, which predate spaceRev/repoRev and HTTP message signatures.

const Bytes = Schema.Uint8Array;

export const SignedCommit = Schema.Struct({
  ver: Schema.Literal(1),
  hash: Bytes,
  ikm: Bytes,
  sig: Bytes,
  mac: Bytes,
  rev: Schema.String,
});

export const ListReposRepo = Schema.Struct({
  did: Schema.String,
  repoRev: Schema.String,
  spaceRev: Schema.String,
  hash: Bytes,
});

export const ListReposOutput = Schema.Struct({
  repos: Schema.Array(ListReposRepo),
  cursor: Schema.optional(Schema.String),
});

export const OpEntry = Schema.Struct({
  rev: Schema.String,
  collection: Schema.String,
  rkey: Schema.String,
  cid: Schema.NullOr(Schema.String),
  prev: Schema.NullOr(Schema.String),
  value: Schema.optional(Schema.Unknown),
});

export const ListRepoOpsOutput = Schema.Struct({
  ops: Schema.Array(OpEntry),
  commit: Schema.optional(SignedCommit),
  cursor: Schema.optional(Schema.String),
});

export const RegisterNotifyOutput = Schema.Struct({ expiresAt: Schema.String });

export const GetSpaceCredentialOutput = Schema.Struct({
  credential: Schema.String,
});

export const NotifyWriteInput = Schema.Struct({
  space: Schema.String,
  repo: Schema.String,
  repoRev: Schema.String,
  hash: Bytes,
  spaceRev: Schema.optional(Schema.String),
  prevSpaceRev: Schema.optional(Schema.String),
});
export type NotifyWriteInput = typeof NotifyWriteInput.Type;

export const NotifySpaceDeletedInput = Schema.Struct({ space: Schema.String });

/** Convert atproto JSON (`{$bytes}`, `{$link}`) into lex values before Schema decoding. */
export const lexJson = (json: unknown): unknown =>
  jsonToLex(json as Parameters<typeof jsonToLex>[0]);

export const invalidResponse =
  (method: string) =>
  (error: Schema.SchemaError): XrpcError =>
    new XrpcError({
      method,
      status: 200,
      error: "InvalidResponse",
      message: error.message,
    });

const SPACE_REF =
  /^at:\/\/(did:[a-z]+:[a-zA-Z0-9._:%-]+)\/space\/([a-zA-Z][a-zA-Z0-9.-]*)\/([A-Za-z0-9._:~-]{1,512})$/;

export const parseSpaceRef = (
  space: string,
): Effect.Effect<
  { authority: string; type: string; skey: string },
  InvalidSpaceRefError
> => {
  const match = SPACE_REF.exec(space);
  return match
    ? Effect.succeed({ authority: match[1], type: match[2], skey: match[3] })
    : Effect.fail(new InvalidSpaceRefError({ space }));
};

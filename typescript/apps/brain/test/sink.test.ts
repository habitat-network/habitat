import { env } from "cloudflare:workers";
import { jsonToLex, parseCid, type LexMap } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import type { PromiseRepoBatch } from "internal/spaceSync";
import { beforeEach, describe, expect, it } from "vitest";
import { chatsForOwner, getDb, messagesForSpace } from "@/db";
import { chats, messages } from "@/db/schema";
import {
  ChatSink,
  MESSAGE_COLLECTION,
  SESSION_COLLECTION,
} from "@/server/sink";

const OWNER = "did:plc:alice" as DidString;
const SPACE =
  `at://${OWNER}/space/network.habitat.brain/3kchat` as SpaceRefString;
const RECORD_CID =
  "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm";

const sessionRecord = (title: string) =>
  jsonToLex({
    $type: SESSION_COLLECTION,
    title,
    createdAt: "2026-10-08T12:00:00.000Z",
  }) as LexMap;

const messageRecord = (role: string, content: string, at: string) =>
  jsonToLex({
    $type: MESSAGE_COLLECTION,
    role,
    content,
    createdAt: at,
  }) as LexMap;

describe("ChatSink", () => {
  const db = getDb(env);
  const signal = new AbortController().signal;
  const sink = new ChatSink({ db });

  beforeEach(async () => {
    await db.delete(messages);
    await db.delete(chats);
  });

  const ops = (
    changes: Extract<PromiseRepoBatch, { _tag: "Ops" }>["changes"],
  ): PromiseRepoBatch => ({
    _tag: "Ops",
    space: SPACE,
    did: OWNER,
    rev: "r1",
    changes,
  });

  const put = (collection: string, rkey: string, value: LexMap | null) => ({
    uri: `${SPACE}/${OWNER}/${collection}/${rkey}`,
    collection,
    rkey,
    cid: value ? parseCid(RECORD_CID) : null,
    value: value ?? undefined,
  });

  it("indexes a chat and its messages in order", async () => {
    await sink.apply(
      ops([
        put(SESSION_COLLECTION, "self", sessionRecord("Hello")),
        put(
          MESSAGE_COLLECTION,
          "b",
          messageRecord("assistant", "Hi!", "2026-10-08T12:00:02.000Z"),
        ),
        put(
          MESSAGE_COLLECTION,
          "a",
          messageRecord("user", "Hello", "2026-10-08T12:00:01.000Z"),
        ),
      ]),
      signal,
    );

    expect(await chatsForOwner(db, OWNER)).toEqual([
      {
        space: SPACE,
        title: "Hello",
        createdAt: Date.parse("2026-10-08T12:00:00.000Z"),
      },
    ]);
    expect(await messagesForSpace(db, SPACE)).toEqual([
      { id: "a", role: "user", content: "Hello" },
      { id: "b", role: "assistant", content: "Hi!" },
    ]);
  });

  it("renames a chat when its session record is rewritten", async () => {
    const write = (title: string) =>
      sink.apply(
        ops([put(SESSION_COLLECTION, "self", sessionRecord(title))]),
        signal,
      );
    await write("Old");
    await write("New");
    expect((await chatsForOwner(db, OWNER)).map((c) => c.title)).toEqual([
      "New",
    ]);
  });

  it("skips malformed records instead of failing the batch", async () => {
    await sink.apply(
      ops([
        put(MESSAGE_COLLECTION, "bad", jsonToLex({ role: 1 }) as LexMap),
        put(
          MESSAGE_COLLECTION,
          "ok",
          messageRecord("user", "Hello", "2026-10-08T12:00:01.000Z"),
        ),
      ]),
      signal,
    );
    expect((await messagesForSpace(db, SPACE)).map((m) => m.id)).toEqual([
      "ok",
    ]);
  });

  it("deletes a message", async () => {
    await sink.apply(
      ops([
        put(
          MESSAGE_COLLECTION,
          "a",
          messageRecord("user", "Hello", "2026-10-08T12:00:01.000Z"),
        ),
      ]),
      signal,
    );
    await sink.apply(ops([put(MESSAGE_COLLECTION, "a", null)]), signal);
    expect(await messagesForSpace(db, SPACE)).toEqual([]);
  });

  it("drops everything when the space is deleted", async () => {
    await sink.apply(
      ops([
        put(SESSION_COLLECTION, "self", sessionRecord("Hello")),
        put(
          MESSAGE_COLLECTION,
          "a",
          messageRecord("user", "Hello", "2026-10-08T12:00:01.000Z"),
        ),
      ]),
      signal,
    );
    await sink.apply({ _tag: "SpaceDeleted", space: SPACE }, signal);
    expect(await chatsForOwner(db, OWNER)).toEqual([]);
    expect(await messagesForSpace(db, SPACE)).toEqual([]);
  });

  it("replaces a repo's records on reset", async () => {
    await sink.apply(
      ops([
        put(
          MESSAGE_COLLECTION,
          "stale",
          messageRecord("user", "Old", "2026-10-08T12:00:01.000Z"),
        ),
      ]),
      signal,
    );
    async function* records() {
      yield {
        collection: MESSAGE_COLLECTION as typeof MESSAGE_COLLECTION,
        rkey: "fresh",
        cid: parseCid(RECORD_CID),
        record: messageRecord("user", "New", "2026-10-08T12:00:02.000Z"),
      };
    }
    await sink.apply(
      { _tag: "Reset", space: SPACE, did: OWNER, records: records() } as never,
      signal,
    );
    expect(await messagesForSpace(db, SPACE)).toEqual([
      { id: "fresh", role: "user", content: "New" },
    ]);
  });
});

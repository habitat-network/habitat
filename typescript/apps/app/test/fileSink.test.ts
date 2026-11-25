import { env } from "cloudflare:workers";
import { jsonToLex, type LexMap } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { beforeEach, describe, expect, it } from "vitest";
import { filesForOrg, getDb } from "@/db";
import { files } from "@/db/schema";
import { FILE_COLLECTION } from "@/server/files/sink";
import { delEvent, indexEvents, putEvent, spaceDeletedEvent } from "./events";

const ORG = "did:web:org.example" as DidString;
const SPACE = `at:space/${ORG}/network.habitat.drop/3kfile` as SpaceRefString;
const URI = `${SPACE}/${ORG}/${FILE_COLLECTION}/self`;
const BLOB_CID = "bafkreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku";

const fileRecord = (name: string) =>
  jsonToLex({
    $type: FILE_COLLECTION,
    name,
    file: {
      $type: "blob",
      ref: { $link: BLOB_CID },
      mimeType: "text/plain",
      size: 11,
    },
    uploadedBy: "did:plc:alice",
    createdAt: "2026-10-08T12:00:00.000Z",
  }) as LexMap;

describe("files indexing", () => {
  const db = getDb(env);

  beforeEach(async () => {
    await db.delete(files);
  });

  const put = (name: string) =>
    putEvent(SPACE, ORG, FILE_COLLECTION, "self", fileRecord(name));
  const del = () => delEvent(SPACE, ORG, FILE_COLLECTION, "self");

  it("indexes a created file", async () => {
    await indexEvents(put("notes.txt"));

    expect(await filesForOrg(db, ORG)).toEqual([
      {
        uri: URI,
        space: SPACE,
        name: "notes.txt",
        mimeType: "text/plain",
        size: 11,
        uploadedBy: "did:plc:alice",
        createdAt: Date.parse("2026-10-08T12:00:00.000Z"),
      },
    ]);
  });

  it("replays an event idempotently", async () => {
    await indexEvents(put("notes.txt"), put("notes.txt"));
    expect(await filesForOrg(db, ORG)).toHaveLength(1);
  });

  it("removes a deleted file and ignores other collections and bad records", async () => {
    await indexEvents(
      put("notes.txt"),
      putEvent(
        SPACE,
        ORG,
        "app.example.other",
        "self",
        fileRecord("ignored.txt"),
      ),
    );
    expect(await filesForOrg(db, ORG)).toHaveLength(1);

    await indexEvents(del());
    expect(await filesForOrg(db, ORG)).toEqual([]);

    await indexEvents(
      putEvent(SPACE, ORG, FILE_COLLECTION, "self", {
        $type: FILE_COLLECTION,
        name: 42,
      }),
    );
    expect(await filesForOrg(db, ORG)).toEqual([]);
  });

  it("drops a space's files when the space is deleted", async () => {
    await indexEvents(put("notes.txt"), spaceDeletedEvent(SPACE));
    expect(await filesForOrg(db, ORG)).toEqual([]);
  });

  it("only indexes files from file spaces", async () => {
    const other = `at:space/${ORG}/network.habitat.docs/3kdoc`;
    await indexEvents(
      putEvent(other, ORG, FILE_COLLECTION, "self", fileRecord("notes.txt")),
    );
    expect(await filesForOrg(db, ORG)).toEqual([]);
  });
});

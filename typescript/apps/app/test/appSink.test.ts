import { env } from "cloudflare:workers";
import { jsonToLex, parseCid, type LexMap } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import type { RepoBatch } from "../src/server/stream/batch";
import { beforeEach, describe, expect, it } from "vitest";
import { appByHost, appsForOrg, getDb } from "@/db";
import { apps } from "@/db/schema";
import { APP_COLLECTION, AppRecordSink, toAppRow } from "@/server/apps/sink";

const ORG = "did:web:org.example" as DidString;
const SPACE = `at://${ORG}/space/network.habitat.apps/3kapp` as SpaceRefString;
const URI = `${SPACE}/${ORG}/${APP_COLLECTION}/self`;
const RECORD_CID =
  "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm";
const BLOB_CID = "bafkreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku";

const appRecord = (over: Record<string, unknown> = {}) =>
  jsonToLex({
    $type: APP_COLLECTION,
    name: "Chat",
    slug: "chat",
    entrypoint: "index.js",
    code: {
      $type: "blob",
      ref: { $link: BLOB_CID },
      mimeType: "application/json",
      size: 120,
    },
    version: 1,
    compatibilityDate: "2026-08-01",
    createdBy: "did:plc:alice",
    updatedBy: "did:plc:alice",
    createdAt: "2026-11-20T12:00:00.000Z",
    updatedAt: "2026-11-20T12:00:00.000Z",
    ...over,
  }) as LexMap;

describe("toAppRow", () => {
  it("maps a record to its row, with its run host", () => {
    expect(toAppRow(URI, RECORD_CID, appRecord())).toMatchObject({
      uri: URI,
      space: SPACE,
      orgDid: ORG,
      repo: ORG,
      host: "chat-3kapp",
      codeCid: BLOB_CID,
      codeSize: 120,
      version: 1,
    });
  });

  it("skips a record a member wrote rather than the group", () => {
    const uri = `${SPACE}/did:plc:alice/${APP_COLLECTION}/self`;
    expect(toAppRow(uri, RECORD_CID, appRecord())).toBeUndefined();
  });

  it("skips a record with a slug that isn't DNS-safe, or that's malformed", () => {
    expect(
      toAppRow(URI, RECORD_CID, appRecord({ slug: "Not OK" })),
    ).toBeUndefined();
    expect(
      toAppRow(URI, RECORD_CID, appRecord({ version: "1" })),
    ).toBeUndefined();
  });
});

describe("AppRecordSink", () => {
  const db = getDb(env);
  const signal = new AbortController().signal;
  const sink = new AppRecordSink({ db });

  beforeEach(async () => {
    await db.delete(apps);
  });

  const put = (value: LexMap): RepoBatch => ({
    _tag: "Ops",
    space: SPACE,
    did: ORG,
    rev: "r1",
    changes: [
      {
        uri: URI,
        collection: APP_COLLECTION,
        rkey: "self",
        cid: parseCid(RECORD_CID),
        value,
      },
    ],
  });

  it("indexes a deployed app and its new versions", async () => {
    await sink.apply(put(appRecord()), signal);
    await sink.apply(
      put(appRecord({ version: 2, entrypoint: "main.js" })),
      signal,
    );
    const [row] = await appsForOrg(db, ORG);
    expect(row).toMatchObject({
      name: "Chat",
      version: 2,
      entrypoint: "main.js",
    });
    expect((await appByHost(db, "chat-3kapp"))?.uri).toBe(URI);
  });

  it("takes an app down when its record is deleted or stops parsing", async () => {
    await sink.apply(put(appRecord()), signal);
    await sink.apply(put(appRecord({ slug: "Bad Slug" })), signal);
    expect(await appsForOrg(db, ORG)).toEqual([]);

    await sink.apply(put(appRecord()), signal);
    await sink.apply(
      {
        _tag: "Ops",
        space: SPACE,
        did: ORG,
        rev: "r2",
        changes: [
          { uri: URI, collection: APP_COLLECTION, rkey: "self", cid: null },
        ],
      },
      signal,
    );
    expect(await appsForOrg(db, ORG)).toEqual([]);
  });

  it("follows space deletions", async () => {
    await sink.apply(put(appRecord()), signal);
    await sink.apply({ _tag: "SpaceDeleted", space: SPACE }, signal);
    expect(await appsForOrg(db, ORG)).toEqual([]);
  });
});

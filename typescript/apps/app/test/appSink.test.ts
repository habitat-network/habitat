import { env } from "cloudflare:workers";
import { jsonToLex, type LexMap } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import { beforeEach, describe, expect, it } from "vitest";
import { appByHost, appsForOrg, getDb } from "@/db";
import { apps } from "@/db/schema";
import { APP_COLLECTION, toAppRow } from "@/server/apps/sink";
import {
  delEvent,
  indexEvents,
  putEvent,
  spaceDeletedEvent,
  RECORD_CID,
} from "./events";

const ORG = "did:web:org.example" as DidString;
const SPACE = `at://${ORG}/space/network.habitat.apps/3kapp` as SpaceRefString;
const URI = `${SPACE}/${ORG}/${APP_COLLECTION}/self`;
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

describe("apps indexing", () => {
  const db = getDb(env);

  beforeEach(async () => {
    await db.delete(apps);
  });

  const put = (value: LexMap) =>
    putEvent(SPACE, ORG, APP_COLLECTION, "self", value);

  it("indexes a deployed app and its new versions", async () => {
    await indexEvents(
      put(appRecord()),
      put(appRecord({ version: 2, entrypoint: "main.js" })),
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
    await indexEvents(put(appRecord()), put(appRecord({ slug: "Bad Slug" })));
    expect(await appsForOrg(db, ORG)).toEqual([]);

    await indexEvents(
      put(appRecord()),
      delEvent(SPACE, ORG, APP_COLLECTION, "self"),
    );
    expect(await appsForOrg(db, ORG)).toEqual([]);
  });

  it("follows space deletions", async () => {
    await indexEvents(put(appRecord()), spaceDeletedEvent(SPACE));
    expect(await appsForOrg(db, ORG)).toEqual([]);
  });
});

import { env } from "cloudflare:workers";
import { jsonToLex, parseCid, type LexMap } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import type { PromiseRepoBatch } from "internal/spaceSync";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { filesForOrg, getDb } from "@/db";
import { files } from "@/db/schema";
import { type BlobFetcher, FILE_COLLECTION, FileSink } from "@/server/sink";

const ORG = "did:web:org.example" as DidString;
const SPACE = `at://${ORG}/space/network.habitat.drop/3kfile` as SpaceRefString;
const OTHER_SPACE =
  `at://${ORG}/space/network.habitat.drop/3kother` as SpaceRefString;
const URI = `${SPACE}/${ORG}/${FILE_COLLECTION}/self`;
const RECORD_CID =
  "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm";
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

async function* records(
  ...items: { rkey: string; record: LexMap; collection?: string }[]
) {
  for (const { rkey, record, collection } of items) {
    yield {
      collection: (collection ?? FILE_COLLECTION) as typeof FILE_COLLECTION,
      rkey,
      cid: parseCid(RECORD_CID),
      record,
    };
  }
}

describe("FileSink", () => {
  const db = getDb(env);
  const signal = new AbortController().signal;
  let fetchBlob: ReturnType<typeof vi.fn<BlobFetcher>>;
  let onChange: ReturnType<typeof vi.fn<(orgDid: string) => void>>;
  let sink: FileSink;

  beforeEach(async () => {
    await db.delete(files);
    await env.FILES.delete(BLOB_CID);
    fetchBlob = vi.fn<BlobFetcher>(async () =>
      new TextEncoder().encode("hello world"),
    );
    onChange = vi.fn<(orgDid: string) => void>();
    sink = new FileSink({ db, bucket: env.FILES, fetchBlob, onChange });
  });

  const ops = (
    changes: Extract<PromiseRepoBatch, { _tag: "Ops" }>["changes"],
  ): PromiseRepoBatch => ({
    _tag: "Ops",
    space: SPACE,
    did: ORG,
    rev: "r1",
    changes,
  });

  it("indexes a created file and mirrors its blob into R2", async () => {
    await sink.apply(
      ops([
        {
          uri: URI,
          collection: FILE_COLLECTION,
          rkey: "self",
          cid: parseCid(RECORD_CID),
          value: fileRecord("notes.txt"),
        },
      ]),
      signal,
    );

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
    expect(fetchBlob).toHaveBeenCalledWith(SPACE, ORG, BLOB_CID);
    expect(await (await env.FILES.get(BLOB_CID))?.text()).toBe("hello world");
    expect(onChange).toHaveBeenCalledWith(ORG);
  });

  it("skips the blob fetch when R2 already has it, and replays idempotently", async () => {
    await env.FILES.put(BLOB_CID, "hello world");
    const batch = ops([
      {
        uri: URI,
        collection: FILE_COLLECTION,
        rkey: "self",
        cid: parseCid(RECORD_CID),
        value: fileRecord("notes.txt"),
      },
    ]);
    await sink.apply(batch, signal);
    await sink.apply(batch, signal);
    expect(fetchBlob).not.toHaveBeenCalled();
    expect(await filesForOrg(db, ORG)).toHaveLength(1);
  });

  it("removes a deleted file and ignores other collections and bad records", async () => {
    await sink.apply(
      ops([
        {
          uri: URI,
          collection: FILE_COLLECTION,
          rkey: "self",
          cid: parseCid(RECORD_CID),
          value: fileRecord("notes.txt"),
        },
        {
          uri: `${SPACE}/${ORG}/app.example.other/self`,
          collection: "app.example.other",
          rkey: "self",
          cid: parseCid(RECORD_CID),
          value: fileRecord("ignored.txt"),
        },
      ]),
      signal,
    );
    expect(await filesForOrg(db, ORG)).toHaveLength(1);

    await sink.apply(
      ops([
        {
          uri: URI,
          collection: FILE_COLLECTION,
          rkey: "self",
          cid: null,
        },
      ]),
      signal,
    );
    expect(await filesForOrg(db, ORG)).toEqual([]);

    await sink.apply(
      ops([
        {
          uri: URI,
          collection: FILE_COLLECTION,
          rkey: "self",
          cid: parseCid(RECORD_CID),
          value: { $type: FILE_COLLECTION, name: 42 } as unknown as LexMap,
        },
      ]),
      signal,
    );
    expect(await filesForOrg(db, ORG)).toEqual([]);
  });

  it("Reset replaces the repo's files with exactly the listed records", async () => {
    // A stale row in this repo, and one in another space that must survive.
    await sink.apply(
      ops([
        {
          uri: `${SPACE}/${ORG}/${FILE_COLLECTION}/stale`,
          collection: FILE_COLLECTION,
          rkey: "stale",
          cid: parseCid(RECORD_CID),
          value: fileRecord("stale.txt"),
        },
      ]),
      signal,
    );
    await sink.apply(
      {
        _tag: "Ops",
        space: OTHER_SPACE,
        did: ORG,
        rev: "r1",
        changes: [
          {
            uri: `${OTHER_SPACE}/${ORG}/${FILE_COLLECTION}/self`,
            collection: FILE_COLLECTION,
            rkey: "self",
            cid: parseCid(RECORD_CID),
            value: fileRecord("other.txt"),
          },
        ],
      },
      signal,
    );

    await sink.apply(
      {
        _tag: "Reset",
        space: SPACE,
        did: ORG,
        rev: "r2",
        records: records({ rkey: "self", record: fileRecord("fresh.txt") }),
      },
      signal,
    );

    const names = (await filesForOrg(db, ORG)).map((f) => f.name).sort();
    expect(names).toEqual(["fresh.txt", "other.txt"]);
  });

  it("drops a space's files when the space is deleted", async () => {
    await sink.apply(
      ops([
        {
          uri: URI,
          collection: FILE_COLLECTION,
          rkey: "self",
          cid: parseCid(RECORD_CID),
          value: fileRecord("notes.txt"),
        },
      ]),
      signal,
    );
    await sink.apply({ _tag: "SpaceDeleted", space: SPACE }, signal);
    expect(await filesForOrg(db, ORG)).toEqual([]);
  });

  it("writes nothing when aborted before committing", async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(
      sink.apply(
        ops([
          {
            uri: URI,
            collection: FILE_COLLECTION,
            rkey: "self",
            cid: parseCid(RECORD_CID),
            value: fileRecord("notes.txt"),
          },
        ]),
        controller.signal,
      ),
    ).rejects.toThrow();
    expect(await filesForOrg(db, ORG)).toEqual([]);
  });
});

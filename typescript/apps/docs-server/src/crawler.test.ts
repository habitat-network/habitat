import assert from "node:assert/strict";
import test from "node:test";
import { DatabaseSync } from "node:sqlite";
import { classify, parseSpaceRecordUri } from "./crawler";
import { DocMetadataStore } from "./docMetadataStore";
import { DocCrdtStore } from "./docCrdtStore";

test("parseSpaceRecordUri parses at:// and ats:// forms", () => {
  const current = parseSpaceRecordUri(
    "at://did:web:org/space/network.habitat.group/s1/did:web:org/network.habitat.docs.markdown/self",
  );
  assert.deepEqual(current, {
    spaceUri: "at://did:web:org/space/network.habitat.group/s1",
    owner: "did:web:org",
    type: "network.habitat.group",
    skey: "s1",
    collection: "network.habitat.docs.markdown",
  });

  const legacy = parseSpaceRecordUri(
    "ats://did:web:org/network.habitat.group/s1/did:web:org/network.habitat.docs.markdown/self",
  );
  assert.equal(legacy?.spaceUri, current?.spaceUri);
});

test("classify maps a null value to a delete", () => {
  const parsed = parseSpaceRecordUri(
    "at://did:web:org/space/network.habitat.group/s1/did:web:org/network.habitat.docs.markdown/self",
  );
  const action = classify({ id: 1, uri: "x", value: null }, parsed!);
  assert.deepEqual(action, { kind: "delete", spaceUri: parsed!.spaceUri });
});

test("classify maps a markdown value to an upsert", () => {
  const parsed = parseSpaceRecordUri(
    "at://did:web:org/space/network.habitat.group/s1/did:web:org/network.habitat.docs.markdown/self",
  );
  const action = classify({ id: 1, uri: "x", value: { title: "Hi" } }, parsed!);
  assert.deepEqual(action, {
    kind: "upsert",
    spaceUri: parsed!.spaceUri,
    docId: "s1",
    title: "Hi",
  });
});

test("deleteDoc removes the doc", () => {
  const db = new DatabaseSync(":memory:");
  const meta = new DocMetadataStore(db);
  meta.upsertDoc({ spaceUri: "s", docId: "d", title: "t" });
  meta.deleteDoc("s");
  assert.deepEqual(meta.docsBySpaceUris(["s"]), []);
});

test("deleteState removes the crdt state", () => {
  const db = new DatabaseSync(":memory:");
  const crdt = new DocCrdtStore(
    {
      putRecord: async () => ({ uri: "", cid: "" }),
      getRecord: async () => undefined,
      createSpace: async () => ({ uri: "", skey: "" }),
      spaceUri: () => "",
      grantRole: async () => undefined,
    } as never,
    db,
  );
  db.prepare(
    `INSERT INTO doc_crdt (space_uri, state, updated_at) VALUES (?, ?, ?)`,
  ).run("s", "state", 1);
  crdt.deleteState("s");
  const row = db.prepare(`SELECT COUNT(*) AS n FROM doc_crdt`).get();
  assert.equal((row as { n: number }).n, 0);
});

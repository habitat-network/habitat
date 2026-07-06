import type { DatabaseSync } from "node:sqlite";
import { Mutex } from "async-mutex";
import * as Y from "yjs";
import type { PearClient } from "./pearClient";
import { renderDoc } from "./render";

const CRDT_COLLECTION = "network.habitat.docs.crdt";
const MARKDOWN_COLLECTION = "network.habitat.docs.markdown";
const SELF = "self";

// DocCrdtStore persists each document's canonical Yjs state in sqlite (shared
// with the doc-metadata store) instead of memory, so state survives restarts
// and large docs aren't all held in memory. Each document is its own space,
// owned by the caller's org; the store writes a CRDT record and a
// rendered-markdown record to pear, both keyed "self", and mirrors the CRDT
// state to the db. The crawler feeds CRDT records it observes from sap back in
// via upsertState. The org DID is passed per call (resolved from the caller's
// membership) so one docs server can serve many orgs.
export class DocCrdtStore {
  private pear: PearClient;
  private db: DatabaseSync;
  // One mutex per space, so concurrent applyUpdate (and crawler) calls for the
  // same doc run one after another and none is lost to a read-modify-write race.
  // Idle mutexes are evicted once their queue drains.
  private locks = new Map<string, Mutex>();

  constructor(pear: PearClient, db: DatabaseSync) {
    this.pear = pear;
    this.db = db;
    this.db.exec(`
      CREATE TABLE IF NOT EXISTS doc_crdt (
        space_uri  TEXT PRIMARY KEY,
        state      TEXT NOT NULL,
        updated_at INTEGER NOT NULL
      );
    `);
  }

  // createDoc creates a new doc space owned by the caller's org, seeds its
  // records, persists the CRDT state, and grants the creating member write
  // access so they can read it back directly.
  async createDoc(
    memberDid: string,
    org: string,
  ): Promise<{ uri: string; docId: string }> {
    const space = await this.pear.createSpace(org);
    const ydoc = new Y.Doc();
    await this.writeRecords(org, space.uri, ydoc);
    this.persist(space.uri, ydoc);
    await this.pear.addMember(org, space.uri, memberDid, "write");
    return { uri: space.uri, docId: space.skey };
  }

  // applyUpdate merges a Yjs update into a doc and rewrites its records.
  // Concurrent calls for the same doc are serialized so the updates apply one
  // after another: each reads the latest state from the db, merges, then writes
  // it back.
  applyUpdate(
    docId: string,
    updateB64: string,
    _: string, // memberDid - not doing attribution yet
    org: string,
  ): Promise<{ uri: string; cid?: string }> {
    const spaceUri = this.pear.spaceUri(docId, org);
    return this.runExclusive(spaceUri, async () => {
      const ydoc = await this.load(org, spaceUri);
      Y.applyUpdateV2(ydoc, decode(updateB64));
      this.persist(spaceUri, ydoc);
      return this.writeRecords(org, spaceUri, ydoc);
    });
  }

  // upsertState merges a CRDT record the crawler observed from sap into the
  // persisted state. It runs under the same per-space lock as applyUpdate so it
  // can't clobber an in-flight edit.
  upsertState(spaceUri: string, stateB64: string): Promise<void> {
    return this.runExclusive(spaceUri, async () => {
      const ydoc = this.read(spaceUri) ?? new Y.Doc();
      Y.applyUpdateV2(ydoc, decode(stateB64));
      this.persist(spaceUri, ydoc);
    });
  }

  // load reads the doc's CRDT state from the db, falling back to the record in
  // pear when the crawler hasn't delivered it yet (e.g. a doc created on another
  // server).
  private async load(org: string, spaceUri: string): Promise<Y.Doc> {
    const stored = this.read(spaceUri);
    if (stored) {
      return stored;
    }
    const record = await this.pear.getRecord(
      org,
      spaceUri,
      CRDT_COLLECTION,
      SELF,
    );
    const ydoc = new Y.Doc();
    const blob = record && (record.value as { blob?: string }).blob;
    if (blob) {
      Y.applyUpdateV2(ydoc, decode(blob));
    }
    return ydoc;
  }

  // read loads the persisted CRDT state for a space, or undefined if none is
  // stored yet.
  private read(spaceUri: string): Y.Doc | undefined {
    const row = this.db
      .prepare(`SELECT state FROM doc_crdt WHERE space_uri = ?`)
      .get(spaceUri);
    if (!row) {
      return undefined;
    }
    const ydoc = new Y.Doc();
    Y.applyUpdateV2(ydoc, decode(String(row.state)));
    return ydoc;
  }

  // persist mirrors the doc's full CRDT state to the db, keyed by space URI.
  private persist(spaceUri: string, ydoc: Y.Doc): void {
    const state = Buffer.from(Y.encodeStateAsUpdateV2(ydoc)).toString("base64");
    this.db
      .prepare(
        `INSERT INTO doc_crdt (space_uri, state, updated_at)
         VALUES (?, ?, ?)
         ON CONFLICT(space_uri) DO UPDATE SET
           state = excluded.state,
           updated_at = excluded.updated_at`,
      )
      .run(spaceUri, state, Date.now());
  }

  // writeRecords persists the CRDT state and a freshly-rendered markdown record.
  // nameOverride sets the title on creation (when the doc is still empty);
  // otherwise the title is derived from the rendered content.
  private async writeRecords(
    org: string,
    spaceUri: string,
    ydoc: Y.Doc,
    nameOverride?: string,
  ): Promise<{ uri: string; cid?: string }> {
    const rendered = renderDoc(ydoc);
    const title = nameOverride ?? rendered.title;
    // TODO this should be a blob
    const crdt = await this.pear.putRecord(
      org,
      spaceUri,
      CRDT_COLLECTION,
      SELF,
      {
        blob: Buffer.from(Y.encodeStateAsUpdateV2(ydoc)).toString("base64"),
      },
    );
    await this.pear.putRecord(org, spaceUri, MARKDOWN_COLLECTION, SELF, {
      title,
      content: rendered.markdown,
    });
    return { uri: crdt.uri, cid: crdt.cid };
  }

  // runExclusive queues fn behind any in-flight work for the same key, so
  // per-space operations run one at a time in call order. The key's mutex is
  // dropped once its queue drains.
  private runExclusive<T>(key: string, fn: () => Promise<T>): Promise<T> {
    let mutex = this.locks.get(key);
    if (!mutex) {
      mutex = new Mutex();
      this.locks.set(key, mutex);
    }
    return mutex.runExclusive(fn).finally(() => {
      if (!mutex.isLocked() && this.locks.get(key) === mutex) {
        this.locks.delete(key);
      }
    });
  }
}

function decode(b64: string): Uint8Array {
  return new Uint8Array(Buffer.from(b64, "base64"));
}

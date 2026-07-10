import type { DatabaseSync } from "node:sqlite";
import { Mutex } from "async-mutex";
import * as Y from "yjs";
import type { PearClient } from "./pearClient";
import { renderDoc } from "./render";

const CRDT_COLLECTION = "network.habitat.docs.crdt";
const MARKDOWN_COLLECTION = "network.habitat.docs.markdown";
const SELF = "self";

// DocCrdtStore persists each document's merged Yjs state in sqlite (shared with
// the doc-metadata store) instead of memory, so state survives restarts and
// large docs aren't all held in memory. Each document is its own space, owned
// by the caller's org. The merged state is the union of every editor's CRDT
// record: each user writes their own CRDT record into their slice of the space
// (repo = their DID) using their own credential, so edits are attributed to the
// user who made them. The store keeps merging those records — the ones pushed
// live from the frontend (applyUpdate) and the ones the crawler observes from
// sap (upsertState) — into one canonical Yjs doc per space. The rendered
// markdown record stays a single "self" record written with the org
// credential. Org and user DIDs are passed per call so one docs server can
// serve many orgs.
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
  // markdown record with the org credential, persists an empty CRDT state, and
  // grants the creating member the owner role. Owner includes read/write (so
  // they can read the doc back and write their own CRDT record) plus
  // manage-members, which is what lets them share the doc with others via the
  // relationship endpoints. No CRDT record is written yet — the member's own
  // CRDT record is created on their first applyUpdate, authored as them.
  async createDoc(
    memberDid: string,
    org: string,
  ): Promise<{ uri: string; docId: string }> {
    const space = await this.pear.createSpace(org);
    const ydoc = new Y.Doc();
    this.persist(space.uri, ydoc);
    await this.writeMarkdown(org, space.uri, ydoc, "Untitled");
    await this.pear.grantRole(org, space.uri, memberDid, "owner");
    return { uri: space.uri, docId: space.skey };
  }

  // applyUpdate merges a Yjs update from a member into the doc's merged state,
  // writes that member's CRDT record with their own credential, and refreshes
  // the org-owned markdown record. Concurrent calls for the same doc are
  // serialized so the updates apply one after another: each reads the latest
  // merged state from the db, merges, then writes it back.
  applyUpdate(
    docId: string,
    updateB64: string,
    memberDid: string,
    org: string,
  ): Promise<{ uri: string; cid?: string }> {
    const spaceUri = this.pear.spaceUri(docId, org);
    return this.runExclusive(spaceUri, async () => {
      const ydoc = this.read(spaceUri) ?? new Y.Doc();
      Y.applyUpdateV2(ydoc, decode(updateB64));
      this.persist(spaceUri, ydoc);
      // The member's CRDT record is written as them (repo = their DID); the
      // markdown record stays org-authored.
      const crdt = await this.writeCrdt(memberDid, spaceUri, ydoc);
      await this.writeMarkdown(org, spaceUri, ydoc);
      return crdt;
    });
  }

  // stateB64 returns the merged CRDT state for a space as a base64 string, or
  // undefined if the server has no state for it yet. This is the canonical doc
  // content the frontend reads (the space no longer holds a single "self" CRDT
  // record; it holds one per editor, and this is their merge).
  stateB64(spaceUri: string): string | undefined {
    const row = this.db
      .prepare(`SELECT state FROM doc_crdt WHERE space_uri = ?`)
      .get(spaceUri);
    return row ? String(row.state) : undefined;
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

  // writeCrdt writes the merged CRDT state as the given member's own record
  // (repo = their DID), authored with their credential. Each editor thus has
  // their own CRDT record in the space; sap replicates them and the crawler
  // merges them back via upsertState.
  private writeCrdt(
    memberDid: string,
    spaceUri: string,
    ydoc: Y.Doc,
  ): Promise<{ uri: string; cid?: string }> {
    // TODO this should be a blob
    return this.pear.putRecord(
      memberDid,
      spaceUri,
      memberDid,
      CRDT_COLLECTION,
      SELF,
      {
        blob: Buffer.from(Y.encodeStateAsUpdateV2(ydoc)).toString("base64"),
      },
    );
  }

  // writeMarkdown writes the freshly-rendered markdown record with the org
  // credential (repo = org). nameOverride sets the title on creation (when the
  // doc is still empty); otherwise the title is derived from the rendered
  // content.
  private async writeMarkdown(
    org: string,
    spaceUri: string,
    ydoc: Y.Doc,
    nameOverride?: string,
  ): Promise<void> {
    const rendered = renderDoc(ydoc);
    const title = nameOverride ?? rendered.title;
    await this.pear.putRecord(
      org,
      spaceUri,
      org,
      MARKDOWN_COLLECTION,
      SELF,
      {
        title,
        content: rendered.markdown,
      },
    );
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

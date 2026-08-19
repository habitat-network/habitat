import type { DatabaseSync } from "node:sqlite";
import * as Y from "yjs";

export interface DocSummary {
  docId: string;
  uri: string;
  ownerDid: string;
  title: string;
}

// DocStore persists the docs chalk has discovered (via its own creates or
// the sap-outbox consumer) and each doc's merged CRDT snapshot, in sqlite so
// both survive restarts.
export class DocStore {
  constructor(private db: DatabaseSync) {
    this.db.exec(`
      CREATE TABLE IF NOT EXISTS docs (
        space_uri  TEXT PRIMARY KEY,
        doc_id     TEXT NOT NULL UNIQUE,
        owner_did  TEXT NOT NULL,
        title      TEXT NOT NULL,
        updated_at INTEGER NOT NULL
      );
      CREATE TABLE IF NOT EXISTS doc_crdt (
        space_uri  TEXT PRIMARY KEY,
        state      TEXT NOT NULL,
        updated_at INTEGER NOT NULL
      );
    `);
  }

  upsertDoc(doc: {
    spaceUri: string;
    docId: string;
    ownerDid: string;
    title: string;
  }): void {
    this.db
      .prepare(
        `INSERT INTO docs (space_uri, doc_id, owner_did, title, updated_at)
         VALUES (?, ?, ?, ?, ?)
         ON CONFLICT(space_uri) DO UPDATE SET
           doc_id = excluded.doc_id,
           owner_did = excluded.owner_did,
           title = excluded.title,
           updated_at = excluded.updated_at`,
      )
      .run(doc.spaceUri, doc.docId, doc.ownerDid, doc.title, Date.now());
  }

  docsForOwner(ownerDid: string): DocSummary[] {
    const rows = this.db
      .prepare(
        `SELECT doc_id AS docId, space_uri AS uri, owner_did AS ownerDid, title
         FROM docs WHERE owner_did = ? ORDER BY updated_at DESC`,
      )
      .all(ownerDid);
    return rows.map(rowToSummary);
  }

  docsByUris(spaceUris: string[]): DocSummary[] {
    if (spaceUris.length === 0) return [];
    const placeholders = spaceUris.map(() => "?").join(",");
    const rows = this.db
      .prepare(
        `SELECT doc_id AS docId, space_uri AS uri, owner_did AS ownerDid, title
         FROM docs WHERE space_uri IN (${placeholders}) ORDER BY updated_at DESC`,
      )
      .all(...spaceUris);
    return rows.map(rowToSummary);
  }

  // docByDocId looks up a doc's summary (including its owner and space URI)
  // by docId alone. Needed anywhere a caller only has the docId in hand — the
  // member submitting an edit isn't necessarily the doc's owner, so their
  // space URI can't be assumed from their own DID; it must come from here.
  docByDocId(docId: string): DocSummary | undefined {
    const row = this.db
      .prepare(
        `SELECT doc_id AS docId, space_uri AS uri, owner_did AS ownerDid, title
         FROM docs WHERE doc_id = ?`,
      )
      .get(docId);
    return row ? rowToSummary(row) : undefined;
  }

  mergedState(spaceUri: string): Y.Doc | undefined {
    const row = this.db
      .prepare(`SELECT state FROM doc_crdt WHERE space_uri = ?`)
      .get(spaceUri);
    if (!row) return undefined;
    const ydoc = new Y.Doc();
    Y.applyUpdateV2(ydoc, Buffer.from(String(row.state), "base64"));
    return ydoc;
  }

  persistMerged(spaceUri: string, ydoc: Y.Doc): void {
    const state = Buffer.from(Y.encodeStateAsUpdateV2(ydoc)).toString("base64");
    this.db
      .prepare(
        `INSERT INTO doc_crdt (space_uri, state, updated_at)
         VALUES (?, ?, ?)
         ON CONFLICT(space_uri) DO UPDATE SET
           state = excluded.state, updated_at = excluded.updated_at`,
      )
      .run(spaceUri, state, Date.now());
  }
}

function rowToSummary(r: Record<string, unknown>): DocSummary {
  return {
    docId: String(r.docId),
    uri: String(r.uri),
    ownerDid: String(r.ownerDid),
    title: String(r.title),
  };
}

import type { DatabaseSync } from "node:sqlite";

export interface DocView {
  docId: string;
  uri: string;
  title: string;
}

// DocMetadataStore persists the docs the sap crawler discovers (space URI, id and
// title). It carries no permission state: what a user may read is resolved on
// demand via relationship.listObjects, and listDocs intersects that with this
// table. Backed by sqlite so the crawl state survives restarts (sap only
// redelivers unacked messages).
export class DocMetadataStore {
  private db: DatabaseSync;

  constructor(db: DatabaseSync) {
    this.db = db;
    this.db.exec(`
      CREATE TABLE IF NOT EXISTS docs (
        space_uri  TEXT PRIMARY KEY,
        doc_id     TEXT NOT NULL,
        title      TEXT NOT NULL,
        updated_at INTEGER NOT NULL
      );
      DROP TABLE IF EXISTS doc_readers;
    `);
  }

  // upsertDoc records (or refreshes the title of) a doc keyed by its space URI.
  upsertDoc(doc: { spaceUri: string; docId: string; title: string }): void {
    this.db
      .prepare(
        `INSERT INTO docs (space_uri, doc_id, title, updated_at)
         VALUES (?, ?, ?, ?)
         ON CONFLICT(space_uri) DO UPDATE SET
           doc_id = excluded.doc_id,
           title = excluded.title,
           updated_at = excluded.updated_at`,
      )
      .run(doc.spaceUri, doc.docId, doc.title, Date.now());
  }

  // docsBySpaceUris returns the crawled docs for the given space URIs, newest
  // first. URIs the crawler hasn't seen yet are simply absent.
  docsBySpaceUris(spaceUris: string[]): DocView[] {
    if (spaceUris.length === 0) {
      return [];
    }
    const placeholders = spaceUris.map(() => "?").join(",");
    const rows = this.db
      .prepare(
        `SELECT doc_id AS docId, space_uri AS uri, title
         FROM docs
         WHERE space_uri IN (${placeholders})
         ORDER BY updated_at DESC`,
      )
      .all(...spaceUris);
    return rows.map((r) => ({
      docId: String(r.docId),
      uri: String(r.uri),
      title: String(r.title),
    }));
  }
}

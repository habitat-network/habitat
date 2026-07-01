import { DatabaseSync } from "node:sqlite";

export interface DocView {
  docId: string;
  uri: string;
  title: string;
}

// CrawlStore persists the docs the sap crawler discovers and, for each doc, the
// set of member DIDs that hold read access. listDocs is served from here so a
// caller only ever sees docs they are allowed to read. It is backed by sqlite
// so the crawl state survives restarts (sap only redelivers unacked messages).
export class CrawlStore {
  private db: DatabaseSync;

  constructor(dbPath: string) {
    this.db = new DatabaseSync(dbPath);
    this.db.exec(`
      CREATE TABLE IF NOT EXISTS docs (
        space_uri  TEXT PRIMARY KEY,
        doc_id     TEXT NOT NULL,
        title      TEXT NOT NULL,
        updated_at INTEGER NOT NULL
      );
      CREATE TABLE IF NOT EXISTS doc_readers (
        space_uri TEXT NOT NULL,
        did       TEXT NOT NULL,
        PRIMARY KEY (space_uri, did)
      );
      CREATE INDEX IF NOT EXISTS idx_doc_readers_did ON doc_readers (did);
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

  // replaceReaders atomically swaps the stored reader DIDs for a doc so the set
  // always reflects the latest listSubjects result.
  replaceReaders(spaceUri: string, dids: string[]): void {
    const del = this.db.prepare(`DELETE FROM doc_readers WHERE space_uri = ?`);
    const ins = this.db.prepare(
      `INSERT OR IGNORE INTO doc_readers (space_uri, did) VALUES (?, ?)`,
    );
    this.db.exec("BEGIN");
    try {
      del.run(spaceUri);
      for (const did of dids) {
        ins.run(spaceUri, did);
      }
      this.db.exec("COMMIT");
    } catch (err) {
      this.db.exec("ROLLBACK");
      throw err;
    }
  }

  // listDocsForSubject returns the docs the given DID may read, newest first.
  listDocsForSubject(did: string): DocView[] {
    const rows = this.db
      .prepare(
        `SELECT d.doc_id AS docId, d.space_uri AS uri, d.title AS title
         FROM docs d
         JOIN doc_readers r ON r.space_uri = d.space_uri
         WHERE r.did = ?
         ORDER BY d.updated_at DESC`,
      )
      .all(did);
    return rows.map((r) => ({
      docId: String(r.docId),
      uri: String(r.uri),
      title: String(r.title),
    }));
  }
}

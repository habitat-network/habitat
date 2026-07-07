import type { DatabaseSync } from "node:sqlite";

export interface CommentRange {
  start: string;
  end: string;
}

// StoredComment is one comment record the crawler observed, keyed by its full
// space-record URI and grouped by the doc space it relates to. Top-level
// comments carry a range; replies carry a parentUri.
export interface StoredComment {
  docSpace: string;
  uri: string;
  author: string;
  body: string;
  createdAt: string;
  parentUri?: string;
  range?: CommentRange;
}

export interface ReplyView {
  uri: string;
  author: string;
  body: string;
  createdAt: string;
}

export interface CommentView extends ReplyView {
  range?: CommentRange;
  replies: ReplyView[];
}

// DocCommentStore persists the comment records the sap crawler discovers,
// grouped by the doc space they relate to. Comments live in each member's repo
// within the doc's companion comment space (written with the member's own
// session), so the store is the docs server's index across all members'
// comment records for a document. Backed by sqlite (shared with the other doc
// stores) so it survives restarts; sap only redelivers unacked messages.
export class DocCommentStore {
  private db: DatabaseSync;

  constructor(db: DatabaseSync) {
    this.db = db;
    this.db.exec(`
      CREATE TABLE IF NOT EXISTS doc_comments (
        uri         TEXT PRIMARY KEY,
        doc_space   TEXT NOT NULL,
        author      TEXT NOT NULL,
        body        TEXT NOT NULL,
        created_at  TEXT NOT NULL,
        parent_uri  TEXT,
        range_start TEXT,
        range_end   TEXT,
        updated_at  INTEGER NOT NULL
      );
      CREATE INDEX IF NOT EXISTS doc_comments_doc
        ON doc_comments (doc_space);
    `);
  }

  // upsertComment records (or overwrites) one comment keyed by its record URI.
  upsertComment(c: StoredComment): void {
    console.log("[docCommentStore] upsertComment called", { uri: c.uri, docSpace: c.docSpace, author: c.author });
    try {
      this.db
        .prepare(
          `INSERT INTO doc_comments
             (uri, doc_space, author, body, created_at, parent_uri, range_start, range_end, updated_at)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
           ON CONFLICT(uri) DO UPDATE SET
             doc_space = excluded.doc_space,
             author = excluded.author,
             body = excluded.body,
             created_at = excluded.created_at,
             parent_uri = excluded.parent_uri,
             range_start = excluded.range_start,
             range_end = excluded.range_end,
             updated_at = excluded.updated_at`,
        )
        .run(
          c.uri,
          c.docSpace,
          c.author,
          c.body,
          c.createdAt,
          c.parentUri ?? null,
          c.range?.start ?? null,
          c.range?.end ?? null,
          Date.now(),
        );
      console.log("[docCommentStore] upsertComment succeeded");
    } catch (err) {
      console.error("[docCommentStore] upsertComment error:", err);
    }
  }

  // threadsForDoc returns the document's comment threads: top-level comments
  // (no parent) oldest first, each with its direct replies oldest first. A
  // reply whose parent isn't indexed is promoted to top level so no comment is
  // dropped; it re-nests on a later rebuild once the parent is indexed.
  threadsForDoc(docSpace: string): CommentView[] {
    const rows = this.db
      .prepare(
        `SELECT uri, author, body, created_at AS createdAt,
                parent_uri AS parentUri, range_start AS rangeStart, range_end AS rangeEnd
         FROM doc_comments
         WHERE doc_space = ?
         ORDER BY created_at ASC`,
      )
      .all(docSpace) as {
      uri: string;
      author: string;
      body: string;
      createdAt: string;
      parentUri: string | null;
      rangeStart: string | null;
      rangeEnd: string | null;
    }[];

    const byUri = new Map<string, CommentView>();
    const replies: { parentUri: string; reply: ReplyView }[] = [];

    for (const r of rows) {
      const base: ReplyView = {
        uri: r.uri,
        author: r.author,
        body: r.body,
        createdAt: r.createdAt,
      };
      if (r.parentUri !== null) {
        replies.push({ parentUri: r.parentUri, reply: base });
      } else {
        const view: CommentView = { ...base, replies: [] };
        if (r.rangeStart !== null && r.rangeEnd !== null) {
          view.range = { start: r.rangeStart, end: r.rangeEnd };
        }
        byUri.set(r.uri, view);
      }
    }

    const orphans: CommentView[] = [];
    for (const { parentUri, reply } of replies) {
      const parent = byUri.get(parentUri);
      if (parent) {
        parent.replies.push(reply);
      } else {
        orphans.push({ ...reply, replies: [] });
      }
    }

    // Rows came back oldest-first, so top-level order and each replies array
    // preserve that ordering.
    return [...byUri.values(), ...orphans];
  }
}

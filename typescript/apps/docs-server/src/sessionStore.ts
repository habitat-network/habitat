import { randomBytes } from "node:crypto";
import type { DatabaseSync } from "node:sqlite";

// SessionStore holds the server sessions that back the docsv2 frontend's cookie
// auth. A session maps an opaque, unguessable token (stored in the browser's
// cookie) to the user DID that authenticated via sap's OAuth flow. The token is
// server-generated and looked up here on every request, so no signing is
// needed. Sessions are persisted to sqlite (shared with the doc stores) so they
// survive restarts.
export class SessionStore {
  private db: DatabaseSync;

  constructor(db: DatabaseSync) {
    this.db = db;
    this.db.exec(`
      CREATE TABLE IF NOT EXISTS server_sessions (
        token      TEXT PRIMARY KEY,
        did        TEXT NOT NULL,
        created_at INTEGER NOT NULL
      );
    `);
  }

  // create mints a new session for the user and returns its token.
  create(did: string): string {
    const token = randomBytes(32).toString("base64url");
    this.db
      .prepare(
        `INSERT INTO server_sessions (token, did, created_at) VALUES (?, ?, ?)`,
      )
      .run(token, did, Date.now());
    return token;
  }

  // didFor returns the user DID a session token authenticates as, or undefined
  // if the token is unknown.
  didFor(token: string): string | undefined {
    const row = this.db
      .prepare(`SELECT did FROM server_sessions WHERE token = ?`)
      .get(token);
    return row ? String(row.did) : undefined;
  }

  // remove deletes a session (logout).
  remove(token: string): void {
    this.db.prepare(`DELETE FROM server_sessions WHERE token = ?`).run(token);
  }
}

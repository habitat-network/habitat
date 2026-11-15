/**
 * The SQL surface the framework needs. It is the subset of Cloudflare's
 * D1Database that the slice uses, so a D1 binding (or a Durable Object's
 * SQLite behind a small adapter) can be passed as is.
 */
export interface SqlStatement {
  bind(...values: unknown[]): SqlStatement;
  all<T = Record<string, unknown>>(): Promise<{ results: T[] }>;
  run(): Promise<unknown>;
}

export interface SqlDatabase {
  prepare(sql: string): SqlStatement;
  /** Runs the statements as one transaction. */
  batch(statements: SqlStatement[]): Promise<unknown>;
}

/** runBatch runs statements in order as one transaction; a no-op when empty. */
export async function runBatch(
  db: SqlDatabase,
  statements: SqlStatement[],
): Promise<void> {
  if (statements.length > 0) await db.batch(statements);
}

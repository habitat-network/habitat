import { DatabaseSync } from "node:sqlite";
import type { SqlDatabase, SqlStatement } from "../app/sql.js";

type Param = null | number | bigint | string | Uint8Array;

// memoryDatabase is an in-memory SQLite behind the framework's SqlDatabase
// interface, standing in for a D1 binding in tests.
export function memoryDatabase(): SqlDatabase {
  const sqlite = new DatabaseSync(":memory:");
  const statement = (sql: string, params: Param[]): SqlStatement => ({
    bind: (...values) => statement(sql, values as Param[]),
    all: async <T>() => ({
      results: sqlite.prepare(sql).all(...params) as T[],
    }),
    run: async () => {
      sqlite.prepare(sql).run(...params);
    },
  });
  return {
    prepare: (sql) => statement(sql, []),
    batch: async (statements) => {
      sqlite.exec("BEGIN");
      try {
        for (const s of statements) await s.run();
        sqlite.exec("COMMIT");
      } catch (err) {
        sqlite.exec("ROLLBACK");
        throw err;
      }
    },
  };
}

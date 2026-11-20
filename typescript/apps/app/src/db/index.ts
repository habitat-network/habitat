import { drizzle } from "drizzle-orm/d1";
import * as schema from "./schema";

export function getDb(env: { DB: D1Database }) {
  return drizzle(env.DB, { schema });
}

export type Db = ReturnType<typeof getDb>;

export * from "./docs";
export * from "./files";
export * from "./memberships";
export * from "./apps";

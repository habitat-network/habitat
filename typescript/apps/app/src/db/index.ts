import { drizzle } from "drizzle-orm/d1";
import { oauthSessions, oauthStates } from "./schema";

export function getDb(env: { DB: D1Database }) {
  return drizzle(env.DB, { schema: { oauthStates, oauthSessions } });
}

export type Db = ReturnType<typeof getDb>;

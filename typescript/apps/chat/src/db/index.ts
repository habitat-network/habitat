import { drizzle } from "drizzle-orm/d1";
import { eq, inArray } from "drizzle-orm";
import {
  connectedOrgs,
  oauthSessions,
  oauthStates,
  syncRepos,
  syncSpaces,
} from "./schema";

export function getDb(env: { DB: D1Database }) {
  return drizzle(env.DB, {
    schema: {
      oauthStates,
      oauthSessions,
      connectedOrgs,
      syncSpaces,
      syncRepos,
    },
  });
}

export type Db = ReturnType<typeof getDb>;

// upsertConnectedOrg records that this deployment now holds the org's own
// OAuth session (see the org connect flow in src/routes/oauth.callback.ts).
export async function upsertConnectedOrg(
  db: Db,
  org: { orgDid: string; name: string; connectedBy: string },
): Promise<void> {
  const row = { ...org, connectedAt: Date.now() };
  await db
    .insert(connectedOrgs)
    .values(row)
    .onConflictDoUpdate({
      target: connectedOrgs.orgDid,
      set: {
        name: row.name,
        connectedBy: row.connectedBy,
        connectedAt: row.connectedAt,
      },
    });
}

export async function deleteConnectedOrg(db: Db, orgDid: string) {
  await db.delete(connectedOrgs).where(eq(connectedOrgs.orgDid, orgDid));
}

// connectedOrgNames maps each of orgIds this deployment holds a session for
// to its cached name.
export async function connectedOrgNames(
  db: Db,
  orgIds: string[],
): Promise<Map<string, string>> {
  if (orgIds.length === 0) return new Map();
  const rows = await db
    .select({ orgDid: connectedOrgs.orgDid, name: connectedOrgs.name })
    .from(connectedOrgs)
    .where(inArray(connectedOrgs.orgDid, orgIds));
  return new Map(rows.map((r) => [r.orgDid, r.name]));
}

export async function isConnectedOrg(db: Db, orgDid: string) {
  const [row] = await db
    .select({ orgDid: connectedOrgs.orgDid })
    .from(connectedOrgs)
    .where(eq(connectedOrgs.orgDid, orgDid))
    .limit(1);
  return row !== undefined;
}

// connectedOrgIds lists every org this deployment holds a session for.
export async function connectedOrgIds(db: Db): Promise<string[]> {
  const rows = await db
    .select({ orgDid: connectedOrgs.orgDid })
    .from(connectedOrgs);
  return rows.map((r) => r.orgDid);
}

// Queries over the synced apps table (see src/server/apps/sink.ts).
import { desc, eq } from "drizzle-orm";
import { apps } from "./schema";
import type { Db } from ".";

export type AppRow = typeof apps.$inferSelect;

export interface AppSummary {
  uri: string;
  space: string;
  host: string;
  name: string;
  slug: string;
  description: string | null;
  entrypoint: string;
  codeSize: number;
  version: number;
  createdBy: string | null;
  updatedBy: string | null;
  createdAt: number;
  updatedAt: number;
}

// appsForOrg lists a group's apps, most recently deployed first.
export async function appsForOrg(
  db: Db,
  orgDid: string,
): Promise<AppSummary[]> {
  return db
    .select({
      uri: apps.uri,
      space: apps.space,
      host: apps.host,
      name: apps.name,
      slug: apps.slug,
      description: apps.description,
      entrypoint: apps.entrypoint,
      codeSize: apps.codeSize,
      version: apps.version,
      createdBy: apps.createdBy,
      updatedBy: apps.updatedBy,
      createdAt: apps.createdAt,
      updatedAt: apps.updatedAt,
    })
    .from(apps)
    .where(eq(apps.orgDid, orgDid))
    .orderBy(desc(apps.updatedAt), desc(apps.uri));
}

export async function appByUri(db: Db, uri: string) {
  const [row] = await db.select().from(apps).where(eq(apps.uri, uri)).limit(1);
  return row;
}

// appByHost finds the app a run URL is for. The oldest wins if two groups'
// apps ever share a label, so a later one can't take over a running URL.
export async function appByHost(
  db: Db,
  host: string,
): Promise<AppRow | undefined> {
  const [row] = await db
    .select()
    .from(apps)
    .where(eq(apps.host, host))
    .orderBy(apps.createdAt)
    .limit(1);
  return row;
}

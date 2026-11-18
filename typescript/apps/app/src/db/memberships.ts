// Queries over the synced memberships table.
import { and, eq, notInArray } from "drizzle-orm";
import { memberships } from "./schema";
import type { Db } from ".";

export interface MembershipRow {
  orgDid: string;
  memberDid: string;
  roles: string[];
}

// orgsOf lists the groups a member currently belongs to, as last synced.
export async function orgsOf(db: Db, memberDid: string): Promise<string[]> {
  const rows = await db
    .select({ orgDid: memberships.orgDid })
    .from(memberships)
    .where(eq(memberships.memberDid, memberDid));
  return rows.map((r) => r.orgDid);
}

export async function isMemberOf(
  db: Db,
  orgDid: string,
  memberDid: string,
): Promise<boolean> {
  const rows = await db
    .select({ orgDid: memberships.orgDid })
    .from(memberships)
    .where(
      and(eq(memberships.orgDid, orgDid), eq(memberships.memberDid, memberDid)),
    )
    .limit(1);
  return rows.length > 0;
}

// memberDidsOf lists a group's members, as last synced.
export async function memberDidsOf(db: Db, orgDid: string): Promise<string[]> {
  const rows = await db
    .select({ memberDid: memberships.memberDid })
    .from(memberships)
    .where(eq(memberships.orgDid, orgDid));
  return rows.map((r) => r.memberDid);
}

export function upsertMembershipStatement(db: Db, row: MembershipRow) {
  const values = {
    orgDid: row.orgDid,
    memberDid: row.memberDid,
    roles: JSON.stringify(row.roles),
    updatedAt: Date.now(),
  };
  return db
    .insert(memberships)
    .values(values)
    .onConflictDoUpdate({
      target: [memberships.orgDid, memberships.memberDid],
      set: { roles: values.roles, updatedAt: values.updatedAt },
    });
}

export async function upsertMembership(
  db: Db,
  row: MembershipRow,
): Promise<void> {
  await upsertMembershipStatement(db, row);
}

// reconcileMemberships makes a member's rows equal the groups pear confirmed
// for them: the bootstrap at sign-in, before (and alongside) the syncer
// keeping them current.
export async function reconcileMemberships(
  db: Db,
  memberDid: string,
  confirmed: readonly { orgDid: string; roles: string[] }[],
): Promise<void> {
  const keep = confirmed.map((c) => c.orgDid);
  await db.batch([
    db
      .delete(memberships)
      .where(
        and(
          eq(memberships.memberDid, memberDid),
          ...(keep.length > 0 ? [notInArray(memberships.orgDid, keep)] : []),
        ),
      ),
    ...confirmed.map((c) => upsertMembershipStatement(db, { ...c, memberDid })),
  ]);
}

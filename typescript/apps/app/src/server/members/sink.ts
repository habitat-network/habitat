import { and, eq, inArray, notInArray } from "drizzle-orm";
import type { LexMap } from "@atproto/lex";
import { AtUri, type DidString } from "@atproto/syntax";
import type { PromiseRepoBatch, PromiseSyncSink } from "internal/spaceSync";
import type { Db } from "@/db";
import { upsertMembershipStatement } from "@/db/memberships";
import { memberships } from "@/db/schema";

// The space each group keeps its roster in, and the record in it that says a
// member belongs: keyed by the member's DID, written by the group itself.
export const MEMBERS_SPACE_TYPE = "community.opensocial.members";
export const MEMBERSHIP_COLLECTION = "community.opensocial.membership";

function rolesOf(value: LexMap): string[] {
  const roles = (value as { roles?: unknown }).roles;
  return Array.isArray(roles)
    ? roles.filter((r): r is string => typeof r === "string")
    : [];
}

// MembershipSink is internal/spaceSync's SyncSink for groups' members
// spaces: it keeps the memberships table equal to each group's
// community.opensocial.membership records. Only records in the group's own
// repo count (the group is the space authority and the only writer of its
// roster); members' own records in the space, such as their acceptance and
// profile, are ignored.
//
// Each batch is one D1 batch (a single transaction), and upserts and
// deletes are keyed by (group, member), so a replay is a no-op.
export class MembershipSink implements PromiseSyncSink {
  constructor(private opts: { db: Db }) {}

  async apply(batch: PromiseRepoBatch, signal: AbortSignal): Promise<void> {
    const { db } = this.opts;
    const org = new AtUri(batch.space).spaceDid as DidString;
    switch (batch._tag) {
      case "Ops": {
        if (batch.did !== org) return;
        const upserts: { member: string; roles: string[] }[] = [];
        const deletes: string[] = [];
        for (const change of batch.changes) {
          if (change.collection !== MEMBERSHIP_COLLECTION) continue;
          if (change.cid === null) deletes.push(change.rkey);
          else if (change.value)
            upserts.push({ member: change.rkey, roles: rolesOf(change.value) });
        }
        signal.throwIfAborted();
        const statements = [
          ...(deletes.length > 0
            ? [
                db
                  .delete(memberships)
                  .where(
                    and(
                      eq(memberships.orgDid, org),
                      inArray(memberships.memberDid, deletes),
                    ),
                  ),
              ]
            : []),
          ...upserts.map((u) =>
            upsertMembershipStatement(db, {
              orgDid: org,
              memberDid: u.member,
              roles: u.roles,
            }),
          ),
        ];
        const [first, ...rest] = statements;
        if (first) await db.batch([first, ...rest]);
        break;
      }
      case "Reset": {
        if (batch.did !== org) {
          // Drain so the records are verified like any other batch's.
          for await (const _ of batch.records) signal.throwIfAborted();
          return;
        }
        const roster: { member: string; roles: string[] }[] = [];
        for await (const record of batch.records) {
          if (record.collection !== MEMBERSHIP_COLLECTION) continue;
          roster.push({ member: record.rkey, roles: rolesOf(record.record) });
        }
        signal.throwIfAborted();
        // Anyone the reset didn't list is no longer a member.
        const keep = roster.map((r) => r.member);
        await db.batch([
          db
            .delete(memberships)
            .where(
              and(
                eq(memberships.orgDid, org),
                ...(keep.length > 0
                  ? [notInArray(memberships.memberDid, keep)]
                  : []),
              ),
            ),
          ...roster.map((r) =>
            upsertMembershipStatement(db, {
              orgDid: org,
              memberDid: r.member,
              roles: r.roles,
            }),
          ),
        ]);
        break;
      }
      case "RepoRemoved":
        if (batch.did === org) {
          await db.delete(memberships).where(eq(memberships.orgDid, org));
        }
        break;
      case "SpaceDeleted":
        await db.delete(memberships).where(eq(memberships.orgDid, org));
        break;
    }
  }
}

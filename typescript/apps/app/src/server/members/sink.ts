import { and, eq } from "drizzle-orm";
import type { LexMap } from "@atproto/lex";
import { community } from "api";
import { AtUri, type DidString } from "@atproto/syntax";
import { Effect } from "effect";
import type { Domain } from "../stream/domain";
import type { Db } from "@/db";
import { upsertMembershipStatement } from "@/db/memberships";
import { memberships } from "@/db/schema";

// The space each group keeps its roster in, and the record in it that says a
// member belongs: keyed by the member's DID, written by the group itself.
export const MEMBERS_SPACE_TYPE = "community.opensocial.members";
export const MEMBERSHIP_COLLECTION = community.opensocial.membership.$nsid;

function rolesOf(value: LexMap): string[] {
  const roles = (value as { roles?: unknown }).roles;
  return Array.isArray(roles)
    ? roles.filter((r): r is string => typeof r === "string")
    : [];
}

// membersDomain keeps the memberships table equal to each group's
// community.opensocial.membership records. Only records in the group's own
// repo count (the group is the space authority and the only writer of its
// roster); members' own records in the space, such as their acceptance and
// profile, are ignored. Upserts and deletes are keyed by (group, member), so
// a replay is a no-op.
export function membersDomain(db: Db): Domain {
  return {
    collections: [
      {
        collection: MEMBERSHIP_COLLECTION,
        handlers: {
          put: (event) => {
            const org = new AtUri(event.space).spaceDid as DidString;
            if (event.did !== org) return Effect.void;
            return Effect.tryPromise(async () => {
              await upsertMembershipStatement(db, {
                orgDid: org,
                memberDid: event.rkey,
                roles: rolesOf(event.record),
              });
            });
          },
          del: (event) => {
            const org = new AtUri(event.space).spaceDid as DidString;
            if (event.did !== org) return Effect.void;
            return Effect.tryPromise(async () => {
              await db
                .delete(memberships)
                .where(
                  and(
                    eq(memberships.orgDid, org),
                    eq(memberships.memberDid, event.rkey),
                  ),
                );
            });
          },
        },
      },
    ],
    spaceDeleted: (event) =>
      Effect.tryPromise(async () => {
        const org = new AtUri(event.space).spaceDid as DidString;
        await db.delete(memberships).where(eq(memberships.orgDid, org));
      }),
  };
}

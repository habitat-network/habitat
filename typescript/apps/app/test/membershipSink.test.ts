import { env } from "cloudflare:workers";
import { jsonToLex, parseCid, type LexMap } from "@atproto/lex";
import type { DidString, SpaceRefString } from "@atproto/syntax";
import type { RepoBatch } from "../src/server/stream/batch";
import { beforeEach, describe, expect, it } from "vitest";
import {
  getDb,
  isMemberOf,
  memberDidsOf,
  orgsOf,
  reconcileMemberships,
} from "@/db";
import { memberships } from "@/db/schema";
import { MEMBERSHIP_COLLECTION, MembershipSink } from "@/server/members/sink";

const ORG = "did:web:org.example" as DidString;
const OTHER_ORG = "did:web:other.example" as DidString;
const ALICE = "did:plc:alice";
const BOB = "did:plc:bob";
const SPACE =
  `at://${ORG}/space/community.opensocial.members/self` as SpaceRefString;
const RECORD_CID =
  "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm";

const membership = (...roles: string[]) =>
  jsonToLex({
    $type: MEMBERSHIP_COLLECTION,
    roles,
    updatedAt: "2026-11-18T12:00:00.000Z",
  }) as LexMap;

describe("MembershipSink", () => {
  const db = getDb(env);
  const signal = new AbortController().signal;
  const sink = new MembershipSink({ db });

  beforeEach(async () => {
    await db.delete(memberships);
  });

  const ops = (
    changes: Extract<RepoBatch, { _tag: "Ops" }>["changes"],
    did: DidString = ORG,
  ): RepoBatch => ({
    _tag: "Ops",
    space: SPACE,
    did,
    rev: "r1",
    changes,
  });

  const created = (member: string, ...roles: string[]) => ({
    uri: `${SPACE}/${ORG}/${MEMBERSHIP_COLLECTION}/${member}`,
    collection: MEMBERSHIP_COLLECTION,
    rkey: member,
    cid: parseCid(RECORD_CID),
    value: membership(...roles),
  });
  const deleted = (member: string) => ({
    uri: `${SPACE}/${ORG}/${MEMBERSHIP_COLLECTION}/${member}`,
    collection: MEMBERSHIP_COLLECTION,
    rkey: member,
    cid: null,
  });

  it("records a member when their membership record is created", async () => {
    await sink.apply(ops([created(ALICE, "admin")]), signal);
    expect(await isMemberOf(db, ORG, ALICE)).toBe(true);
    expect(await orgsOf(db, ALICE)).toEqual([ORG]);
    expect(await isMemberOf(db, ORG, BOB)).toBe(false);
  });

  it("drops a member when their membership record is deleted", async () => {
    await sink.apply(ops([created(ALICE), created(BOB)]), signal);
    await sink.apply(ops([deleted(BOB)]), signal);
    expect(await memberDidsOf(db, ORG)).toEqual([ALICE]);
  });

  it("is a no-op when a batch is replayed", async () => {
    const batch = ops([created(ALICE, "member")]);
    await sink.apply(batch, signal);
    await sink.apply(batch, signal);
    expect(await memberDidsOf(db, ORG)).toEqual([ALICE]);
  });

  it("ignores other collections and records not in the group's own repo", async () => {
    await sink.apply(
      ops([
        {
          ...created(ALICE),
          collection: "community.opensocial.acceptance",
        },
      ]),
      signal,
    );
    await sink.apply(ops([created(BOB)], ALICE as DidString), signal);
    expect(await memberDidsOf(db, ORG)).toEqual([]);
  });

  it("clears the roster when the space is deleted", async () => {
    await sink.apply(ops([created(ALICE)]), signal);
    await sink.apply({ _tag: "SpaceDeleted", space: SPACE }, signal);
    expect(await memberDidsOf(db, ORG)).toEqual([]);
  });
});

describe("reconcileMemberships", () => {
  const db = getDb(env);
  beforeEach(async () => {
    await db.delete(memberships);
  });

  it("makes a member's groups equal what pear confirmed", async () => {
    await reconcileMemberships(db, ALICE, [
      { orgDid: ORG, roles: ["member"] },
      { orgDid: OTHER_ORG, roles: [] },
    ]);
    await reconcileMemberships(db, ALICE, [{ orgDid: OTHER_ORG, roles: [] }]);
    expect(await orgsOf(db, ALICE)).toEqual([OTHER_ORG]);
  });

  it("leaves other members' rows alone", async () => {
    await reconcileMemberships(db, BOB, [{ orgDid: ORG, roles: [] }]);
    await reconcileMemberships(db, ALICE, []);
    expect(await orgsOf(db, BOB)).toEqual([ORG]);
  });
});

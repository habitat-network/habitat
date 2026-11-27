import { env } from "cloudflare:workers";
import { jsonToLex, type LexMap } from "@atproto/lex";
import type { DidString } from "@atproto/syntax";
import { beforeEach, describe, expect, it } from "vitest";
import {
  getDb,
  isMemberOf,
  memberDidsOf,
  orgsOf,
  reconcileMemberships,
} from "@/db";
import { memberships } from "@/db/schema";
import { MEMBERSHIP_COLLECTION } from "@/server/members/sink";
import { delEvent, indexEvents, putEvent, spaceDeletedEvent } from "./events";

const ORG = "did:web:org.example" as DidString;
const OTHER_ORG = "did:web:other.example" as DidString;
const ALICE = "did:plc:alice";
const BOB = "did:plc:bob";
const SPACE = `at:space/${ORG}/org.atgrp.members/self`;

const membership = (...roles: string[]) =>
  jsonToLex({
    $type: MEMBERSHIP_COLLECTION,
    roles,
    updatedAt: "2026-11-18T12:00:00.000Z",
  }) as LexMap;

describe("roster indexing", () => {
  const db = getDb(env);

  beforeEach(async () => {
    await db.delete(memberships);
  });

  const created = (member: string, roles: string[] = [], did: string = ORG) =>
    putEvent(SPACE, did, MEMBERSHIP_COLLECTION, member, membership(...roles));
  const deleted = (member: string) =>
    delEvent(SPACE, ORG, MEMBERSHIP_COLLECTION, member);

  it("records a member when their membership record is created", async () => {
    await indexEvents(created(ALICE, ["admin"]));
    expect(await isMemberOf(db, ORG, ALICE)).toBe(true);
    expect(await orgsOf(db, ALICE)).toEqual([ORG]);
    expect(await isMemberOf(db, ORG, BOB)).toBe(false);
  });

  it("drops a member when their membership record is deleted", async () => {
    await indexEvents(created(ALICE), created(BOB), deleted(BOB));
    expect(await memberDidsOf(db, ORG)).toEqual([ALICE]);
  });

  it("is a no-op when an event is replayed", async () => {
    await indexEvents(created(ALICE, ["member"]), created(ALICE, ["member"]));
    expect(await memberDidsOf(db, ORG)).toEqual([ALICE]);
  });

  it("ignores other collections and records not in the group's own repo", async () => {
    await indexEvents(
      putEvent(SPACE, ORG, "org.atgrp.acceptance", ALICE, membership()),
      created(BOB, [], ALICE),
    );
    expect(await memberDidsOf(db, ORG)).toEqual([]);
  });

  it("clears the roster when the space is deleted", async () => {
    await indexEvents(created(ALICE), spaceDeletedEvent(SPACE));
    expect(await memberDidsOf(db, ORG)).toEqual([]);
  });

  it("leaves rosters alone when another kind of space is deleted", async () => {
    await indexEvents(
      created(ALICE),
      spaceDeletedEvent(`at:space/${ORG}/network.habitat.docs/3kdoc`),
    );
    expect(await memberDidsOf(db, ORG)).toEqual([ALICE]);
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

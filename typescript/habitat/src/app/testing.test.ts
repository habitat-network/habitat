import { describe, expect, it } from "vitest";
import { memoryDatabase } from "../test/sqlite.js";
import { defineApp, mutation, query, testHarness } from "./index.js";

const ORG = "did:plc:org";
const ALICE = "did:plc:alice";
const BOB = "did:plc:bob";
const NOTE = "com.example.notes.note";
const TEAM = `at://${ORG}/space/com.example.notes/team`;
const BOARD = `at://${ORG}/space/com.example.notes/board`;

const app = defineApp({
  name: "notes",
  spaceTypes: ["com.example.notes"],
  collections: { [NOTE]: { fields: { text: "$.text" } } },
});

const notes = query({
  handler: async (ctx) =>
    (await ctx.db.query<{ text: string }>(NOTE).orderBy("text").collect()).map(
      (r) => r.value.text,
    ),
});

// Functions only receive args that went through a parser (zod in real apps).
const unchecked = <A>() => ({ parse: (input: unknown) => input as A });

const addNote = mutation({
  args: unchecked<{ space: string; text: string }>(),
  handler: (ctx, { space, text }) =>
    ctx.spaces.put({ space, collection: NOTE, record: { text } }),
});

const removeNote = mutation({
  args: unchecked<{ space: string; rkey: string }>(),
  handler: (ctx, { space, rkey }) =>
    ctx.spaces.delete({ space, collection: NOTE, rkey }),
});

// seeded: alice sits on the board, bob is a plain member; the team space is
// open to members and the board space to the board.
async function seeded() {
  const t = await testHarness(app, { db: memoryDatabase() });
  await t.member(ORG, ALICE, ["member", "board"]);
  await t.member(ORG, BOB, ["member"]);
  await t.access(TEAM, ["member"]);
  await t.access(BOARD, ["board"]);
  await t.record(TEAM, BOB, NOTE, "1", { text: "standup at ten" });
  await t.record(BOARD, ALICE, NOTE, "2", { text: "acquisition" });
  return t;
}

describe("testHarness", () => {
  it("shows each viewer only the spaces their roles open", async () => {
    const t = await seeded();
    expect(await t.as(ALICE).run(notes)).toEqual([
      "acquisition",
      "standup at ten",
    ]);
    expect(await t.as(BOB).run(notes)).toEqual(["standup at ten"]);
    expect(await t.as("did:plc:stranger").run(notes)).toEqual([]);
  });

  it("follows membership, access and record changes", async () => {
    const t = await seeded();
    await t.member(ORG, BOB, ["member", "board"]);
    expect(await t.as(BOB).run(notes)).toHaveLength(2);
    await t.access(BOARD, []);
    expect(await t.as(BOB).run(notes)).toEqual(["standup at ten"]);
    await t.member(ORG, BOB, []);
    expect(await t.as(BOB).run(notes)).toEqual([]);
    await t.remove(TEAM, BOB, NOTE, "1");
    expect(await t.as(ALICE).run(notes)).toEqual([]);
  });

  it("accepts writes to spaces the viewer's roles open and records them", async () => {
    const t = await seeded();
    const written = await t
      .as(BOB)
      .run(addNote, { space: TEAM, text: "bring snacks" });
    expect(written.uri).toBe(`${TEAM}/${BOB}/${NOTE}/${written.rkey}`);
    expect(await t.as(ALICE).run(notes)).toContain("bring snacks");
    await t.as(BOB).run(removeNote, { space: TEAM, rkey: written.rkey });
    expect(await t.as(ALICE).run(notes)).not.toContain("bring snacks");
    expect(t.writes.map((w) => [w.viewer, w.nsid, w.accepted])).toEqual([
      [BOB, "com.atproto.space.putRecord", true],
      [BOB, "com.atproto.space.deleteRecord", true],
    ]);
  });

  it("refuses writes the way the Habitat instance would", async () => {
    const t = await seeded();
    await expect(
      t.as(BOB).run(addNote, { space: BOARD, text: "let me in" }),
    ).rejects.toThrow(/403/);
    expect(t.writes).toMatchObject([{ viewer: BOB, accepted: false }]);
    expect(await t.as(ALICE).run(notes)).not.toContain("let me in");
  });
});

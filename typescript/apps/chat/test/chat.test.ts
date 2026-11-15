import { env } from "cloudflare:workers";
import { createRuntime, type SliceBatch } from "@habitat-network/habitat";
import { beforeEach, describe, expect, it } from "vitest";
import { CHANNEL, chat, MESSAGE } from "@/habitat/app";
import {
  deleteMessage,
  listChannels,
  listMessages,
  sendMessage,
} from "@/habitat/functions";

// Chat's Habitat app functions against a real (local) D1, with the slice
// filled the way the syncer fills it: batches applied to the runtime's sink.

const ORG = "did:web:org.example";
const OTHER_ORG = "did:web:other.example";
const ALICE = "did:plc:alice";
const BOB = "did:plc:bob";
const MEMBERS = `at://${ORG}/space/community.opensocial.members/self`;
const GENERAL = `at://${ORG}/space/network.habitat.chat/general`;
const LEADS = `at://${ORG}/space/network.habitat.chat/leads`;
const ELSEWHERE = `at://${OTHER_ORG}/space/network.habitat.chat/general`;

function put(
  space: string,
  did: string,
  collection: string,
  rkey: string,
  value: unknown,
): SliceBatch {
  return {
    _tag: "Ops",
    space,
    did,
    changes: [
      {
        uri: `${space}/${did}/${collection}/${rkey}`,
        collection,
        rkey,
        cid: { toString: () => `cid-${rkey}` },
        value,
      },
    ],
  };
}

// Pear's answers to the writes a mutation makes, recorded for assertions.
const writes: Array<{ path: string; body: Record<string, string> }> = [];
let refuse = false;

function runtime() {
  return createRuntime(chat, {
    db: env.DB,
    agent: async () => ({
      fetchHandler: async (path, init) => {
        const body = JSON.parse(String(init.body));
        writes.push({ path, body });
        if (refuse) return new Response("not a writer", { status: 403 });
        return Response.json({
          uri: `${body.space}/${body.repo}/${body.collection}/3kmsg`,
          cid: "cid-3kmsg",
        });
      },
    }),
  });
}

describe("chat functions", () => {
  beforeEach(async () => {
    writes.length = 0;
    refuse = false;
    const rt = runtime();
    await rt.migrate();
    for (const table of [
      "hab_records",
      "hab_space_roles",
      "hab_member_roles",
    ]) {
      await env.DB.prepare(`DELETE FROM ${table}`).run();
    }
    const { sink } = rt;
    // Alice is a member and a lead; Bob is a plain member.
    await sink.apply(
      put(MEMBERS, ORG, "community.opensocial.membership", ALICE, {
        roles: ["member", "lead"],
      }),
    );
    await sink.apply(
      put(MEMBERS, ORG, "community.opensocial.membership", BOB, {
        roles: ["member"],
      }),
    );
    for (const [space, roles, name] of [
      [GENERAL, ["member"], "general"],
      [LEADS, ["lead"], "leads"],
      [ELSEWHERE, ["member"], "elsewhere"],
    ] as const) {
      const org = space === ELSEWHERE ? OTHER_ORG : ORG;
      await sink.apply(
        put(space, org, "community.opensocial.access", "self", { roles }),
      );
      await sink.apply(put(space, org, CHANNEL, "self", { name }));
    }
    await sink.apply(
      put(GENERAL, BOB, MESSAGE, "1", {
        text: "morning",
        createdAt: "2026-11-13T09:00:00Z",
      }),
    );
    await sink.apply(
      put(LEADS, ALICE, MESSAGE, "2", {
        text: "budget",
        createdAt: "2026-11-13T09:05:00Z",
      }),
    );
  });

  it("lists only the org's channels the viewer can read", async () => {
    const names = async (viewer: string) =>
      (await runtime().run(listChannels, { viewer, args: { org: ORG } })).map(
        (c) => c.name,
      );
    expect(await names(ALICE)).toEqual(["general", "leads"]);
    expect(await names(BOB)).toEqual(["general"]);
    // Another org's channel is never listed, even though its role is also
    // called "member".
    expect(
      await runtime().run(listChannels, {
        viewer: BOB,
        args: { org: OTHER_ORG },
      }),
    ).toEqual([]);
  });

  it("hides a channel's messages from a member without its role", async () => {
    const texts = async (viewer: string, space: string) =>
      (await runtime().run(listMessages, { viewer, args: { space } })).map(
        (m) => m.text,
      );
    expect(await texts(ALICE, LEADS)).toEqual(["budget"]);
    expect(await texts(BOB, LEADS)).toEqual([]);
    expect(await texts(BOB, GENERAL)).toEqual(["morning"]);
    expect(await texts(BOB, ELSEWHERE)).toEqual([]);
  });

  it("returns the latest messages oldest first", async () => {
    const { sink } = runtime();
    for (let i = 0; i < 5; i++) {
      await sink.apply(
        put(GENERAL, ALICE, MESSAGE, `m${i}`, {
          text: `n${i}`,
          createdAt: `2026-11-13T10:00:0${i}Z`,
        }),
      );
    }
    const page = await runtime().run(listMessages, {
      viewer: ALICE,
      args: { space: GENERAL, limit: 3 },
    });
    expect(page.map((m) => m.text)).toEqual(["n2", "n3", "n4"]);
    expect(page[0]).toMatchObject({ author: ALICE, rkey: "m2" });
  });

  it("sends as the viewer through the Habitat instance and reads it back", async () => {
    await runtime().run(sendMessage, {
      viewer: BOB,
      args: { space: GENERAL, text: "  hello  " },
    });
    expect(writes).toHaveLength(1);
    expect(writes[0]).toMatchObject({
      path: "/xrpc/com.atproto.space.putRecord",
      body: { space: GENERAL, repo: BOB, collection: MESSAGE },
    });
    const messages = await runtime().run(listMessages, {
      viewer: ALICE,
      args: { space: GENERAL },
    });
    expect(messages.map((m) => [m.author, m.text])).toEqual([
      [BOB, "morning"],
      [BOB, "hello"],
    ]);
  });

  it("won't send to a channel the viewer can't read, or an empty message", async () => {
    await expect(
      runtime().run(sendMessage, {
        viewer: BOB,
        args: { space: LEADS, text: "let me in" },
      }),
    ).rejects.toThrow("Channel not found");
    await expect(
      runtime().run(sendMessage, {
        viewer: BOB,
        args: { space: GENERAL, text: "   " },
      }),
    ).rejects.toThrow();
    expect(writes).toEqual([]);
  });

  it("keeps the slice unchanged when the Habitat instance refuses a write", async () => {
    refuse = true;
    await expect(
      runtime().run(sendMessage, {
        viewer: BOB,
        args: { space: GENERAL, text: "hello" },
      }),
    ).rejects.toThrow(/403/);
    const messages = await runtime().run(listMessages, {
      viewer: BOB,
      args: { space: GENERAL },
    });
    expect(messages.map((m) => m.text)).toEqual(["morning"]);
  });

  it("deletes the viewer's own message", async () => {
    await runtime().run(deleteMessage, {
      viewer: BOB,
      args: { space: GENERAL, rkey: "1" },
    });
    expect(writes[0]).toMatchObject({
      path: "/xrpc/com.atproto.space.deleteRecord",
      body: { space: GENERAL, repo: BOB, collection: MESSAGE, rkey: "1" },
    });
    expect(
      await runtime().run(listMessages, {
        viewer: BOB,
        args: { space: GENERAL },
      }),
    ).toEqual([]);
  });

  it("drops a member's access as soon as their membership changes", async () => {
    await runtime().sink.apply(
      put(MEMBERS, ORG, "community.opensocial.membership", ALICE, {
        roles: ["member"],
      }),
    );
    expect(
      await runtime().run(listMessages, {
        viewer: ALICE,
        args: { space: LEADS },
      }),
    ).toEqual([]);
  });
});

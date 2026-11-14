import { mutation, parseSpace, query } from "@habitat-network/habitat";
import { z } from "zod";
import { CHANNEL, CHANNEL_SPACE_TYPE, MESSAGE } from "./app";

// Chat's queries and mutations. Each runs as the signed-in member
// (ctx.viewer); ctx.db only ever returns records from channels that member
// can read, so none of these check membership themselves.

export interface ChannelView {
  space: string;
  name: string;
  topic: string | null;
}

export interface MessageView {
  uri: string;
  rkey: string;
  author: string;
  text: string;
  createdAt: string;
}

interface ChannelRecord {
  name?: string;
  topic?: string;
}

interface MessageRecord {
  text?: string;
  createdAt?: string;
}

export const MAX_MESSAGE_LENGTH = 4000;
const DEFAULT_PAGE = 100;

// listChannels lists the channels of one org the viewer can read. A channel
// whose descriptor record hasn't synced yet is listed under its space key.
export const listChannels = query({
  args: z.object({ org: z.string() }),
  handler: async (ctx, { org }): Promise<ChannelView[]> => {
    const spaces = await ctx.db.spaces({
      type: CHANNEL_SPACE_TYPE,
      authority: org,
    });
    const records = await ctx.db
      .query<ChannelRecord>(CHANNEL)
      .where("repo", "=", org)
      .collect();
    const bySpace = new Map(records.map((r) => [r.space, r.value]));
    return spaces
      .map((space) => {
        const record = bySpace.get(space);
        return {
          space,
          name: record?.name ?? parseSpace(space)?.key ?? space,
          topic: record?.topic ?? null,
        };
      })
      .sort((a, b) => a.name.localeCompare(b.name));
  },
});

// listMessages returns a channel's most recent messages, oldest first. A
// channel the viewer can't read returns nothing, the same as an empty one.
export const listMessages = query({
  args: z.object({
    space: z.string(),
    limit: z.number().int().min(1).max(500).optional(),
  }),
  handler: async (ctx, { space, limit }): Promise<MessageView[]> => {
    const rows = await ctx.db
      .query<MessageRecord>(MESSAGE)
      .in(space)
      .orderBy("createdAt", "desc")
      .limit(limit ?? DEFAULT_PAGE)
      .collect();
    return rows
      .map((r) => ({
        uri: r.uri,
        rkey: r.rkey,
        author: r.repo,
        text: r.value.text ?? "",
        createdAt: r.value.createdAt ?? "",
      }))
      .reverse();
  },
});

// sendMessage writes a message into the viewer's own repo in the channel.
// The Habitat instance decides whether they may write there; the check here
// only turns a channel they can't see into a clearer error.
export const sendMessage = mutation({
  args: z.object({
    space: z.string(),
    text: z.string().trim().min(1).max(MAX_MESSAGE_LENGTH),
  }),
  handler: async (ctx, { space, text }) => {
    if (
      parseSpace(space)?.type !== CHANNEL_SPACE_TYPE ||
      !(await ctx.db.canRead(space))
    ) {
      throw new Error("Channel not found");
    }
    return ctx.spaces.put({
      space,
      collection: MESSAGE,
      record: { text, createdAt: new Date().toISOString() },
    });
  },
});

// deleteMessage deletes one of the viewer's own messages: writes only ever
// go to the viewer's repo, so there is no way to name someone else's.
export const deleteMessage = mutation({
  args: z.object({ space: z.string(), rkey: z.string() }),
  handler: async (ctx, { space, rkey }) => {
    await ctx.spaces.delete({ space, collection: MESSAGE, rkey });
  },
});

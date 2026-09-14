// typescript/apps/convex-test/convex/records.ts
import { internalMutation, query } from "./_generated/server";
import { v } from "convex/values";
import { collections } from "../collections.config.mjs";

const collectionByNsid = new Map(collections.map((c) => [c.nsid, c]));

// at://<did>/<collection>/<rkey>
function parseUri(uri: string): { did: string; collection: string; rkey: string } {
  const match = /^at:\/\/([^/]+)\/([^/]+)\/([^/]+)$/.exec(uri);
  if (!match) throw new Error(`unparseable at-uri: ${uri}`);
  const [, did, collection, rkey] = match;
  return { did, collection, rkey };
}

export const upsertFromWebhook = internalMutation({
  args: { uri: v.string(), value: v.any() },
  handler: async (ctx, { uri, value }) => {
    const { did, collection } = parseUri(uri);
    const target = collectionByNsid.get(collection);
    if (!target) {
      // Not a collection this prototype cares about — skip, not an error.
      return;
    }
    if (target.table !== "userRelations") {
      throw new Error(`unhandled table ${target.table} — add a case below`);
    }
    const existing = await ctx.db
      .query("userRelations")
      .withIndex("by_uri", (q) => q.eq("uri", uri))
      .unique();
    const row = {
      uri,
      did,
      syncStatus: "confirmed" as const,
      subject: value.subject,
      relation: value.relation,
      createdAt: value.createdAt,
    };
    if (existing) {
      await ctx.db.patch(existing._id, row);
    } else {
      await ctx.db.insert("userRelations", row);
    }
  },
});

export const list = query({
  args: {},
  handler: async (ctx) => ctx.db.query("userRelations").collect(),
});

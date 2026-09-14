// typescript/apps/convex-test/convex/records.ts
import { env, internalAction, internalMutation, mutation, query } from "./_generated/server";
import { v } from "convex/values";
import { collections } from "../collections.config.mjs";
import { internal } from "./_generated/api";

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

export const write = mutation({
  args: {
    space: v.string(),
    repo: v.string(),
    subject: v.string(),
    relation: v.string(),
  },
  handler: async (ctx, { space, repo, subject, relation }) => {
    const uri = `pending:${crypto.randomUUID()}`; // placeholder until putRecord returns the real at-uri
    const recordId = await ctx.db.insert("userRelations", {
      uri,
      did: repo,
      syncStatus: "pending",
      subject,
      relation,
    });
    await ctx.scheduler.runAfter(0, internal.records.pushToSap, {
      recordId,
      space,
      repo,
      subject,
      relation,
    });
    return recordId;
  },
});

// Internal (not client-callable): scheduled by `write`, carries the
// SAP_INTERNAL_AUTH_SECRET, and is only ever invoked via
// `internal.records.pushToSap`.
export const pushToSap = internalAction({
  args: {
    recordId: v.id("userRelations"),
    space: v.string(),
    repo: v.string(),
    subject: v.string(),
    relation: v.string(),
  },
  handler: async (ctx, { recordId, space, repo, subject, relation }) => {
    // Convex actions run in Convex's own sandbox and cannot import from
    // src/server/ (the TanStack app's server code, a separate deploy
    // target), so this duplicates the minimal fetch call rather than
    // importing SapClient from src/server/sapClient.ts.
    // Use the generated `env` (a typed wrapper around process.env — see
    // _generated/server.ts) rather than `process.env` directly, since this
    // file also exports mutations/queries and cannot run in the Node.js
    // runtime (which "process"/"Buffer" would otherwise require).
    const sapUrl = env.SAP_INTERNAL_URL;
    if (!sapUrl) throw new Error("SAP_INTERNAL_URL is not set");
    const secret = env.SAP_INTERNAL_AUTH_SECRET;
    const headers: Record<string, string> = {
      "Habitat-Did": repo,
      "content-type": "application/json",
    };
    if (secret) {
      headers.Authorization = `Basic ${btoa(`:${secret}`)}`;
    }
    // A thrown fetch error (network failure, sap unreachable, timeout) is
    // just as much a failure as a non-ok response — mark the row failed on
    // either path so it never gets stuck at syncStatus: "pending" with no
    // signal to the UI.
    let res: Response;
    try {
      res = await fetch(`${sapUrl}/proxy/network.habitat.space.putRecord`, {
        method: "POST",
        headers,
        body: JSON.stringify({
          space,
          repo,
          collection: "network.habitat.relationship.userRelation",
          record: { subject, relation, createdAt: new Date().toISOString() },
        }),
      });
    } catch (err) {
      await ctx.runMutation(internal.records.markFailed, { recordId });
      throw err;
    }
    if (!res.ok) {
      await ctx.runMutation(internal.records.markFailed, { recordId });
      throw new Error(`putRecord failed (${res.status}): ${await res.text()}`);
    }
    const { uri, cid } = (await res.json()) as { uri: string; cid: string };
    await ctx.runMutation(internal.records.confirmWrite, { recordId, uri, cid });
  },
});

export const confirmWrite = internalMutation({
  args: { recordId: v.id("userRelations"), uri: v.string(), cid: v.string() },
  handler: async (ctx, { recordId, uri, cid }) => {
    await ctx.db.patch(recordId, { uri, cid, syncStatus: "confirmed" });
  },
});

export const markFailed = internalMutation({
  args: { recordId: v.id("userRelations") },
  handler: async (ctx, { recordId }) => {
    await ctx.db.patch(recordId, { syncStatus: "failed" });
  },
});

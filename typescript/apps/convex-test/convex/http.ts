// typescript/apps/convex-test/convex/http.ts
import { httpRouter } from "convex/server";
import { internal } from "./_generated/api";
import { httpAction } from "./_generated/server";

const http = httpRouter();

http.route({
  path: "/sap-webhook",
  method: "POST",
  handler: httpAction(async (ctx, request) => {
    const payload = (await request.json()) as { id: number; uri: string; value: unknown };
    await ctx.runMutation(internal.records.upsertFromWebhook, {
      uri: payload.uri,
      value: payload.value,
    });
    return new Response(null, { status: 200 });
  }),
});

export default http;

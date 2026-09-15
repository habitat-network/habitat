// typescript/apps/convex-test/convex/convex.config.ts
import { defineApp } from "convex/server";
import { v } from "convex/values";

// Typed app environment variables (see convex/_generated/ai/guidelines.md)
// consumed by the `pushToSap` action in convex/records.ts to relay writes
// to the sap Go service.
const app = defineApp({
  env: {
    SAP_INTERNAL_URL: v.optional(v.string()),
    SAP_INTERNAL_AUTH_SECRET: v.optional(v.string()),
  },
});

export default app;

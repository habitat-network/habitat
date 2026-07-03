import fs from "node:fs/promises";
import { serve } from "@hono/node-server";
import { loadConfig } from "./config";
import { PearClient, resolveOrgDid } from "./pearClient";
import { DocStore } from "./docStore";
import { CrawlStore } from "./crawlStore";
import { Crawler } from "./crawler";
import { createApp } from "./server";

async function main() {
  const config = loadConfig();
  // Authenticated pear calls are proxied by sap using the org's tracked
  // session; we only need the org DID to name that session in the Habitat-Did
  // header, so resolve it from the configured handle once at startup. pear may
  // still be coming up (they start together in dev), so retry rather than crash.
  const orgDid = await resolveOrgDidWithRetry(config.orgHandle);
  const pear = new PearClient(config, orgDid);
  const docs = new DocStore(pear);

  // The crawler subscribes to sap's outbox channel to discover the org's docs
  // and persists them (with their reader DIDs) to the crawl store, which backs
  // the per-caller listDocs endpoint.
  await fs.mkdir(config.dataDir, { recursive: true });
  const crawl = new CrawlStore(config.crawlDbPath);
  const crawler = new Crawler(config, pear, crawl);
  crawler.start();

  const app = createApp(config, docs, crawl);

  serve({ fetch: app.fetch, port: config.port }, (info) => {
    console.log(
      `[docs-server] listening on :${info.port} as ${config.did} ` +
        `(service #${config.serviceId})`,
    );
  });
}

// resolveOrgDidWithRetry resolves the org handle to its DID, retrying while pear
// is still starting up (the handle lookup goes through pear). Gives up after
// ~60s so a genuinely bad handle still surfaces as a fatal error.
async function resolveOrgDidWithRetry(handle: string): Promise<string> {
  const maxAttempts = 30;
  const delayMs = 2000;
  for (let attempt = 1; ; attempt++) {
    try {
      return await resolveOrgDid(handle);
    } catch (err) {
      if (attempt >= maxAttempts) {
        throw err;
      }
      console.warn(
        `[docs-server] could not resolve org handle ${handle} ` +
          `(attempt ${attempt}/${maxAttempts}), retrying: ${String(err)}`,
      );
      await new Promise((resolve) => setTimeout(resolve, delayMs));
    }
  }
}

main().catch((err) => {
  console.error("[docs-server] fatal:", err);
  process.exit(1);
});

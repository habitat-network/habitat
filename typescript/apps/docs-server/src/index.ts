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
  // header, so resolve it from the configured handle once at startup.
  const orgDid = await resolveOrgDid(config.orgHandle);
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

main().catch((err) => {
  console.error("[docs-server] fatal:", err);
  process.exit(1);
});

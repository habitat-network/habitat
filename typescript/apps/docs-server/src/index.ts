import fs from "node:fs/promises";
import { serve } from "@hono/node-server";
import { loadConfig } from "./config";
import { PearClient } from "./pearClient";
import { DocStore } from "./docStore";
import { CrawlStore } from "./crawlStore";
import { Crawler } from "./crawler";
import { OrgDirectory } from "./orgDirectory";
import { createApp } from "./server";

async function main() {
  const config = loadConfig();
  // Authenticated pear calls are proxied by sap, which holds sessions for the
  // orgs it manages. The caller's org is resolved per request from org
  // membership (OrgDirectory), so the docs server needs no per-org config.
  const pear = new PearClient(config);
  const docs = new DocStore(pear);
  const orgs = new OrgDirectory(config, pear);

  // The crawler subscribes to sap's outbox channel to discover the org's docs
  // and persists their titles to the crawl store; permissions are resolved on
  // demand at read time.
  await fs.mkdir(config.dataDir, { recursive: true });
  const crawl = new CrawlStore(config.crawlDbPath);
  const crawler = new Crawler(config, crawl);
  crawler.start();

  const app = createApp(config, pear, docs, crawl, orgs);

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

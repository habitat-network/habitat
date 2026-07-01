import fs from "node:fs/promises";
import { serve } from "@hono/node-server";
import { loadConfig } from "./config";
import { OrgClient } from "./orgClient";
import { PearClient } from "./pearClient";
import { DocStore } from "./docStore";
import { CrawlStore } from "./crawlStore";
import { Crawler } from "./crawler";
import { createApp } from "./server";

async function main() {
  const config = loadConfig();
  const org = await OrgClient.create(config);
  const pear = new PearClient(config, org);
  const docs = new DocStore(pear);

  // The crawler subscribes to sap's outbox channel to discover the org's docs
  // and persists them (with their reader DIDs) to the crawl store, which backs
  // the per-caller listDocs endpoint.
  await fs.mkdir(config.dataDir, { recursive: true });
  const crawl = new CrawlStore(config.crawlDbPath);
  const crawler = new Crawler(config, pear, crawl);
  crawler.start();

  const app = createApp(config, org, docs, crawl);

  if (!org.isAuthorized()) {
    console.warn(
      `[docs-server] not yet authorized for ${config.orgHandle}; ` +
        `visit https://${config.domain}/oauth/login to grant the org credential`,
    );
  }

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

import { serve } from "@hono/node-server";
import { loadConfig } from "./config";
import { OrgClient } from "./orgClient";
import { PearClient } from "./pearClient";
import { DocStore } from "./docStore";
import { PermissionStore } from "./permissionStore";
import { createApp } from "./server";

// How often the permission snapshot is refreshed from pear's relationship
// graph, so listDocs filtering keeps up with grants made elsewhere.
const CRAWL_INTERVAL_MS = 30_000;

async function main() {
  const config = loadConfig();
  const org = await OrgClient.create(config);
  const pear = new PearClient(config, org);
  const permissions = new PermissionStore(config.dataDir);
  await permissions.load();
  const docs = new DocStore(pear, permissions);
  const app = createApp(config, org, docs);

  if (!org.isAuthorized()) {
    console.warn(
      `[docs-server] not yet authorized for ${config.orgHandle}; ` +
        `visit https://${config.domain}/oauth/login to grant the org credential`,
    );
  } else {
    // Prime the permission snapshot and keep it fresh in the background.
    void crawl(docs);
    setInterval(() => void crawl(docs), CRAWL_INTERVAL_MS);
  }

  serve({ fetch: app.fetch, port: config.port }, (info) => {
    console.log(
      `[docs-server] listening on :${info.port} as ${config.did} ` +
        `(service #${config.serviceId})`,
    );
  });
}

async function crawl(docs: DocStore): Promise<void> {
  try {
    await docs.crawl();
  } catch (err) {
    console.error("[docs-server] permission crawl failed:", err);
  }
}

main().catch((err) => {
  console.error("[docs-server] fatal:", err);
  process.exit(1);
});

import fs from "node:fs/promises";
import path from "node:path";
import { DatabaseSync } from "node:sqlite";
import { serve } from "@hono/node-server";
import { loadConfig } from "./config";
import { PearClient } from "./pearClient";
import { DocCrdtStore } from "./docCrdtStore";
import { DocMetadataStore } from "./docMetadataStore";
import { Crawler } from "./crawler";
import { DocCommentStore } from "./docCommentStore";
import { OrgDirectory } from "./orgDirectory";
import { createApp } from "./server";

async function main() {
  const config = loadConfig();
  // Authenticated pear calls are proxied by sap, which holds sessions for the
  // orgs it manages. The caller's org is resolved per request from org
  // membership (OrgDirectory), so the docs server needs no per-org config.
  const pear = new PearClient(config);

  // Doc CRDT state, doc metadata and org membership share one sqlite database
  // so all of it survives restarts.
  await fs.mkdir(path.dirname(config.db), { recursive: true });
  const db = new DatabaseSync(config.db);
  const docs = new DocCrdtStore(pear, db);
  const meta = new DocMetadataStore(db);
  const orgs = new OrgDirectory(config, pear, db);
  const comments = new DocCommentStore(db);

  // The crawler subscribes to sap's outbox channel to discover the org's docs,
  // persisting their titles to the metadata store and their CRDT state to the
  // CRDT store; permissions are resolved on demand at read time. It also
  // forwards events on the network.habitat.organization space so the org
  // directory refetches membership instead of polling on an interval.
  const crawler = new Crawler(config, meta, docs, orgs, comments);
  crawler.start();

  // Populate the org directory once at startup; subsequent per-org refreshes
  // are driven by sap's org-space events above.
  orgs
    .refreshAll()
    .catch((err) =>
      console.error("[org-directory] initial refresh failed", err),
    );

  const app = createApp(config, pear, docs, meta, orgs, comments);

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

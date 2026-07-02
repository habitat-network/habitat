import path from "node:path";

// Config is parsed once at startup from the environment, mirroring the
// "parse env in main() only" convention used by the Go binaries.
export interface Config {
  // Public domain the docs server is reachable at. Also its did:web host, so
  // the DID is did:web:<domain> and pear can resolve the #docs service endpoint.
  domain: string;
  // The org this server acts on behalf of (e.g. "acme.local.habitat.network").
  // Resolved to the org DID at startup; that DID is sent to sap so it proxies
  // requests using the org's tracked session, and is the handle sap's /org/add
  // OAuth flow authorizes.
  orgHandle: string;
  port: number;
  // Directory where the crawl database is persisted.
  dataDir: string;
  // Space type each doc's space is created under. Each document gets its own
  // space of this type.
  spaceType: string;
}

export interface DerivedConfig extends Config {
  did: string;
  serviceId: string;
  // Base URL of sap's internal port. All authenticated pear XRPC calls are
  // routed through <sapUrl>/proxy (sap attaches the org's OAuth token), and the
  // org-login bootstrap kicks off sap's <sapUrl>/org/add OAuth flow.
  sapUrl: string;
  sapProxyUrl: string;
  // Websocket URL of sap's internal outbox channel. The crawler subscribes here
  // to discover the org's docs and acks each message it receives.
  sapChannelUrl: string;
  // Path to the sqlite database the crawler persists discovered docs and their
  // per-doc reader DIDs to.
  crawlDbPath: string;
}

function required(name: string): string {
  const v = process.env[name];
  if (!v) {
    throw new Error(`missing required environment variable: ${name}`);
  }
  return v;
}

export function loadConfig(): DerivedConfig {
  const domain = required("DOCS_SERVER_DOMAIN");
  const orgHandle = required("DOCS_SERVER_ORG_HANDLE");
  const dataDir = process.env.DOCS_SERVER_DATA_DIR ?? ".docs-server";
  // sap's internal port serves /proxy (http), /org/add (http) and /channel
  // (ws), and is not publicly exposed via TLS. Defaults to the local-dev sap.
  // The channel websocket URL is derived by swapping the scheme.
  const sapUrl = (
    process.env.DOCS_SERVER_SAP_URL ?? "http://127.0.0.1:2581"
  ).replace(/\/$/, "");
  const config: Config = {
    domain,
    orgHandle,
    port: parseInt(process.env.DOCS_SERVER_PORT ?? "2590", 10),
    dataDir,
    spaceType: process.env.DOCS_SERVER_SPACE_TYPE ?? "network.habitat.docs",
  };
  const serviceId = "docs";
  return {
    ...config,
    serviceId,
    did: `did:web:${domain}`,
    sapUrl,
    sapProxyUrl: `${sapUrl}/proxy`,
    sapChannelUrl: `${sapUrl.replace(/^http/, "ws")}/channel`,
    crawlDbPath:
      process.env.DOCS_SERVER_CRAWL_DB ?? path.join(dataDir, "crawl.db"),
  };
}

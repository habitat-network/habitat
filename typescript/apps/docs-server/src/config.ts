import path from "node:path";

// Config is parsed once at startup from the environment, mirroring the
// "parse env in main() only" convention used by the Go binaries.
export interface Config {
  // Public domain the docs server is reachable at. Also its did:web host, so
  // the DID is did:web:<domain> and pear can resolve the #docs service endpoint.
  domain: string;
  // The org this server holds a credential for (e.g. "acme.local.habitat.network").
  orgHandle: string;
  // Base URL of the org's pear instance, used for OAuth and XRPC calls.
  pearHost: string;
  port: number;
  // Directory where the OAuth credential and signing key are persisted so the
  // server doesn't need re-authorization on every restart.
  dataDir: string;
  // Space type each doc's space is created under. Each document gets its own
  // space of this type.
  spaceType: string;
}

export interface DerivedConfig extends Config {
  did: string;
  serviceId: string;
  clientId: string;
  redirectUri: string;
  credentialPath: string;
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
  // Tolerate DOCS_SERVER_PEAR_HOST being set with or without a scheme; all pear
  // URLs are built by concatenation, so a bare host would otherwise be invalid.
  const rawPearHost = required("DOCS_SERVER_PEAR_HOST").replace(/\/$/, "");
  const pearHost = /^https?:\/\//.test(rawPearHost)
    ? rawPearHost
    : `https://${rawPearHost}`;
  const dataDir = process.env.DOCS_SERVER_DATA_DIR ?? ".docs-server";
  const config: Config = {
    domain,
    orgHandle,
    pearHost,
    port: parseInt(process.env.DOCS_SERVER_PORT ?? "2590", 10),
    dataDir,
    spaceType: process.env.DOCS_SERVER_SPACE_TYPE ?? "network.habitat.docs",
  };
  const serviceId = "docs";
  return {
    ...config,
    serviceId,
    did: `did:web:${domain}`,
    clientId: `https://${domain}/client-metadata.json`,
    redirectUri: `https://${domain}/oauth-callback`,
    credentialPath: path.join(dataDir, "credential.json"),
    // sap's internal port serves the /channel outbox endpoint over plain ws
    // (it is not publicly exposed via TLS). Defaults to the local-dev sap.
    sapChannelUrl:
      process.env.DOCS_SERVER_SAP_URL ?? "ws://127.0.0.1:2581/channel",
    crawlDbPath:
      process.env.DOCS_SERVER_CRAWL_DB ?? path.join(dataDir, "crawl.db"),
  };
}

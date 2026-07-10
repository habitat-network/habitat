// Config is parsed once at startup from the environment, mirroring the
// "parse env in main() only" convention used by the Go binaries.
export interface Config {
  // Public domain the docs server is reachable at. Also its did:web host, so
  // the DID is did:web:<domain> and pear can resolve the #docs service endpoint.
  domain: string;
  port: number;
  // Path to the sqlite database the docs server persists to (crawled docs and
  // org membership). Its parent directory is created at startup.
  db: string;
}

export interface DerivedConfig extends Config {
  did: string;
  serviceId: string;
  // Base URL of sap's internal port. All authenticated pear XRPC calls are
  // routed through <sapUrl>/proxy (sap attaches the DID's OAuth token), and the
  // org-login and user-login bootstraps kick off sap's OAuth flows.
  sapUrl: string;
  // Origin of the docsv2 frontend, allowed to make credentialed (cookie)
  // cross-origin requests. Server sessions are established via a cookie on this
  // server's domain, so the frontend must be a known origin for CORS.
  appOrigin: string;
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
  const sapUrl = required("DOCS_SERVER_SAP_URL").replace(/\/$/, "");
  const appOrigin = required("DOCS_SERVER_APP_ORIGIN").replace(/\/$/, "");
  const config: Config = {
    domain,
    port: parseInt(process.env.DOCS_SERVER_PORT ?? "2590", 10),
    db: process.env.DOCS_SERVER_DB ?? ".docs-server/docs-server.db",
  };
  const serviceId = "docs";
  return {
    ...config,
    serviceId,
    did: `did:web:${domain}`,
    sapUrl,
    appOrigin,
  };
}

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
  // Space type + key the canonical doc records are written into.
  spaceType: string;
  spaceSkey: string;
}

export interface DerivedConfig extends Config {
  did: string;
  serviceId: string;
  clientId: string;
  redirectUri: string;
  credentialPath: string;
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
    spaceSkey: process.env.DOCS_SERVER_SPACE_SKEY ?? "docs",
  };
  const serviceId = "docs";
  return {
    ...config,
    serviceId,
    did: `did:web:${domain}`,
    clientId: `https://${domain}/client-metadata.json`,
    redirectUri: `https://${domain}/oauth-callback`,
    credentialPath: path.join(dataDir, "credential.json"),
  };
}

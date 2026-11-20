import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  DockerComposeEnvironment,
  Wait,
  type StartedDockerComposeEnvironment,
} from "testcontainers";

const e2eRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);

/**
 * The instance's domain. Not "localhost": atproto OAuth clients refuse loopback
 * hosts for the server a DID names as its PDS (which is pear for *.test logins),
 * and atproto directories refuse did:web hosts under reserved TLDs such as
 * ".example", which pear needs to resolve its own DID. It is no real host: the
 * host resolver rule below sends browsers to the test bed, and compose network
 * aliases send pear's own requests there.
 */
export const PEAR_DOMAIN = "habitat-e2e.dev";

// Throwaway secrets for the test instance only. Same values pear:dev uses.
export const PEAR_ENV = {
  HABITAT_DOMAIN: PEAR_DOMAIN,
  HABITAT_PORT: "8000",
  HABITAT_SPACE_SIGNING_KEY: "z42txZeYVsanuqfQUQ4eDQEaKC2X21LjDgVc3MPk9k32BVzd",
  HABITAT_PDS_CRED_ENCRYPT_KEY: "WRlpsucNQl8AVO1qNN+T+hzbVtYjdoF21QfF+0uDJSM=",
  HABITAT_OAUTH_SERVER_SECRET: "KFLA79A/GPPsxz+y5kk7dLX2Nxxe7RM9Ik96rD2tO4c=",
  HABITAT_OAUTH_CLIENT_SECRET: "RL4fcDJiumyAEMJ5US0WWu/QKDallQAZ/2GJHz99d4Y=",
  // Enables the Google login method. Fake: tests stop at the redirect to Google.
  HABITAT_GOOGLE_CLIENT_ID: "e2e-google-client-id",
  HABITAT_GOOGLE_CLIENT_SECRET: "e2e-google-client-secret",
};

/**
 * Pear builds absolute redirects as https://<HABITAT_DOMAIN>/..., so browsers
 * must reach it over HTTPS at its domain on port 443. Pear itself speaks plain
 * HTTP; a Caddy in front terminates TLS with its internal CA (the browser
 * ignores the certificate error). Caddy and pear listen on arbitrary ports, so
 * the browser maps (see `hostResolverRule`) its domain on port 443 onto Caddy.
 */
export const PEAR_ORIGIN = `https://${PEAR_DOMAIN}`;

/**
 * Where the web client (frontend/) is served by the compose test bed, behind the
 * same TLS proxy as pear. Pear fetches its OAuth client metadata from here, so
 * the test bed is the only flavour that serves it.
 */
export const APP_ORIGIN = `https://app.${PEAR_DOMAIN}`;

/**
 * Where the Habitat app (typescript/apps/app: docs, files and group management,
 * the successor of chalk) is served by the compose test bed. Pear fetches its
 * OAuth client metadata from here.
 */
export const CHALK_ORIGIN = `https://chalk.${PEAR_DOMAIN}`;

/** Where the alpha PDS (compose test bed only) is served, behind the same TLS proxy. */
export const PDS_ORIGIN = `https://pds.${PEAR_DOMAIN}`;

export interface PearInstance {
  /** host:port of pear's plain HTTP listener; used by Node-side API calls. */
  address: string;
  /** host:port of the TLS-terminating proxy in front of pear; used by browsers. */
  tlsAddress: string;
  /** Whether the web client is served at APP_ORIGIN. */
  app?: boolean;
  /**
   * host:port of the alpha PDS's plain HTTP listener (compose test bed only);
   * used to seed its accounts (harness/pds.ts). Browsers and pear reach it at
   * PDS_ORIGIN through the TLS proxy.
   */
  pdsAddress?: string;
  stop(): Promise<void>;
}

/**
 * Chromium flag that sends PEAR_ORIGIN, APP_ORIGIN and the orgs' subdomains
 * (all under PEAR_DOMAIN, port 443) to the TLS proxy.
 */
export function hostResolverRule(pear: Pick<PearInstance, "tlsAddress">) {
  return `--host-resolver-rules=MAP *${PEAR_DOMAIN}:443 ${pear.tlsAddress}`;
}

/** Caddyfile that serves `site` over TLS and proxies to pear at `upstream`. */
function caddyfile(site: string, upstream: string): string {
  return `{
  admin off
  skip_install_trust
  auto_https disable_redirects
}
${site} {
  tls internal
  reverse_proxy ${upstream}
}
`;
}

/**
 * Starts a fresh pear (empty database, migrations applied) behind a
 * Caddy TLS proxy and waits until it is healthy. Which flavour runs is picked
 * from the environment:
 *
 *  - PEAR_ADDRESS + PEAR_TLS_ADDRESS  use an already-running pear and proxy
 *                                     whose domain is PEAR_DOMAIN (nothing is
 *                                     started or stopped); add PDS_ADDRESS
 *                                     for its PDS

 *  - PEAR_BINARY=<path>               run a locally built pear binary and the
 *                                     caddy on PATH (CADDY_BIN to override);
 *                                     no Docker needed
 *  - default                          docker compose (compose.yaml, started with
 *                                     testcontainers): pear on Postgres, the
 *                                     web client, the alpha PDS with a local
 *                                     PLC directory, and caddy
 *
 * Only the compose flavour serves the web client (APP_ORIGIN) and the PDS;
 * tests that need them check `E2E_APP` and `E2E_PDS_ACCOUNTS`.
 */
export async function startPear(): Promise<PearInstance> {
  if (process.env.PEAR_ADDRESS && process.env.PEAR_TLS_ADDRESS) {
    return {
      address: process.env.PEAR_ADDRESS,
      tlsAddress: process.env.PEAR_TLS_ADDRESS,
      pdsAddress: process.env.PDS_ADDRESS,
      stop: async () => {},
    };
  }
  if (process.env.PEAR_BINARY) {
    return startBinary(path.resolve(process.env.PEAR_BINARY));
  }
  return startCompose();
}

async function startCompose(): Promise<PearInstance> {
  // E2E_COMPOSE_FILES adds comma-separated compose files (relative to e2e/) on
  // top of compose.yaml, e.g. to run prebuilt images instead of building them.
  const files = [
    "compose.yaml",
    ...(process.env.E2E_COMPOSE_FILES?.split(",").filter(Boolean) ?? []),
  ];
  const project = `habitat-e2e-${process.pid}`;
  const environment = new DockerComposeEnvironment(e2eRoot, files)
    .withProjectName(project)
    // Pear's secrets reach compose.yaml by interpolation.
    .withEnvironment(PEAR_ENV)
    // Builds the pear and web client images on first use (cached afterwards).
    .withBuild()
    .withWaitStrategy(
      "pear-1",
      Wait.forHttp("/health", 8000).forStatusCode(200),
    )
    .withWaitStrategy(
      "pds-1",
      Wait.forHttp("/xrpc/_health", 3000).forStatusCode(200),
    )
    .withWaitStrategy(
      "chalk-1",
      Wait.forHttp("/oauth/client-metadata.json", 5181).forStatusCode(200),
    )
    .withWaitStrategy("caddy-1", Wait.forListeningPorts())
    // The first run also builds the PLC directory and pulls the PDS image.
    .withStartupTimeout(180_000);
  let started: StartedDockerComposeEnvironment;
  try {
    started = await environment.up(["pear", "caddy"]);
  } catch (err) {
    // `up` leaves what it started behind when it fails.
    try {
      execFileSync(
        "docker",
        ["compose", "-p", project, "down", "--volumes", "--timeout", "5"],
        { stdio: "ignore" },
      );
    } catch {
      // best effort
    }
    throw new Error(`test bed failed to start: ${err}`);
  }
  const pear = started.getContainer("pear-1");
  const caddy = started.getContainer("caddy-1");
  const pds = started.getContainer("pds-1");
  return {
    address: `${pear.getHost()}:${pear.getMappedPort(8000)}`,
    tlsAddress: `${caddy.getHost()}:${caddy.getMappedPort(443)}`,
    app: true,
    pdsAddress: `${pds.getHost()}:${pds.getMappedPort(3000)}`,
    stop: async () => {
      // E2E_PEAR_LOGS=1 prints pear's logs; a comma-separated list of service
      // names (e.g. "pear,pds,plc") prints those. (Container.logs() follows the
      // log and never ends, so ask compose for what is there so far.)
      const logs = process.env.E2E_PEAR_LOGS;
      if (logs) {
        const services = logs === "1" ? ["pear"] : logs.split(",");
        try {
          process.stderr.write(
            execFileSync(
              "docker",
              ["compose", "-p", project, "logs", "--no-color", ...services],
              { maxBuffer: 64 * 1024 * 1024 },
            ),
          );
        } catch (err) {
          process.stderr.write(`could not read test bed logs: ${err}\n`);
        }
      }
      await started.down({ removeVolumes: true, timeout: 5_000 });
    },
  };
}

async function startBinary(binary: string): Promise<PearInstance> {
  const dir = mkdtempSync(path.join(tmpdir(), "pear-e2e-"));
  const pearPort = String(30000 + Math.floor(Math.random() * 10000));
  const tlsPort = String(40000 + Math.floor(Math.random() * 10000));
  const env = { ...process.env, ...PEAR_ENV, HABITAT_PORT: pearPort };
  execFileSync(binary, ["migrate"], { cwd: dir, env, stdio: "ignore" });
  const pear: ChildProcess = spawn(binary, ["serve"], {
    cwd: dir,
    env,
    stdio: "ignore",
  });
  const caddyConfig = path.join(dir, "Caddyfile");
  writeFileSync(
    caddyConfig,
    caddyfile(`${PEAR_DOMAIN}:${tlsPort}`, `localhost:${pearPort}`),
  );
  const caddy: ChildProcess = spawn(
    process.env.CADDY_BIN ?? "caddy",
    ["run", "--config", caddyConfig, "--adapter", "caddyfile"],
    { cwd: dir, stdio: "ignore" },
  );
  const address = `localhost:${pearPort}`;
  await waitForHealth(address);
  return {
    address,
    tlsAddress: `localhost:${tlsPort}`,
    stop: async () => {
      pear.kill();
      caddy.kill();
      rmSync(dir, { recursive: true, force: true });
    },
  };
}

async function waitForHealth(address: string) {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    try {
      if ((await fetch(`http://${address}/health`)).ok) return;
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`pear at ${address} did not become healthy`);
}

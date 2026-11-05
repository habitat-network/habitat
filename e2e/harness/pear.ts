import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  GenericContainer,
  Network,
  Wait,
  type StartedNetwork,
  type StartedTestContainer,
} from "testcontainers";

const repoRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../..",
);

// Throwaway secrets for the test instance only. Same values pear:dev uses.
const PEAR_ENV = {
  HABITAT_DOMAIN: "localhost",
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
 * the browser maps (see `hostResolverRule`) localhost:443 onto Caddy.
 */
export const PEAR_ORIGIN = "https://localhost";

export interface PearInstance {
  /** host:port of pear's plain HTTP listener; used by Node-side API calls. */
  address: string;
  /** host:port of the TLS-terminating proxy in front of pear; used by browsers. */
  tlsAddress: string;
  stop(): Promise<void>;
}

/** Chromium flag that sends PEAR_ORIGIN to the TLS proxy. */
export function hostResolverRule(pear: Pick<PearInstance, "tlsAddress">) {
  return `--host-resolver-rules=MAP localhost:443 ${pear.tlsAddress}`;
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
 *                                     whose domain is localhost (nothing is
 *                                     started or stopped)
 *  - PEAR_BINARY=<path>               run a locally built pear binary and the
 *                                     caddy on PATH (CADDY_BIN to override);
 *                                     no Docker needed
 *  - default                          run pear (on Postgres) and caddy:2 in
 *                                     testcontainers
 */
export async function startPear(): Promise<PearInstance> {
  if (process.env.PEAR_ADDRESS && process.env.PEAR_TLS_ADDRESS) {
    return {
      address: process.env.PEAR_ADDRESS,
      tlsAddress: process.env.PEAR_TLS_ADDRESS,
      stop: async () => {},
    };
  }
  if (process.env.PEAR_BINARY) {
    return startBinary(path.resolve(process.env.PEAR_BINARY));
  }
  return startContainers();
}

async function startContainers(): Promise<PearInstance> {
  const image = ensureImage();
  const network: StartedNetwork = await new Network().start();
  // The pear image is built without cgo, so it cannot use the SQLite driver
  // that local runs default to; like production it needs Postgres.
  const postgres = await new GenericContainer("postgres:16-alpine")
    .withEnvironment({
      POSTGRES_DB: "habitat",
      POSTGRES_USER: "habitat",
      POSTGRES_PASSWORD: "habitat",
    })
    .withNetwork(network)
    .withNetworkAliases("postgres")
    // The entrypoint restarts postgres once after init, hence two occurrences.
    .withWaitStrategy(
      Wait.forLogMessage(/database system is ready to accept connections/, 2),
    )
    .start();
  const pearLogs: string[] = [];
  const pear = await new GenericContainer(image)
    .withEnvironment({
      ...PEAR_ENV,
      HABITAT_DB:
        "postgres://habitat:habitat@postgres:5432/habitat?sslmode=disable",
    })
    .withLogConsumer((stream) => {
      stream.on("data", (line) => pearLogs.push(String(line)));
    })
    .withNetwork(network)
    .withNetworkAliases("pear")
    .withExposedPorts(8000)
    // The server does not migrate on startup, so migrate first.
    .withCommand(["sh", "-c", "/app/pear migrate && exec /app/pear serve"])
    .withWaitStrategy(Wait.forHttp("/health", 8000).forStatusCode(200))
    .withStartupTimeout(60_000)
    .start()
    .catch(async (err) => {
      await postgres.stop();
      throw new Error(
        `pear container failed to start: ${err}\n--- pear logs ---\n${pearLogs.slice(-60).join("")}`,
      );
    });
  const caddy = await new GenericContainer("caddy:2-alpine")
    .withNetwork(network)
    .withCopyContentToContainer([
      {
        content: caddyfile("localhost:443", "pear:8000"),
        target: "/etc/caddy/Caddyfile",
      },
    ])
    .withExposedPorts(443)
    .withWaitStrategy(Wait.forListeningPorts())
    .start()
    .catch(async (err) => {
      await pear.stop();
      await postgres.stop();
      throw new Error(`caddy container failed to start: ${err}`);
    });
  return {
    address: `${pear.getHost()}:${pear.getMappedPort(8000)}`,
    tlsAddress: `${caddy.getHost()}:${caddy.getMappedPort(443)}`,
    stop: async () => {
      await dumpLogs(pear);
      await caddy.stop();
      await pear.stop();
      await postgres.stop();
      await network.stop();
    },
  };
}

/** Prints the container's logs when E2E_PEAR_LOGS is set, for debugging. */
async function dumpLogs(container: StartedTestContainer) {
  if (!process.env.E2E_PEAR_LOGS) return;
  const stream = await container.logs();
  for await (const chunk of stream) process.stderr.write(chunk);
}

/**
 * Returns the pear image to run, building it from cmd/pear/Dockerfile when it
 * is not there yet. Set PEAR_IMAGE to use an image built elsewhere (CI, moon).
 * Building goes through the docker CLI because the Dockerfile needs BuildKit.
 */
function ensureImage(): string {
  const image = process.env.PEAR_IMAGE ?? "pear:e2e";
  if (process.env.PEAR_IMAGE || hasImage(image)) return image;
  execFileSync(
    "docker",
    ["build", "-f", "cmd/pear/Dockerfile", "-t", image, "."],
    {
      cwd: repoRoot,
      stdio: "inherit",
      env: { ...process.env, DOCKER_BUILDKIT: "1" },
    },
  );
  return image;
}

function hasImage(image: string): boolean {
  try {
    execFileSync("docker", ["image", "inspect", image], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
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
    caddyfile(`localhost:${tlsPort}`, `localhost:${pearPort}`),
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

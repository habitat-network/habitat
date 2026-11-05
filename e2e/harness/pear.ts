import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { generate } from "selfsigned";
import {
  GenericContainer,
  Wait,
  type StartedTestContainer,
} from "testcontainers";

const repoRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../..",
);

/** Password of the instance admin on the instance under test. */
export const ADMIN_PASSWORD = "e2e-admin-password";

// Throwaway secrets for the test instance only. Same values pear:dev uses.
const PEAR_ENV = {
  HABITAT_DOMAIN: "localhost",
  HABITAT_PORT: "8000",
  HABITAT_ADMIN_PASSWORD: ADMIN_PASSWORD,
  HABITAT_SPACE_SIGNING_KEY: "z42txZeYVsanuqfQUQ4eDQEaKC2X21LjDgVc3MPk9k32BVzd",
  HABITAT_PDS_CRED_ENCRYPT_KEY: "WRlpsucNQl8AVO1qNN+T+hzbVtYjdoF21QfF+0uDJSM=",
  HABITAT_OAUTH_SERVER_SECRET: "KFLA79A/GPPsxz+y5kk7dLX2Nxxe7RM9Ik96rD2tO4c=",
  HABITAT_OAUTH_CLIENT_SECRET: "RL4fcDJiumyAEMJ5US0WWu/QKDallQAZ/2GJHz99d4Y=",
};

/**
 * Pear builds absolute redirects as https://<HABITAT_DOMAIN>/..., so the
 * instance has to be reachable over HTTPS at its domain on port 443. Containers
 * and local processes listen on arbitrary ports instead, so the browser maps
 * (see `hostResolverRule`) localhost:443 onto the real address.
 */
export const PEAR_ORIGIN = "https://localhost";

export interface PearInstance {
  /** host:port pear is actually listening on, as reachable from this machine. */
  address: string;
  stop(): Promise<void>;
}

/** Chromium flag that sends PEAR_ORIGIN to the running instance. */
export function hostResolverRule(pear: Pick<PearInstance, "address">): string {
  return `--host-resolver-rules=MAP localhost:443 ${pear.address}`;
}

/**
 * Starts a fresh pear (empty SQLite database, migrations applied) and waits
 * until it is healthy. Which flavour runs is picked from the environment:
 *
 *  - PEAR_ADDRESS=host:port  use an already-running TLS pear whose domain is
 *                            localhost (nothing is started/stopped)
 *  - PEAR_BINARY=<path>      run a locally built pear binary (no Docker needed)
 *  - default                 run the pear image in a testcontainer
 */
export async function startPear(): Promise<PearInstance> {
  if (process.env.PEAR_ADDRESS) {
    return { address: process.env.PEAR_ADDRESS, stop: async () => {} };
  }
  if (process.env.PEAR_BINARY) {
    return startBinary(path.resolve(process.env.PEAR_BINARY));
  }
  return startContainer();
}

/** Writes a self-signed cert for localhost as fullchain.pem/privkey.pem, the names pear expects. */
async function writeCerts(dir: string) {
  mkdirSync(dir, { recursive: true });
  const pems = await generate([{ name: "commonName", value: "localhost" }], {
    algorithm: "sha256",
    extensions: [
      { name: "subjectAltName", altNames: [{ type: 2, value: "localhost" }] },
    ],
  });
  writeFileSync(path.join(dir, "fullchain.pem"), pems.cert);
  writeFileSync(path.join(dir, "privkey.pem"), pems.private);
}

async function startContainer(): Promise<PearInstance> {
  const image = ensureImage();
  const certDir = mkdtempSync(path.join(tmpdir(), "pear-e2e-certs-"));
  await writeCerts(certDir);
  const container = await new GenericContainer(image)
    .withEnvironment({ ...PEAR_ENV, HABITAT_HTTPSCERTS: "/certs" })
    .withCopyDirectoriesToContainer([{ source: certDir, target: "/certs" }])
    .withExposedPorts(8000)
    // The server does not migrate on startup, so migrate first.
    .withCommand(["sh", "-c", "/app/pear migrate && exec /app/pear serve"])
    .withWaitStrategy(
      Wait.forHttp("/health", 8000)
        .usingTls()
        .allowInsecure()
        .forStatusCode(200),
    )
    .withStartupTimeout(60_000)
    .start()
    .catch((err) => {
      throw new Error(`pear container failed to start: ${err}`);
    });
  return {
    address: `${container.getHost()}:${container.getMappedPort(8000)}`,
    stop: async () => {
      await dumpLogs(container);
      await container.stop();
      rmSync(certDir, { recursive: true, force: true });
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
  const port = String(30000 + Math.floor(Math.random() * 20000));
  const certDir = path.join(dir, "certs");
  await writeCerts(certDir);
  const env = {
    ...process.env,
    ...PEAR_ENV,
    HABITAT_PORT: port,
    HABITAT_HTTPSCERTS: certDir,
  };
  execFileSync(binary, ["migrate"], { cwd: dir, env, stdio: "ignore" });
  const child: ChildProcess = spawn(binary, ["serve"], {
    cwd: dir,
    env,
    stdio: "ignore",
  });
  const address = `localhost:${port}`;
  await waitForHealth(address);
  return {
    address,
    stop: async () => {
      child.kill();
      rmSync(dir, { recursive: true, force: true });
    },
  };
}

async function waitForHealth(address: string) {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    try {
      if ((await fetch(`https://${address}/health`)).ok) return;
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`pear at ${address} did not become healthy`);
}

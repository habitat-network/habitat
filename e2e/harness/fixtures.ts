import { test as base, expect } from "@playwright/test";
import { PearApi, type CreateOrgInput, type Org } from "./api";
import { PEAR_ORIGIN, hostResolverRule } from "./pear";
import { registerMcpClient, type McpClient } from "./oauth";
import type { SeedData } from "./seed";
import { MemberLoginPage } from "./pages/memberLogin";

let orgCounter = 0;

interface Fixtures {
  /** Client for the pear under test. */
  api: PearApi;
  /** Data created by harness/seed.ts. Treat as read-only. */
  seed: SeedData;
  /** Creates an org with a unique subdomain; use for tests that need their own state. */
  createOrg: (overrides?: Partial<CreateOrgInput>) => Promise<Org>;
  /** A freshly registered OAuth client for driving member sign-in. */
  mcpClient: McpClient;
  memberLogin: MemberLoginPage;
}

/**
 * Import `test` and `expect` from here, not from @playwright/test. Add new
 * fixtures (page objects, helpers, pre-authenticated pages) to this file.
 */
export const test = base.extend<Fixtures>({
  // Browsers reach the pear started in global setup at its domain (PEAR_ORIGIN);
  // the host resolver rule maps that to wherever it is really listening.
  baseURL: PEAR_ORIGIN,
  launchOptions: [
    async ({ launchOptions }, use) => {
      const args = [
        ...(launchOptions.args ?? []),
        hostResolverRule({ tlsAddress: process.env.E2E_PEAR_TLS_ADDRESS! }),
      ];
      await use({ ...launchOptions, args });
    },
    { scope: "worker" },
  ],
  api: async ({}, use) => {
    await use(new PearApi(process.env.E2E_PEAR_ADDRESS!));
  },
  seed: async ({}, use) => {
    await use(JSON.parse(process.env.E2E_SEED!) as SeedData);
  },
  createOrg: async ({ api }, use) => {
    // Subdomains must be unique across the run; worker index + counter + time keeps parallel tests apart.
    await use((overrides = {}) =>
      api.createOrg({
        handle: `t${Date.now().toString(36)}${test.info().parallelIndex}${orgCounter++}`,
        ...overrides,
      }),
    );
  },
  mcpClient: async ({ api, baseURL }, use) => {
    await use(await registerMcpClient(api, baseURL!));
  },
  memberLogin: async ({ page }, use) => {
    await use(new MemberLoginPage(page));
  },
});

export { expect };

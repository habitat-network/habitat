import { defineConfig } from "@playwright/test";

// Pear is started once per run by globalSetup (see harness/global-setup.ts) and
// every test shares it. Tests stay isolated by creating their own orgs through
// the fixtures in harness/fixtures.ts instead of resetting the instance.
export default defineConfig({
  testDir: "./tests",
  globalSetup: "./harness/global-setup.ts",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  // First start may build the pear image, which dwarfs the per-test timeouts.
  globalTimeout: 20 * 60_000,
  use: {
    ignoreHTTPSErrors: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    // Optional: use a system Chromium instead of the one `playwright install` fetches.
    launchOptions: { executablePath: process.env.CHROMIUM_PATH || undefined },
  },
});

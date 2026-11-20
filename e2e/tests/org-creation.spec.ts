import { test } from "../harness/fixtures";
import { WebApp } from "../harness/pages/webApp";

test.skip(
  !process.env.E2E_APP,
  "needs the compose test bed (serves the web client)",
);

// A member signs in through the *.test passthrough login and creates an
// organization from the web client. Landing in the new org and every page of
// its settings must load without console errors, failed or 4xx/5xx requests,
// or error UI.
test("creating an org lands in it and its settings pages show no errors", async ({
  page,
  webApp,
}) => {
  await webApp.signIn(WebApp.uniqueHandle("alice", test.info().parallelIndex));

  await test.step("create the org", async () => {
    await page.waitForURL(/\/orgs\/?$/);
    await page.getByRole("button", { name: "New community" }).click();
    await page.getByLabel("Handle").fill(`o${Date.now().toString(36)}`);
    await page.getByRole("button", { name: "Create organization" }).click();
    await page.waitForURL(/\/orgs\/did/);
    await webApp.expectClean();
  });

  for (const tab of ["Members", "Roles", "Branding"]) {
    await test.step(`settings: ${tab}`, async () => {
      await page.getByRole("link", { name: tab, exact: true }).click();
      await webApp.expectClean();
    });
  }
});

import { test, expect } from "../harness/fixtures";

test.skip(
  !process.env.E2E_APP || !process.env.E2E_PDS_ACCOUNTS,
  "needs the compose test bed (serves the web client and the PDS)",
);

// A member with an account on the alpha PDS signs in to the web client through
// the PDS's own OAuth sign-in and consent pages, with their did:plc identity
// resolved through the test bed's PLC directory.
test("an account on the alpha PDS signs in to the web client", async ({
  page,
  webApp,
  pdsAccounts,
}) => {
  await webApp.signIn(pdsAccounts.alice);
  await page.waitForURL(/\/orgs\/?$/);
  await expect(
    page.getByRole("heading", { name: "Organizations" }),
  ).toBeVisible();
  await webApp.expectClean();
});

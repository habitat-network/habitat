import { test, expect } from "../harness/fixtures";

test.skip(
  !process.env.E2E_APP || !process.env.E2E_PDS_ACCOUNTS,
  "needs the compose test bed (serves the web client and the PDS)",
);

// Signs in to the web client with an account on the alpha PDS, then walks the spaces pages from the list down
// to a record, failing on any console error, failed or 4xx/5xx request, or
// error UI along the way.
test("clicking through the spaces pages from the list to a record shows no errors", async ({
  page,
  webApp,
  pdsAccounts,
}) => {
  const alice = pdsAccounts.alice;
  const did = alice.did;
  // Unique per run, so a kept-running test bed does not list earlier runs' spaces.
  const spaceType = `network.habitat.e2e.thing${Date.now().toString(36)}`;
  const collection = "network.habitat.e2e.note";
  const step = () => webApp.expectClean();
  const openSpaces = () => webApp.openSpaces();

  await test.step("sign in with the PDS account", async () => {
    await webApp.signIn(alice);
    await step();
  });

  // The spaces list only shows spaces the user has written to, so the space
  // needs a record before the walk down from the list can start.
  await test.step("create a space with a record", async () => {
    await openSpaces();
    await page
      .getByPlaceholder("network.habitat.example.thing")
      .fill(spaceType);
    await page.getByRole("button", { name: "Create space" }).click();
    // Creating a space lands on it.
    await expect(page.getByRole("heading", { name: /Repos/ })).toBeVisible();
    await page.getByRole("button", { name: "Create record" }).click();
    const dialog = page.getByRole("dialog");
    await dialog
      .getByPlaceholder("network.habitat.example.thing")
      .fill(collection);
    await dialog.locator("textarea").fill('{"text": "hello"}');
    await dialog.getByRole("button", { name: "Create" }).click();
    await expect(
      page.locator("tbody").getByRole("link", { name: did }),
    ).toBeVisible();
    await step();
  });

  await test.step("spaces list", async () => {
    await openSpaces();
    await expect(page.getByRole("link", { name: spaceType })).toBeVisible();
    await step();
  });

  await test.step("type", async () => {
    await page.getByRole("link", { name: spaceType }).click();
    await expect(page.getByRole("heading", { name: spaceType })).toBeVisible();
    await step();
  });

  await test.step("space", async () => {
    await page.locator("tbody a").first().click(); // the space key
    await expect(page.getByRole("heading", { name: /Repos/ })).toBeVisible();
    await expect(
      page.locator("tbody").getByRole("link", { name: did }),
    ).toBeVisible();
    await step();
  });

  await test.step("repo", async () => {
    await page.locator("tbody").getByRole("link", { name: did }).click();
    await step();
    await page.getByText(collection).click();
  });

  await test.step("record", async () => {
    await page.locator("tbody a").first().click();
    await expect(page.getByText("hello")).toBeVisible();
    await step();
  });
});

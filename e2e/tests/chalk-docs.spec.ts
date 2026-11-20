import { test, expect } from "../harness/fixtures";

test.skip(
  !process.env.E2E_APP || !process.env.E2E_PDS_ACCOUNTS,
  "needs the compose test bed (serves the app and the PDS)",
);

// A member on the alpha PDS (bob) signs in to the Habitat app (typescript/apps/app,
// chalk's successor), creates a group, links it to the app (pear's admin
// approval), then creates a doc and edits it. The edit must survive a reload
// and show up as the doc's title in the group's files.
test("a member links a group, creates a doc and edits it", async ({
  page,
  chalk,
  pdsAccounts,
}) => {
  // bob, not alice: the web client's tests sign in as alice and expect her
  // to belong to no group.
  const bob = pdsAccounts.bob;
  const title = `Plan ${Date.now().toString(36)}`;

  await test.step("sign in with the PDS account", async () => {
    await chalk.signIn(bob);
    await chalk.expectClean();
  });

  await test.step("create a group", async () => {
    await chalk.createGroup("E2E Docs");
    await expect(
      page.getByRole("button", { name: "Connect group" }),
    ).toBeVisible();
    await chalk.expectClean();
  });

  await test.step("link the group to the app", async () => {
    await chalk.connectGroup(bob);
    await expect(
      page.getByRole("button", { name: "Connect group" }),
    ).toHaveCount(0);
    await chalk.expectClean();
  });

  await test.step("create a doc", async () => {
    await page.getByRole("button", { name: "New doc" }).click();
    await page.waitForURL(/\/docs\//);
    await expect(chalk.editor()).toBeVisible();
    await chalk.expectClean();
  });

  await test.step("edit the doc", async () => {
    await chalk.editor().click();
    await page.keyboard.type(title);
    await page.keyboard.press("Enter");
    await page.keyboard.type("Written by the e2e suite.");
    await chalk.expectClean();
  });

  // The doc's room writes the edit to the group's space once edits go idle,
  // and the files list then shows the title it published.
  await test.step("the doc is listed under its new title", async () => {
    await page.getByRole("link", { name: "Files" }).click();
    await expect(page.getByRole("link", { name: title })).toBeVisible({
      timeout: 30_000,
    });
    await chalk.expectClean();
  });

  await test.step("reopening the doc shows the edit", async () => {
    // Selecting the doc opens its details; open it from there.
    await page.getByRole("link", { name: title }).click();
    await page.getByRole("button", { name: "Open doc" }).click();
    await page.waitForURL(/\/docs\//);
    await expect(chalk.editor()).toBeVisible();
    await chalk.expectClean();
    // A fresh load, so the page reads everything back from the server
    // rather than from the client's cache.
    await page.reload();
    await expect(chalk.editor()).toContainText(title);
    await expect(chalk.editor()).toContainText("Written by the e2e suite.");
    await chalk.expectClean();
  });
});

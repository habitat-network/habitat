import { test, expect } from "../harness/fixtures";

test.skip(
  !process.env.E2E_APP || !process.env.E2E_PDS_ACCOUNTS,
  "needs the compose test bed (serves the app and the PDS)",
);

// A member creates an app from the Apps page, deploys the built-in example,
// and then loads it at its own URL: the app's record is written to the
// group's space, indexed from its stream, and the runner starts the code in a
// Dynamic Worker (the LOADER binding, under workerd) from the bundle in pear.
test("a member deploys the example app and loads it", async ({
  page,
  chalk,
  pdsAccounts,
}) => {
  // bob, not alice: the web client's tests expect alice to belong to no group.
  const bob = pdsAccounts.bob;
  const name = `Hello ${Date.now().toString(36)}`;

  await test.step("sign in, create a group and link it", async () => {
    await chalk.signIn(bob);
    await chalk.createGroup("E2E Apps");
    await chalk.connectGroup(bob);
    await expect(
      page.getByRole("button", { name: "Connect group" }),
    ).toHaveCount(0);
    await chalk.expectClean();
  });

  let url = "";
  await test.step("create an app from the example and deploy it", async () => {
    await page.getByRole("link", { name: "Apps" }).click();
    await page.waitForURL(/\/g\/[^/]+\/apps/);
    await page.getByLabel("Name").fill(name);
    await page.getByRole("button", { name: "Use the example" }).click();
    await expect(page.getByText("index.js")).toBeVisible();
    await page.getByRole("button", { name: "Deploy", exact: true }).click();
    // Deploying returns once the stream has indexed the record, so the app's
    // page shows it straight away.
    await expect(page.getByRole("heading", { name })).toBeVisible({
      timeout: 15_000,
    });
    const link = page.getByRole("link", { name: /apps\.habitat-e2e\.dev/ });
    url = (await link.getAttribute("href")) ?? "";
    expect(url).toMatch(
      /^https:\/\/hello-[a-z0-9]+-[a-z0-9]+\.apps\.habitat-e2e\.dev\/$/,
    );
    await chalk.expectClean();
  });

  await test.step("the app answers at its own URL", async () => {
    const res = await page.goto(url);
    expect(res?.status()).toBe(200);
    await expect(page.getByRole("heading", { name })).toBeVisible();
    await expect(page.getByText("Version 1, served from /")).toBeVisible();

    await page.getByRole("link", { name: "Try the API" }).click();
    await expect(page.locator("body")).toContainText('{"hello":"habitat"}');
  });
});

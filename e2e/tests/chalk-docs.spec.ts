import { test, expect } from "../harness/fixtures";

test.skip(
  !process.env.E2E_APP || !process.env.E2E_PDS_ACCOUNTS,
  "needs the compose test bed (serves the app and the PDS)",
);

// A member on the alpha PDS (carol) signs in to the Habitat app (typescript/apps/app,
// chalk's successor), creates a group, links it to the app (pear's admin
// approval), uploads a file, then creates a doc and edits it. The upload must
// be listed as soon as it returns, and the edit must survive a reload and
// show up as the doc's title in the group's files: both arrive through the
// group's subscribe stream.
test("a member links a group, creates a doc and edits it", async ({
  page,
  chalk,
  pdsAccounts,
}) => {
  // carol, not alice or bob: the web client's tests sign in as them and
  // expect them to belong to no group.
  const carol = pdsAccounts.carol;
  const title = `Plan ${Date.now().toString(36)}`;

  await test.step("sign in with the PDS account", async () => {
    await chalk.signIn(carol);
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
    await chalk.connectGroup(carol);
    await expect(
      page.getByRole("button", { name: "Connect group" }),
    ).toHaveCount(0);
    await chalk.expectClean();
  });

  // An upload returns only once the group's stream has indexed its record
  // (the app writes through @habitat-network/jetstream and waits for the
  // event), so the list shows it with no polling or optimistic update.
  await test.step("upload a file; it's listed once the upload returns", async () => {
    await page.getByRole("link", { name: "Files" }).click();
    const name = `e2e-${Date.now().toString(36)}.txt`;
    const uploaded = page.waitForResponse(
      (res) =>
        res.url().includes("/api/upload") && res.request().method() === "POST",
    );
    await page.locator('input[type="file"]').setInputFiles({
      name,
      mimeType: "text/plain",
      buffer: Buffer.from("uploaded by the e2e suite"),
    });
    const res = await uploaded;
    expect(res.ok()).toBe(true);
    expect(
      ((await res.json()) as { syncError?: string }).syncError,
    ).toBeUndefined();
    await expect(page.getByRole("link", { name })).toBeVisible({
      timeout: 5_000,
    });
    await chalk.expectClean();

    // Downloading reads the blob back out of pear (getBlob, as the group):
    // the bytes and the file's name come back as uploaded.
    const href = await page.getByRole("link", { name }).getAttribute("href");
    // Fetched from inside the page: the browser resolves the test domains,
    // Playwright's request context doesn't.
    const download = await page.evaluate(async (url) => {
      const res = await fetch(url);
      return {
        status: res.status,
        body: await res.text(),
        disposition: res.headers.get("content-disposition"),
      };
    }, href!);
    expect(download.status).toBe(200);
    expect(download.body).toBe("uploaded by the e2e suite");
    expect(download.disposition).toContain(encodeURIComponent(name));
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

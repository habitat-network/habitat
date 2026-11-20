import { test, expect } from "../harness/fixtures";
import { APP_ORIGIN } from "../harness/pear";
import { PageErrors } from "../harness/pageErrors";

test.skip(
  !process.env.E2E_APP,
  "needs the compose test bed (serves the web client)",
);

// Signs in to the web client through pear's *.test passthrough login (no
// identity provider involved), then walks the spaces pages from the list down
// to a record, failing on any console error, failed or 4xx/5xx request, or
// error UI along the way.
test("clicking through the spaces pages from the list to a record shows no errors", async ({
  page,
}) => {
  const errors = new PageErrors(page);
  const handle = `alice-${Date.now().toString(36)}-${test.info().parallelIndex}.test`;
  const did = `did:web:${handle}`;
  const spaceType = "network.habitat.e2e.thing";
  const collection = "network.habitat.e2e.note";

  // Wherever we are, an error boundary or toast means something went wrong.
  const expectNoErrorUi = async () => {
    await expect(page.getByText(/something went wrong/i)).toHaveCount(0);
    await expect(page.getByRole("alert")).toHaveCount(0);
  };
  const step = async () => {
    await page.waitForLoadState("networkidle");
    await errors.expectNone();
    await expectNoErrorUi();
  };

  // The header's Spaces button (the breadcrumbs on deeper pages are links).
  const openSpaces = () =>
    page.getByRole("button", { name: "Spaces", exact: true }).click();

  await test.step("sign in through the *.test provider", async () => {
    await page.goto(`${APP_ORIGIN}/oauth-login`);
    await page.getByPlaceholder(/alice\.bsky\.social/).fill(handle);
    await page.getByRole("button", { name: "Sign In" }).click();
    // The passthrough provider signs in without asking for credentials, leaving
    // only pear's consent screen.
    await page.getByRole("button", { name: "Authorize" }).click();
    // Back on the web client. Errors from pear's own sign-in pages (e.g. their
    // missing favicon) are not what this test is about.
    await page.waitForURL(`${APP_ORIGIN}/**`);
    errors.reset();
    await expect(page.getByRole("button", { name: "Logout" })).toBeVisible();
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

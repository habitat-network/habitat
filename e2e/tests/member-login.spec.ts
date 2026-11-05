import { test, expect } from "../harness/fixtures";

// Full OAuth sign-in as an org member: handle -> password -> consent -> the
// client's redirect_uri with an authorization code. Replaces the old Go/Selenium
// TestPearOAuthFlow, but uses a password org so no external PDS is needed.
test.describe("member login", () => {
  test("signs in with handle and password and is redirected back with a code", async ({
    page,
    seed,
    mcpClient,
    memberLogin,
  }) => {
    const { url, state } = mcpClient.authorizeUrl();
    // Nothing listens on the redirect URI; the test only needs to see the redirect.
    await page.route(`${mcpClient.redirectUri}**`, (route) =>
      route.fulfill({ status: 200, body: "callback" }),
    );

    await page.goto(url);
    await memberLogin.enterHandle(seed.org.adminHandle);
    await memberLogin.enterPassword(seed.org.adminPassword);
    await memberLogin.authorize();

    await page.waitForURL(`${mcpClient.redirectUri}**`);
    const callback = new URL(page.url());
    expect(callback.searchParams.get("state")).toBe(state);
    expect(callback.searchParams.get("code")).toBeTruthy();
  });

  test("shows an error for a wrong password", async ({
    page,
    createOrg,
    mcpClient,
    memberLogin,
  }) => {
    const org = await createOrg();
    await page.goto(mcpClient.authorizeUrl().url);
    await memberLogin.enterHandle(org.adminHandle);
    await memberLogin.enterPassword("wrong-password");

    await memberLogin.expectPasswordError();
    await expect(page).toHaveURL(/\/ui\/login\/habitat/);
  });

  test("rejects an unknown handle", async ({
    page,
    mcpClient,
    memberLogin,
  }) => {
    await page.goto(mcpClient.authorizeUrl().url);
    await memberLogin.enterHandle("nobody.nowhere.localhost");

    await expect(
      page.getByText("Couldn't sign in with that handle"),
    ).toBeVisible();
  });
});

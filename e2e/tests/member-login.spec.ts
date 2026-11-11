import { test, expect } from "../harness/fixtures";

// Member sign-in as far as it can go without a real identity provider: the
// OAuth authorize request lands on pear's sign-in prompt, a work email is
// resolved to the org that owns its domain, and the browser is sent to that
// org's login provider (Google). Google itself is stubbed out.
test.describe("member login", () => {
  test("sends a member of an email-domain org to Google", async ({
    page,
    seed,
    mcpClient,
    memberLogin,
  }) => {
    await page.route("https://accounts.google.com/**", (route) =>
      route.fulfill({ status: 200, body: "google" }),
    );

    await page.goto(mcpClient.authorizeUrl().url);
    await expect(page).toHaveURL(/\/ui\/login\/mcp/);
    await memberLogin.submitIdentifier(`alice@${seed.org.domain}`);

    await page.waitForURL("https://accounts.google.com/**");
    const google = new URL(page.url());
    expect(google.searchParams.get("client_id")).toBe("e2e-google-client-id");
    expect(google.searchParams.get("login_hint")).toBe(
      `alice@${seed.org.domain}`,
    );
    expect(google.searchParams.get("redirect_uri")).toBe(
      "https://localhost/oauth-callback",
    );
  });

  test("rejects an email at a domain no org owns", async ({
    page,
    mcpClient,
    memberLogin,
  }) => {
    await page.goto(mcpClient.authorizeUrl().url);
    await memberLogin.submitIdentifier("alice@unclaimed.test");

    await memberLogin.expectError("Couldn't sign in with that handle");
    await expect(page).toHaveURL(/\/ui\/login\/mcp/);
  });

  test("rejects an unknown handle", async ({
    page,
    mcpClient,
    memberLogin,
  }) => {
    await page.goto(mcpClient.authorizeUrl().url);
    await memberLogin.submitIdentifier("nobody.nowhere.localhost");

    await memberLogin.expectError("Couldn't sign in with that handle");
  });
});

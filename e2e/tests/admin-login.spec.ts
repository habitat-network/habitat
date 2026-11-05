import { test, expect } from "../harness/fixtures";

test.describe("instance admin login", () => {
  test("logs in with the admin password and logs out", async ({
    page,
    adminLoginPage,
    adminHomePage,
    adminPassword,
  }) => {
    await adminLoginPage.goto();
    await adminLoginPage.login(adminPassword);

    await expect(page).toHaveURL(/\/ui\/admin\/?$/);
    await adminHomePage.expectLoaded();

    await adminHomePage.logout();
    await expect(page).toHaveURL(/\/ui\/admin\/login/);
  });

  test("rejects a wrong password", async ({ page, adminLoginPage }) => {
    await adminLoginPage.goto();
    await adminLoginPage.login("not-the-password");

    await expect(page).toHaveURL(/\/ui\/admin\/login/);
    await adminLoginPage.expectError("invalid password");
  });

  test("redirects to login without a session", async ({
    page,
    adminHomePage,
  }) => {
    await adminHomePage.goto();
    await expect(page).toHaveURL(/\/ui\/admin\/login/);
  });
});

import { expect, type Page } from "@playwright/test";

/** Instance admin login (/admin/login -> /ui/admin/login). */
export class AdminLoginPage {
  constructor(private readonly page: Page) {}

  async goto() {
    await this.page.goto("/admin/login");
  }

  async login(password: string) {
    await this.page.getByLabel("Password").fill(password);
    await this.page.getByRole("button", { name: "Log in" }).click();
  }

  async expectError(message: string) {
    await expect(this.page.getByText(message)).toBeVisible();
  }
}

/** Instance settings page, shown after admin login. */
export class AdminHomePage {
  constructor(private readonly page: Page) {}

  async goto() {
    await this.page.goto("/admin");
  }

  async expectLoaded() {
    await expect(this.page.getByText("Instance settings")).toBeVisible();
  }

  async logout() {
    await this.page.getByRole("button", { name: "Log out" }).click();
  }
}

import { expect, type Page } from "@playwright/test";

/**
 * The member sign-in screens a browser walks through during an OAuth
 * authorize request: handle prompt -> password -> consent.
 */
export class MemberLoginFlow {
  constructor(private readonly page: Page) {}

  async enterHandle(handle: string) {
    await this.page.getByPlaceholder("alice.example.com").fill(handle);
    await this.page.getByRole("button", { name: "Continue" }).click();
  }

  async enterPassword(password: string) {
    await this.page.getByPlaceholder("password").fill(password);
    await this.page.getByRole("button", { name: "Sign in" }).click();
  }

  async expectPasswordError() {
    await expect(
      this.page.locator("p.text-destructive, [role=alert]").first(),
    ).toBeVisible();
  }

  async authorize() {
    await this.page.getByRole("button", { name: "Authorize" }).click();
  }
}

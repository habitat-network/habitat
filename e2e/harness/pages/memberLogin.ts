import { expect, type Page } from "@playwright/test";

/**
 * The sign-in prompt a browser lands on during an OAuth authorize request
 * (/ui/login/mcp). Members enter a work email (or handle); pear then sends
 * them to their org's login provider.
 */
export class MemberLoginPage {
  constructor(private readonly page: Page) {}

  async submitIdentifier(identifier: string) {
    await this.page.getByPlaceholder("alice.example.com").fill(identifier);
    await this.page.getByRole("button", { name: "Continue" }).click();
  }

  async expectError(message: string) {
    await expect(this.page.getByText(message)).toBeVisible();
  }
}

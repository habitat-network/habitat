import { expect, type Page } from "@playwright/test";
import { CHALK_ORIGIN, PDS_ORIGIN, PEAR_ORIGIN } from "../pear";
import type { PdsAccount } from "../pds";
import { PageErrors } from "../pageErrors";

/**
 * The Habitat app (typescript/apps/app: docs, files and group management, the
 * successor of chalk) served by the compose test bed, signed in with an
 * account on the test bed's alpha PDS. `expectClean()` is the check a step
 * ends with: no console errors, failed or 4xx/5xx requests, or error UI since
 * the last call.
 */
export class ChalkApp {
  private readonly errors: PageErrors;

  constructor(private readonly page: Page) {
    this.errors = new PageErrors(page);
  }

  /**
   * Signs in as a PDS account. The app is an atproto OAuth client of the
   * account's PDS: the PDS's sign-in and consent pages, then back to the app.
   */
  async signIn(account: PdsAccount) {
    const { page } = this;
    await page.goto(`${CHALK_ORIGIN}/login`);
    await page.getByPlaceholder(/alice\.bsky\.social/).fill(account.handle);
    await page.getByRole("button", { name: "Sign In" }).click();
    await this.pdsSignIn(account);
    await this.landed();
  }

  /** Creates a group with a unique handle and lands on its files. */
  async createGroup(name: string) {
    const { page } = this;
    await page.goto(`${CHALK_ORIGIN}/groups/new`);
    await page.getByLabel("Name").fill(name);
    await page.getByLabel("Handle").fill(`g${Date.now().toString(36)}`);
    await page.getByRole("button", { name: "Create group" }).click();
    await page.waitForURL(/\/g\/[^/]+\/files/);
  }

  /**
   * Links the open group to the app: pear asks for an admin of the group
   * (`admin`, signing in at their PDS) to approve the app, then sends the
   * browser back to the group.
   */
  async connectGroup(admin: PdsAccount) {
    const { page } = this;
    await page.getByRole("button", { name: "Connect group" }).click();
    await page.waitForURL(`${PEAR_ORIGIN}/**`);
    await page.getByPlaceholder("handle").fill(admin.handle);
    await page.getByRole("button", { name: "Continue" }).click();
    await this.pdsSignIn(admin);
    await page.getByRole("button", { name: "Authorize" }).click();
    await page.waitForURL(/chalk\.[^/]+\/g\/[^/]+/);
    await this.landed();
  }

  /** The doc editor (tiptap). */
  editor() {
    return this.page.locator(".ProseMirror");
  }

  /**
   * The PDS's own sign-in and consent pages. A browser already signed in to
   * the PDS skips the password and goes straight to consent.
   */
  private async pdsSignIn(account: PdsAccount) {
    const { page } = this;
    await page.waitForURL(`${PDS_ORIGIN}/**`);
    const password = page.locator('input[type="password"]');
    const authorize = page.getByRole("button", {
      name: /^(authorize|accept|allow)$/i,
    });
    await expect(password.or(authorize).first()).toBeVisible();
    if (await password.isVisible()) {
      await password.fill(account.password);
      await page
        .getByRole("button", { name: /^(sign in|next|continue)$/i })
        .click();
    }
    await authorize.click();
  }

  /** Back on the app, signed in. */
  private async landed() {
    // Errors from pear's or the PDS's own pages (e.g. their missing favicon)
    // are not what the tests are about.
    await this.page.waitForURL(`${CHALK_ORIGIN}/**`);
    await this.page.waitForLoadState("networkidle");
    this.errors.reset();
  }

  /** Waits for the page to settle, then asserts nothing has gone wrong. */
  async expectClean() {
    await this.page.waitForLoadState("networkidle");
    await this.errors.expectNone();
    await expect(this.page.getByText(/something went wrong/i)).toHaveCount(0);
  }
}

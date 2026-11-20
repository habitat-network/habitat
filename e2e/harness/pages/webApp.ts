import { expect, type Page } from "@playwright/test";
import { APP_ORIGIN, PDS_ORIGIN } from "../pear";
import type { PdsAccount } from "../pds";
import { PageErrors } from "../pageErrors";

/**
 * The Habitat web client (frontend/) served by the compose test bed, signed in
 * with an account on the test bed's alpha PDS (`signIn`), or through pear's
 * *.test passthrough login (`signInPassthrough`, no identity provider involved).
 * `expectClean()` is the check every page of a walk ends with: no console
 * errors, failed or 4xx/5xx requests, or error UI since the last call.
 */
export class WebApp {
  private readonly errors: PageErrors;

  constructor(private readonly page: Page) {
    this.errors = new PageErrors(page);
  }

  /** A handle no other test uses, at the reserved .test TLD. */
  static uniqueHandle(prefix: string, workerIndex: number) {
    return `${prefix}-${Date.now().toString(36)}-${workerIndex}.test`;
  }

  /**
   * Signs in as a PDS account. The web client is an atproto OAuth client of the
   * account's PDS, so this is the PDS's own sign-in and consent pages and then
   * straight back to the web client. (For a *.test handle, the "PDS" is pear.)
   */
  async signIn(account: PdsAccount) {
    const { page } = this;
    await this.startSignIn(account.handle);
    await page.waitForURL(`${PDS_ORIGIN}/**`);
    await page.locator('input[type="password"]').fill(account.password);
    await page
      .getByRole("button", { name: /^(sign in|next|continue)$/i })
      .click();
    // The PDS asks the account to let pear (its OAuth client) in.
    await page
      .getByRole("button", { name: /^(authorize|accept|allow)$/i })
      .click();
    await this.landed();
  }

  /**
   * Signs in with a *.test handle, which pear lets in without credentials,
   * leaving only pear's consent screen.
   */
  async signInPassthrough(handle: string) {
    await this.startSignIn(handle);
    await this.page.getByRole("button", { name: "Authorize" }).click();
    await this.landed();
  }

  private async startSignIn(handle: string) {
    const { page } = this;
    await page.goto(`${APP_ORIGIN}/oauth-login`);
    await page.getByPlaceholder(/alice\.bsky\.social/).fill(handle);
    await page.getByRole("button", { name: "Sign In" }).click();
  }

  /** Back on the web client, signed in. */
  private async landed() {
    const { page } = this;
    // Errors from pear's or the PDS's own sign-in pages (e.g. their missing
    // favicon) are not what the tests are about.
    await page.waitForURL(`${APP_ORIGIN}/**`);
    this.errors.reset();
    await expect(page.getByRole("button", { name: "Logout" })).toBeVisible();
  }

  /** The header's Spaces button (the breadcrumbs on deeper pages are links). */
  openSpaces() {
    return this.page
      .getByRole("button", { name: "Spaces", exact: true })
      .click();
  }

  /** Waits for the page to settle, then asserts nothing has gone wrong. */
  async expectClean() {
    await this.page.waitForLoadState("networkidle");
    await this.errors.expectNone();
    // An error boundary or toast means something went wrong.
    await expect(this.page.getByText(/something went wrong/i)).toHaveCount(0);
    await expect(this.page.getByRole("alert")).toHaveCount(0);
  }
}

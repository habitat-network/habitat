import { expect, type Page } from "@playwright/test";
import { APP_ORIGIN } from "../pear";
import { PageErrors } from "../pageErrors";

/**
 * The Habitat web client (frontend/) served by the compose test bed, signed in
 * through pear's *.test passthrough login (no identity provider involved).
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

  async signIn(handle: string) {
    const { page } = this;
    await page.goto(`${APP_ORIGIN}/oauth-login`);
    await page.getByPlaceholder(/alice\.bsky\.social/).fill(handle);
    await page.getByRole("button", { name: "Sign In" }).click();
    // The passthrough provider signs in without asking for credentials, leaving
    // only pear's consent screen.
    await page.getByRole("button", { name: "Authorize" }).click();
    // Back on the web client. Errors from pear's own sign-in pages (e.g. their
    // missing favicon) are not what the tests are about.
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

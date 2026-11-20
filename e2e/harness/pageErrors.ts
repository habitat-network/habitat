import { expect, type Page } from "@playwright/test";

/**
 * Collects everything that signals a broken page while a test drives it:
 * console errors, uncaught exceptions, failed requests and 4xx/5xx responses.
 * Call `expectNone()` after each step so a failure names the page it happened on.
 */
export class PageErrors {
  private errors: string[] = [];
  private pending: Promise<void>[] = [];

  constructor(private readonly page: Page) {
    page.on("console", (msg) => {
      if (msg.type() === "error") {
        this.errors.push(
          `console.error: ${msg.text()} (${msg.location().url})`,
        );
      }
    });
    page.on("pageerror", (err) => {
      this.errors.push(`uncaught exception: ${err.message}`);
    });
    page.on("requestfailed", (req) => {
      // The page cancelled it (navigated away, or the app aborted a fetch it
      // no longer needs): nothing failed on the server.
      if (req.failure()?.errorText === "net::ERR_ABORTED") return;
      this.errors.push(
        `request failed: ${req.method()} ${req.url()} (${req.failure()?.errorText})`,
      );
    });
    page.on("response", (res) => {
      if (res.status() < 400) return;
      // The body usually says why, so keep it with the report.
      this.pending.push(
        res
          .text()
          .catch(() => "")
          .then((body) => {
            this.errors.push(
              `${res.status()} ${res.request().method()} ${res.url()} ${body.slice(0, 300)}`.trim(),
            );
          }),
      );
    });
  }

  /** Forgets what has been collected, e.g. from pages the test isn't about. */
  reset() {
    this.errors = [];
    this.pending = [];
  }

  /** Asserts nothing has gone wrong since the last call, then starts afresh. */
  async expectNone() {
    // A round trip to the page delivers any event still in flight, e.g. the
    // response that a console error just reported.
    await this.page.evaluate(() => {});
    await Promise.all(this.pending);
    this.pending = [];
    const errors = this.errors;
    this.errors = [];
    expect(errors, errors.join("\n")).toEqual([]);
  }
}

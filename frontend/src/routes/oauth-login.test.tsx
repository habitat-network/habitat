import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { mockHabitatInstance, renderApp } from "@/test/render";

describe("/oauth-login", () => {
  it("renders the sign-in form", async () => {
    mockHabitatInstance();
    await renderApp("/oauth-login");
    expect(
      await screen.findByRole("button", { name: "Sign In" }),
    ).toBeDefined();
  });

  it("prefills the handle and shows the error from the search params", async () => {
    mockHabitatInstance();
    await renderApp(
      "/oauth-login?handle=alice.bsky.social&error=access%20denied",
    );
    expect(await screen.findByText("access denied")).toBeDefined();
    expect(screen.getByRole<HTMLInputElement>("textbox").value).toBe(
      "alice.bsky.social",
    );
  });
});

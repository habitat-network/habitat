import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import AuthForm from "./AuthForm";
import type { AuthManager } from "./authManager";
import { EMAIL_DOMAIN_NOT_FOUND_MESSAGE } from "./SignInForm";

const REDIRECT_URL = "https://app.test/";

function renderForm(login: AuthManager["login"]) {
  const authManager = { login } as unknown as AuthManager;
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AuthForm authManager={authManager} redirectUrl={REDIRECT_URL} />
    </QueryClientProvider>,
  );
}

function signIn(loginHint: string) {
  fireEvent.change(screen.getByRole("textbox"), {
    target: { value: loginHint },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign In" }));
}

afterEach(() => {
  cleanup();
});

describe("AuthForm", () => {
  it("signs in through the auth manager", async () => {
    const login = vi.fn().mockResolvedValue(undefined);
    renderForm(login);
    signIn("bob@company.com");
    await waitFor(() => {
      expect(login).toHaveBeenCalledWith("bob@company.com", REDIRECT_URL);
    });
  });

  it("explains when an email's domain isn't set up", async () => {
    // BrowserOAuthClient wraps resolver failures, so the habitat error is
    // the cause rather than the thrown error itself.
    const login = vi.fn().mockRejectedValue(
      new Error("Failed to resolve identity: bob@gmail.com", {
        // Shaped like @habitat-network/habitat's HabitatIdentityResolverError.
        cause: Object.assign(new Error("not found"), {
          status: 404,
          xrpcError: "DidNotFound",
        }),
      }),
    );
    renderForm(login);
    signIn("bob@gmail.com");
    expect(
      await screen.findByText(EMAIL_DOMAIN_NOT_FOUND_MESSAGE),
    ).toBeDefined();
  });
});

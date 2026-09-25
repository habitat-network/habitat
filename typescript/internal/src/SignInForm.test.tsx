import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import SignInForm from "./SignInForm";

function renderForm(
  props: {
    defaultHandle?: string;
    serverError?: string;
    onSubmit?: (loginHint: string) => Promise<void>;
  } = {},
) {
  const onSubmit = props.onSubmit ?? vi.fn().mockResolvedValue(undefined);
  render(
    <QueryClientProvider client={new QueryClient()}>
      <SignInForm {...props} onSubmit={onSubmit} />
    </QueryClientProvider>,
  );
  return { onSubmit };
}

function choose(label: "Sign in with AT Protocol" | "Sign in with Google") {
  fireEvent.click(screen.getByRole("button", { name: label }));
}

function submit(placeholder: string, value: string) {
  fireEvent.change(screen.getByPlaceholderText(placeholder), {
    target: { value },
  });
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
}

afterEach(() => {
  cleanup();
});

describe("SignInForm", () => {
  it("offers AT Protocol and Google sign-in options", () => {
    renderForm();
    expect(
      screen.getByRole("button", { name: "Sign in with AT Protocol" }),
    ).toBeDefined();
    expect(
      screen.getByRole("button", { name: "Sign in with Google" }),
    ).toBeDefined();
    expect(screen.queryByRole("textbox")).toBeNull();
  });

  it("signs in with an AT Protocol handle", async () => {
    const { onSubmit } = renderForm();
    choose("Sign in with AT Protocol");
    submit("alice.bsky.social", " bob.bsky.social ");
    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("bob.bsky.social");
    });
  });

  it("rejects an email in the AT Protocol option", async () => {
    const { onSubmit } = renderForm();
    choose("Sign in with AT Protocol");
    submit("alice.bsky.social", "bob@company.com");
    expect(
      await screen.findByText(
        "That looks like an email. Use Sign in with Google instead.",
      ),
    ).toBeDefined();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("signs in with a Google work email", async () => {
    const { onSubmit } = renderForm();
    choose("Sign in with Google");
    submit("you@company.com", "bob@company.com");
    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("bob@company.com");
    });
  });

  it("rejects a handle in the Google option", async () => {
    const { onSubmit } = renderForm();
    choose("Sign in with Google");
    submit("you@company.com", "bob.bsky.social");
    expect(
      await screen.findByText("Enter your work email, e.g. you@company.com"),
    ).toBeDefined();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("shows the error thrown by onSubmit", async () => {
    renderForm({
      onSubmit: () => Promise.reject(new Error("domain not set up")),
    });
    choose("Sign in with Google");
    submit("you@company.com", "bob@company.com");
    expect(await screen.findByText("domain not set up")).toBeDefined();
  });

  it("returns to the sign-in options on back", () => {
    renderForm();
    choose("Sign in with Google");
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(
      screen.getByRole("button", { name: "Sign in with AT Protocol" }),
    ).toBeDefined();
    expect(screen.queryByRole("textbox")).toBeNull();
  });

  it("opens the AT Protocol option prefilled with a default handle", () => {
    renderForm({ defaultHandle: "alice.bsky.social" });
    const input =
      screen.getByPlaceholderText<HTMLInputElement>("alice.bsky.social");
    expect(input.value).toBe("alice.bsky.social");
  });

  it("shows a server error on the options view", () => {
    renderForm({ serverError: "access denied" });
    expect(screen.getByText("access denied")).toBeDefined();
  });
});

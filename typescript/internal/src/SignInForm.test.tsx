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

function submit(value: string) {
  fireEvent.change(screen.getByRole("textbox"), { target: { value } });
  fireEvent.click(screen.getByRole("button", { name: "Sign In" }));
}

afterEach(() => {
  cleanup();
});

describe("SignInForm", () => {
  it("signs in with an AT Protocol handle", async () => {
    const { onSubmit } = renderForm();
    submit(" bob.bsky.social ");
    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("bob.bsky.social");
    });
  });

  it("signs in with a work email", async () => {
    const { onSubmit } = renderForm();
    submit("bob@company.com");
    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("bob@company.com");
    });
  });

  it("requires a handle or email", async () => {
    const { onSubmit } = renderForm();
    submit("");
    expect(
      await screen.findByText("Handle or email is required"),
    ).toBeDefined();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("shows the error thrown by onSubmit", async () => {
    renderForm({
      onSubmit: () => Promise.reject(new Error("domain not set up")),
    });
    submit("bob@company.com");
    expect(await screen.findByText("domain not set up")).toBeDefined();
  });

  it("prefills a default handle", () => {
    renderForm({ defaultHandle: "alice.bsky.social" });
    expect(screen.getByRole<HTMLInputElement>("textbox").value).toBe(
      "alice.bsky.social",
    );
  });

  it("shows a server error", () => {
    renderForm({ serverError: "access denied" });
    expect(screen.getByText("access denied")).toBeDefined();
  });
});
